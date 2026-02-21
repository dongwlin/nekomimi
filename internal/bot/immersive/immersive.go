package immersive

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

// Default configuration values for the immersive buffer.
const (
	defaultMaxBatchMessages          = 10
	defaultMaxBatchChars             = 1200
	defaultImmediateDelayMS          = 120
	defaultTimelineMaxMessages       = 200
	defaultTimelineOverflowMessages  = 50
	defaultContinuousMinChars        = 12
	defaultContinuousMaxChars        = 80
	defaultContinuousMinMS           = 300
	defaultContinuousMaxMS           = 900
	defaultPokeReactionWindowMS      = 180000
	defaultPokeReactionMildThresh    = 3
	defaultPokeReactionAnnoyedThresh = 6
	maxImmersiveWaitRounds           = 2
	defaultProtocolErrorWaitMS       = 600
	immersiveEmptyReplyFallback      = "..."
)

var errControlHeaderProtocol = errors.New("control header protocol")

// NewImmersiveBuffer creates a new ImmersiveBuffer with the given configuration,
// LLM manager, and bot nicknames.
func NewImmersiveBuffer(cfg config.ImmersiveConfig, llmManager *llm.Manager, nicknames []string) *ImmersiveBuffer {
	normalized := normalizeImmersiveConfig(cfg)
	configNames := normalizedBotNames(nicknames)
	return &ImmersiveBuffer{
		cfg:       normalized,
		llm:       llmManager,
		nicknames: configNames,
		identity: botIdentity{
			ConfigNicknames: configNames,
		},
		sessions: make(map[string]*immersiveSession),
	}
}

// NewEngine creates an immersive engine with configured policies.
func NewEngine(cfg config.ImmersiveConfig, llmManager *llm.Manager, nicknames []string) *ImmersiveBuffer {
	return NewImmersiveBuffer(cfg, llmManager, nicknames)
}

// ReloadConfig refreshes immersive policies without clearing queued/timeline records.
func (b *ImmersiveBuffer) ReloadConfig(cfg config.ImmersiveConfig, nicknames []string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cfg = normalizeImmersiveConfig(cfg)
	b.nicknames = normalizedBotNames(nicknames)
	b.identity.ConfigNicknames = normalizedBotNames(nicknames)
}

// Enqueue adds a new message to the session buffer and schedules an immediate
// flush. It detects message signals (mentions, addressed to bot, questions)
// for downstream policies.
func (b *ImmersiveBuffer) Enqueue(ctx *zero.Ctx, sessionKey, text, speaker string, isPrivate bool) {
	if b == nil || b.llm == nil {
		return
	}
	trimmed := strings.TrimSpace(text)
	if strings.TrimSpace(sessionKey) == "" || trimmed == "" {
		return
	}
	state := b.session(sessionKey)
	now := time.Now()
	charCount := len([]rune(trimmed))
	mention, addressed, question := b.detectMessageSignals(ctx, trimmed)
	msg := queuedMessage{
		text:             trimmed,
		speaker:          strings.TrimSpace(speaker),
		ts:               now,
		chars:            charCount,
		isMentionBot:     mention,
		isQuestion:       question,
		isAddressedToBot: addressed,
	}

	state.mu.Lock()
	state.lastCtx = ctx
	state.queue = append(state.queue, msg)
	state.queueChars += charCount
	state.timeline = appendTimelineMessage(state.timeline, msg, b.timelineLimit())

	// Cooldown is intentionally disabled: flush immediately after enqueue.
	cooldown := time.Duration(0)
	queueSnapshot := make([]queuedMessage, len(state.queue))
	copy(queueSnapshot, state.queue)
	log.Info().
		Str("session", sessionKey).
		Bool("is_private", isPrivate).
		Bool("mention", mention).
		Bool("addressed", addressed).
		Bool("question", question).
		Int("queue_len", len(queueSnapshot)).
		Int("queue_chars", state.queueChars).
		Int64("cooldown_ms", cooldown.Milliseconds()).
		Msg("immersive enqueue scheduled")
	state.mu.Unlock()

	state.mu.Lock()
	if state.timer != nil {
		state.timer.Stop()
	}
	state.timer = time.AfterFunc(cooldown, func() {
		b.flush(sessionKey)
	})
	state.mu.Unlock()
}

// Clear removes all queued messages and resets the session state for the given session key.
func (b *ImmersiveBuffer) Clear(sessionKey string) {
	if b == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	state := b.session(sessionKey)
	state.mu.Lock()
	state.queue = nil
	state.queueChars = 0
	state.timeline = nil
	state.waitRounds = 0
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.mu.Unlock()
	log.Info().Str("session", sessionKey).Msg("immersive session buffer cleared")
}

// flush processes the queued messages for a session using a single immersive LLM
// stream call. The first line of the stream is parsed as a control header:
// SKIP / WAIT:<ms> / REPLY.
func (b *ImmersiveBuffer) flush(sessionKey string) {
	state := b.session(sessionKey)
	state.mu.Lock()
	if state.inFlight {
		if len(state.queue) > 0 {
			delay := time.Duration(b.cfg.ImmediateDelayMS) * time.Millisecond
			if state.timer != nil {
				state.timer.Stop()
			}
			state.timer = time.AfterFunc(delay, func() {
				b.flush(sessionKey)
			})
		}
		state.mu.Unlock()
		return
	}
	if len(state.queue) == 0 {
		state.mu.Unlock()
		return
	}
	if !b.llm.IsEnabled() || !b.llm.IsImmersive(sessionKey) {
		state.queue = nil
		state.queueChars = 0
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		state.mu.Unlock()
		return
	}
	queue := make([]queuedMessage, len(state.queue))
	copy(queue, state.queue)
	timeline := make([]queuedMessage, len(state.timeline))
	copy(timeline, state.timeline)
	timelineSnapshotLen := len(timeline)
	state.queue = nil
	state.queueChars = 0
	state.inFlight = true
	waitRounds := state.waitRounds
	ctx := state.lastCtx
	state.mu.Unlock()

	log.Info().
		Str("session", sessionKey).
		Int("batch_messages", len(queue)).
		Int("batch_chars", sumQueueChars(queue)).
		Int("wait_rounds", waitRounds).
		Msg("immersive flush started")

	timeline = trimTimelineTail(timeline, b.timelineLimit())
	state.mu.Lock()
	if len(state.timeline) > timelineSnapshotLen {
		tail := make([]queuedMessage, len(state.timeline)-timelineSnapshotLen)
		copy(tail, state.timeline[timelineSnapshotLen:])
		merged := make([]queuedMessage, 0, len(timeline)+len(tail))
		merged = append(merged, timeline...)
		merged = append(merged, tail...)
		state.timeline = trimTimelineTail(merged, b.timelineLimit())
	} else {
		state.timeline = trimTimelineTail(timeline, b.timelineLimit())
	}
	state.mu.Unlock()

	input := buildCombinedInput(timeline, b.currentIdentity())
	if strings.TrimSpace(input) == "" {
		input = buildCombinedInput(queue, b.currentIdentity())
	}
	repeatText, repeatCount, repeatParticipants := detectConsecutiveRepeat(queue)
	if repeatText != "" && ctx != nil {
		ctx.Send(repeatText)
		b.recordAssistantUtterance(sessionKey, repeatText)
		log.Info().
			Str("session", sessionKey).
			Str("repeat_text", repeatText).
			Int("repeat_count", repeatCount).
			Int("repeat_participants", repeatParticipants).
			Msg("immersive repeat triggered")
		state.mu.Lock()
		state.inFlight = false
		state.waitRounds = 0
		pending := len(state.queue) > 0
		state.mu.Unlock()
		if pending {
			log.Info().Str("session", sessionKey).Msg("pending messages detected, flushing again")
			b.flush(sessionKey)
		}
		return
	}
	if strings.TrimSpace(input) == "" {
		state.mu.Lock()
		state.inFlight = false
		state.waitRounds = 0
		pending := len(state.queue) > 0
		state.mu.Unlock()
		if pending {
			b.flush(sessionKey)
		}
		return
	}

	headerParser := newControlHeaderParser()
	acc := newStreamChunkAccumulator(b.cfg.ContinuousSpeech)
	var replyBuilder strings.Builder
	var sentReplyBuilder strings.Builder
	sentMessages := 0
	sentChars := 0
	decision := controlHeaderDecision{}
	recordReply := func(reply, reason string) {
		trimmed := strings.TrimSpace(reply)
		if trimmed == "" {
			return
		}
		b.recordAssistantUtterance(sessionKey, trimmed)
		b.llm.AppendTurn(sessionKey, input, "", trimmed)
		log.Info().
			Str("session", sessionKey).
			Str("reason", reason).
			Int("reply_chars", len([]rune(trimmed))).
			Msg("immersive reply recorded into timeline and llm history")
	}

	onEvent := func(event llm.StreamEvent) error {
		switch event.Type {
		case llm.StreamEventDelta:
			delta := event.Delta
			parsedDecision, bodyDelta, ready, err := headerParser.Consume(delta)
			if err != nil {
				return fmt.Errorf("%w: %w", errControlHeaderProtocol, err)
			}
			if !ready {
				return nil
			}
			decision = parsedDecision
			if parsedDecision.action != controlActionReply || bodyDelta == "" {
				return nil
			}
			replyBuilder.WriteString(bodyDelta)
			if ctx == nil || !b.cfg.ContinuousSpeech.Enabled {
				return nil
			}
			chunks := acc.Append(bodyDelta)
			for _, chunk := range chunks {
				if sentMessages > 0 {
					time.Sleep(nextContinuousSpeechDelay(b.cfg.ContinuousSpeech))
				}
				ctx.Send(chunk)
				sentReplyBuilder.WriteString(chunk)
				sentMessages++
				sentChars += len([]rune(chunk))
			}
		case llm.StreamEventToolCall, llm.StreamEventToolResult, llm.StreamEventFinal, llm.StreamEventError:
			log.Debug().
				Str("session", sessionKey).
				Int64("stream_seq", event.Seq).
				Int("stream_step", event.Step).
				Str("stream_event_type", string(event.Type)).
				Msg("immersive stream event")
		}
		return nil
	}

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	_, streamErr := b.llm.ReplyStreamWithExtraPrompt(
		streamCtx,
		input,
		sessionKey,
		"",
		llmprompt.ImmersiveControlPrompt,
		onEvent,
	)
	if streamErr == nil {
		finalDecision, err := headerParser.Finalize()
		if err != nil {
			streamErr = fmt.Errorf("%w: %w", errControlHeaderProtocol, err)
		} else {
			decision = finalDecision
		}
	}

	action := decision.action
	waitMS := decision.waitMS
	decisionReason := "model"
	if streamErr != nil {
		if isControlHeaderProtocolError(streamErr) {
			if waitRounds == 0 {
				action = controlActionWait
				waitMS = defaultProtocolErrorWaitMS
				decisionReason = "protocol_error_wait"
			} else {
				action = controlActionSkip
				waitMS = 0
				decisionReason = "protocol_error_skip"
			}
		} else {
			action = controlActionSkip
			waitMS = 0
			decisionReason = "llm_stream_error_skip"
		}
	}
	if action == controlActionWait && waitRounds >= maxImmersiveWaitRounds {
		action = controlActionSkip
		waitMS = 0
		decisionReason = "wait_round_limit_skip"
	}
	if action == "" {
		action = controlActionSkip
		waitMS = 0
		decisionReason = "empty_action_skip"
	}

	decisionLog := log.Info().
		Str("session", sessionKey).
		Str("action", string(action)).
		Int("wait_ms", waitMS).
		Int("wait_rounds", waitRounds).
		Str("reason", decisionReason)
	if streamErr != nil {
		decisionLog = decisionLog.Err(streamErr)
	}
	decisionLog.Msg("immersive control decision evaluated")
	if action == controlActionSkip && streamErr != nil && ctx != nil && sentMessages > 0 {
		recordReply(sentReplyBuilder.String(), "partial_stream_recorded")
	}

	if action == controlActionWait {
		waitDelay := time.Duration(waitMS) * time.Millisecond
		state.mu.Lock()
		state.queue = prependMessages(queue, state.queue)
		state.queueChars += sumQueueChars(queue)
		state.inFlight = false
		state.waitRounds++
		if state.timer != nil {
			state.timer.Stop()
		}
		state.timer = time.AfterFunc(waitDelay, func() {
			b.flush(sessionKey)
		})
		state.mu.Unlock()
		log.Info().
			Str("session", sessionKey).
			Int64("next_wait_ms", waitDelay.Milliseconds()).
			Int("wait_rounds", waitRounds+1).
			Msg("immersive flush deferred by wait action")
		return
	}

	if action == controlActionReply {
		if ctx != nil && b.cfg.ContinuousSpeech.Enabled {
			for _, chunk := range acc.FlushTail() {
				if sentMessages > 0 {
					time.Sleep(nextContinuousSpeechDelay(b.cfg.ContinuousSpeech))
				}
				ctx.Send(chunk)
				sentReplyBuilder.WriteString(chunk)
				sentMessages++
				sentChars += len([]rune(chunk))
			}
		}
		reply := strings.TrimSpace(replyBuilder.String())
		if reply == "" {
			reply = immersiveEmptyReplyFallback
		}
		if ctx == nil {
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(reply))).
				Msg("immersive reply prepared without ctx")
		} else if b.cfg.ContinuousSpeech.Enabled {
			if sentMessages == 0 {
				ctx.Send(reply)
				sentReplyBuilder.WriteString(reply)
				sentMessages = 1
				sentChars = len([]rune(reply))
			}
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(reply))).
				Int("reply_messages", sentMessages).
				Int("sent_chars", sentChars).
				Msg("immersive continuous reply sent")
		} else {
			ctx.Send(reply)
			sentReplyBuilder.WriteString(reply)
			sentMessages = 1
			sentChars = len([]rune(reply))
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(reply))).
				Msg("immersive reply sent")
		}
		recordReply(reply, "reply_action")
	}

	delay := time.Duration(b.cfg.ImmediateDelayMS) * time.Millisecond
	state.mu.Lock()
	state.inFlight = false
	state.waitRounds = 0
	pending := len(state.queue) > 0
	if state.timer != nil {
		state.timer.Stop()
	}
	if pending {
		state.timer = time.AfterFunc(delay, func() {
			b.flush(sessionKey)
		})
	}
	state.mu.Unlock()
	if pending {
		log.Info().
			Str("session", sessionKey).
			Int64("next_delay_ms", delay.Milliseconds()).
			Msg("pending messages detected, flushing again")
	}
}

func isControlHeaderProtocolError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errControlHeaderProtocol) {
		return true
	}
	if errors.Is(err, errControlHeaderTooLong) ||
		errors.Is(err, errControlHeaderMissingNewline) ||
		errors.Is(err, errControlHeaderInvalid) ||
		errors.Is(err, errControlHeaderUnexpectedContent) {
		return true
	}
	return false
}

// session retrieves or creates the session state for the given session key.
func (b *ImmersiveBuffer) session(sessionKey string) *immersiveSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = make(map[string]*immersiveSession)
	}
	if state, ok := b.sessions[sessionKey]; ok {
		return state
	}
	state := &immersiveSession{}
	b.sessions[sessionKey] = state
	return state
}

func (b *ImmersiveBuffer) recordAssistantUtterance(sessionKey, text string) {
	if strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(text) == "" {
		return
	}
	b.RecordTimelineEvent(sessionKey, text, b.botPrimaryName())
}

// RecordTimelineEvent appends an event to session timeline without queuing or flushing.
func (b *ImmersiveBuffer) RecordTimelineEvent(sessionKey, text, speaker string) {
	if b == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(text) == "" {
		return
	}
	state := b.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	msg := queuedMessage{
		text:    strings.TrimSpace(text),
		speaker: strings.TrimSpace(speaker),
		ts:      time.Now(),
		chars:   len([]rune(strings.TrimSpace(text))),
	}
	state.timeline = appendTimelineMessage(state.timeline, msg, b.timelineLimit())
}

func (b *ImmersiveBuffer) botPrimaryName() string {
	identity := b.currentIdentity()
	for _, name := range identity.ConfigNicknames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			return trimmed
		}
	}
	if trimmed := strings.TrimSpace(identity.AccountNickname); trimmed != "" {
		return trimmed
	}
	return "bot"
}

// RefreshIdentityFromCtx updates runtime bot identity data from event context.
// It captures account nickname and account IDs from both event and get_login_info.
func (b *ImmersiveBuffer) RefreshIdentityFromCtx(ctx *zero.Ctx) {
	if b == nil || ctx == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	updated := b.identity
	updated.ConfigNicknames = normalizedBotNames(b.nicknames)
	if ctx.Event != nil {
		if ctx.Event.SelfID != 0 {
			updated.AccountIDs = append(updated.AccountIDs, strconv.FormatInt(ctx.Event.SelfID, 10))
		}
		if strings.TrimSpace(ctx.Event.SelfTinyID) != "" {
			updated.AccountIDs = append(updated.AccountIDs, strings.TrimSpace(ctx.Event.SelfTinyID))
		}
	}
	info := ctx.GetLoginInfo()
	if nick := strings.TrimSpace(info.Get("nickname").String()); nick != "" {
		updated.AccountNickname = nick
	}
	if id := strings.TrimSpace(info.Get("user_id").String()); id != "" {
		updated.AccountIDs = append(updated.AccountIDs, id)
	}
	updated.AccountIDs = normalizedBotNames(updated.AccountIDs)
	b.identity = updated
}

func (b *ImmersiveBuffer) currentIdentity() botIdentity {
	b.mu.Lock()
	defer b.mu.Unlock()
	current := b.identity
	configNames := append([]string{}, b.nicknames...)
	configNames = append(configNames, current.ConfigNicknames...)
	current.ConfigNicknames = normalizedBotNames(configNames)
	current.AccountIDs = normalizedBotNames(current.AccountIDs)
	return current
}

func (b *ImmersiveBuffer) timelineLimit() int {
	return b.cfg.Timeline.MaxMessages + b.cfg.Timeline.OverflowMessages
}
