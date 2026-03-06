package immersive

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

// Default configuration values for the immersive buffer.
const (
	defaultRuntimeBufferMaxMessages  = 200
	defaultPokeReactionWindowMS      = 180000
	defaultPokeReactionMildThresh    = 3
	defaultPokeReactionAnnoyedThresh = 6
	maxImmersiveWaitRounds           = 2
	defaultProtocolErrorWaitMS       = 600
	defaultPendingFlushDelayMS       = 120
	immersiveEmptyReplyFallback      = "..."
	immersiveLogPreviewChars         = 600
)

// NewImmersiveBuffer creates a new ImmersiveBuffer with the given configuration,
// LLM manager, and bot nicknames.
func NewImmersiveBuffer(cfg config.ImmersiveConfig, llmManager llm.Service, nicknames []string) *ImmersiveBuffer {
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
func NewEngine(cfg config.ImmersiveConfig, llmManager llm.Service, nicknames []string) *ImmersiveBuffer {
	return NewImmersiveBuffer(cfg, llmManager, nicknames)
}

// ReloadConfig refreshes immersive policies without clearing runtime buffers.
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
	state.nextBatch = append(state.nextBatch, msg)
	state.nextBatchChars += charCount
	state.runtimeBuffer = appendTimelineMessage(state.runtimeBuffer, msg, b.runtimeBufferLimit())

	// Cooldown is intentionally disabled: flush immediately after enqueue.
	cooldown := time.Duration(0)
	queueSnapshot := make([]queuedMessage, len(state.nextBatch))
	copy(queueSnapshot, state.nextBatch)
	log.Info().
		Str("session", sessionKey).
		Bool("is_private", isPrivate).
		Bool("mention", mention).
		Bool("addressed", addressed).
		Bool("question", question).
		Int("queue_len", len(queueSnapshot)).
		Int("queue_chars", state.nextBatchChars).
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
	state.nextBatch = nil
	state.nextBatchChars = 0
	state.processingBatch = nil
	state.runtimeBuffer = nil
	state.waitRounds = 0
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.mu.Unlock()
	log.Info().Str("session", sessionKey).Msg("immersive session buffer cleared")
}

// flush processes queued messages through a two-phase flow:
// 1) control intent decision (skip/wait/reply), then
// 2) reply generation when action=reply.
func (b *ImmersiveBuffer) flush(sessionKey string) {
	state := b.session(sessionKey)
	state.mu.Lock()
	if state.inFlight {
		if len(state.nextBatch) > 0 {
			delay := time.Duration(defaultPendingFlushDelayMS) * time.Millisecond
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
	if len(state.nextBatch) == 0 {
		state.mu.Unlock()
		return
	}
	if !b.llm.IsEnabled() || !b.llm.IsImmersive(sessionKey) {
		state.nextBatch = nil
		state.nextBatchChars = 0
		state.processingBatch = nil
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		state.mu.Unlock()
		return
	}
	processing := make([]queuedMessage, len(state.nextBatch))
	copy(processing, state.nextBatch)
	state.processingBatch = make([]queuedMessage, len(processing))
	copy(state.processingBatch, processing)
	state.nextBatch = nil
	state.nextBatchChars = 0
	state.inFlight = true
	waitRounds := state.waitRounds
	ctx := state.lastCtx
	state.mu.Unlock()

	log.Info().
		Str("session", sessionKey).
		Int("batch_messages", len(processing)).
		Int("batch_chars", sumQueueChars(processing)).
		Int("wait_rounds", waitRounds).
		Msg("immersive flush started")

	var cutoffSeq int64
	for i := range processing {
		if processing[i].persisted {
			if processing[i].causalSeq > cutoffSeq {
				cutoffSeq = processing[i].causalSeq
			}
			continue
		}
		seq, ok := b.llm.AppendUserEventAt(sessionKey, processing[i].text, processing[i].speaker, processing[i].ts)
		if !ok {
			log.Warn().
				Str("session", sessionKey).
				Str("speaker", processing[i].speaker).
				Msg("immersive user event append skipped")
			continue
		}
		processing[i].persisted = true
		processing[i].causalSeq = seq
		if seq > cutoffSeq {
			cutoffSeq = seq
		}
	}

	state.mu.Lock()
	state.processingBatch = make([]queuedMessage, len(processing))
	copy(state.processingBatch, processing)
	runtimeSnapshot := make([]queuedMessage, len(state.runtimeBuffer))
	copy(runtimeSnapshot, state.runtimeBuffer)
	state.mu.Unlock()

	identity := b.currentIdentity()
	b.llm.SetAssistantSpeaker(assistantSpeakerLabel(identity))
	timelineSlice := trimTimelineTail(runtimeSnapshot, b.runtimeBufferLimit())
	input := buildCombinedInput(timelineSlice, identity)
	if strings.TrimSpace(input) == "" {
		timelineSlice = processing
		input = buildCombinedInput(processing, identity)
	}
	immersiveCtx := buildImmersiveContext(timelineSlice, identity)
	repeatText, repeatCount, repeatParticipants := detectConsecutiveRepeat(processing)
	if repeatText != "" && ctx != nil {
		b.sendTracked(ctx, repeatText)
		b.recordAssistantUtterance(sessionKey, repeatText)
		_ = b.llm.AppendAssistantEvent(sessionKey, repeatText, cutoffSeq)
		log.Info().
			Str("session", sessionKey).
			Str("repeat_text", repeatText).
			Int("repeat_count", repeatCount).
			Int("repeat_participants", repeatParticipants).
			Msg("immersive repeat triggered")
		state.mu.Lock()
		state.inFlight = false
		state.processingBatch = nil
		state.waitRounds = 0
		pending := len(state.nextBatch) > 0
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
		state.processingBatch = nil
		state.waitRounds = 0
		pending := len(state.nextBatch) > 0
		state.mu.Unlock()
		if pending {
			b.flush(sessionKey)
		}
		return
	}
	log.Info().
		Str("session", sessionKey).
		Int("input_chars", len([]rune(input))).
		Str("input_preview", previewForLog(input, immersiveLogPreviewChars)).
		Msg("immersive control intent input prepared")

	recordReply := func(reply, reason string) {
		trimmed := strings.TrimSpace(reply)
		if trimmed == "" {
			return
		}
		b.recordAssistantUtterance(sessionKey, trimmed)
		_ = b.llm.AppendAssistantEvent(sessionKey, trimmed, cutoffSeq)
		log.Info().
			Str("session", sessionKey).
			Str("reason", reason).
			Int("reply_chars", len([]rune(trimmed))).
			Int64("reply_to_cutoff_seq", cutoffSeq).
			Msg("immersive reply recorded into runtime buffer and llm history")
	}

	intent, intentErr := b.llm.DecideImmersiveIntent(context.Background(), input, sessionKey, "", immersiveCtx)
	decision := decisionFromIntent(intent)
	action := decision.action
	waitMS := decision.waitMS
	decisionReason := decision.reason
	decisionReasonCode := "model"

	if intentErr != nil {
		if isControlIntentProtocolError(intentErr) {
			if waitRounds == 0 {
				action = controlActionWait
				waitMS = defaultProtocolErrorWaitMS
				decisionReasonCode = "protocol_error_wait"
				decisionReason = "control protocol invalid, wait once for retry"
			} else {
				action = controlActionSkip
				waitMS = 0
				decisionReasonCode = "protocol_error_skip"
				decisionReason = "control protocol invalid repeatedly, skip this round"
			}
		} else {
			action = controlActionSkip
			waitMS = 0
			decisionReasonCode = "intent_error_skip"
			decisionReason = "intent decision failed, skip this round"
		}
	}
	if action == controlActionWait && waitRounds >= maxImmersiveWaitRounds {
		action = controlActionSkip
		waitMS = 0
		decisionReasonCode = "wait_round_limit_skip"
		decisionReason = "wait round limit reached, skip this round"
	}
	if action == "" {
		action = controlActionSkip
		waitMS = 0
		decisionReasonCode = "empty_action_skip"
		decisionReason = "empty control action, skip this round"
	}
	if (action == controlActionWait || action == controlActionSkip) && strings.TrimSpace(decisionReason) == "" {
		decisionReasonCode = "missing_reason_fallback"
		decisionReason = "missing control reason, skip this round"
		action = controlActionSkip
		waitMS = 0
	}

	decisionLog := log.Info().
		Str("session", sessionKey).
		Str("action", string(action)).
		Int("wait_ms", waitMS).
		Int("wait_rounds", waitRounds).
		Str("reason", decisionReason).
		Str("reason_code", decisionReasonCode)
	if intentErr != nil {
		decisionLog = decisionLog.Err(intentErr)
	}
	decisionLog.Msg("immersive control decision evaluated")

	if action == controlActionWait {
		waitDelay := time.Duration(waitMS) * time.Millisecond
		state.mu.Lock()
		state.nextBatch = prependMessages(processing, state.nextBatch)
		state.nextBatchChars += sumQueueChars(processing)
		state.processingBatch = nil
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
			Str("reason", decisionReason).
			Str("reason_code", decisionReasonCode).
			Msg("immersive flush deferred by wait action")
		return
	}

	if action == controlActionReply {
		onReplyEvent := func(event llm.StreamEvent) error {
			switch event.Type {
			case llm.StreamEventToolCall, llm.StreamEventToolResult, llm.StreamEventFinal, llm.StreamEventError:
				log.Debug().
					Str("session", sessionKey).
					Int64("stream_seq", event.Seq).
					Int("stream_step", event.Step).
					Str("stream_event_type", string(event.Type)).
					Msg("immersive reply stream event")
			}
			return nil
		}

		replyCtx, cancelReply := context.WithCancel(context.Background())
		reply, replyErr := b.llm.ReplyStreamWithExtraPromptAllowTools(
			replyCtx,
			input,
			sessionKey,
			"",
			"",
			onReplyEvent,
			immersiveCtx,
		)
		cancelReply()
		replyLog := log.Info().
			Str("session", sessionKey).
			Int("output_chars", len([]rune(reply))).
			Str("output_first_line", firstLineForLog(reply)).
			Str("output_preview", previewForLog(reply, immersiveLogPreviewChars))
		if replyErr != nil {
			replyLog = replyLog.Err(replyErr)
		}
		replyLog.Msg("immersive reply llm raw output captured")
		if replyErr != nil {
			log.Warn().
				Str("session", sessionKey).
				Err(replyErr).
				Msg("immersive reply generation failed")
			goto finalizeState
		}

		reply = strings.TrimSpace(reply)
		if reply == "" {
			reply = immersiveEmptyReplyFallback
		}

		segments := SplitReplySegments(reply)
		if len(segments) == 0 {
			segments = []string{reply}
		}

		sentMessages := 0
		sentChars := 0
		replyParts := make([]string, 0, len(segments))
		for _, segment := range segments {
			trimmed := strings.TrimSpace(segment)
			if trimmed == "" {
				continue
			}
			replyParts = append(replyParts, trimmed)
			if ctx == nil {
				continue
			}
			if sentMessages > 0 {
				time.Sleep(NextReplySegmentDelay(trimmed))
			}
			b.sendTracked(ctx, trimmed)
			sentMessages++
			sentChars += len([]rune(trimmed))
		}
		finalReply := strings.TrimSpace(strings.Join(replyParts, "\n"))
		if finalReply == "" {
			finalReply = immersiveEmptyReplyFallback
		}

		if ctx == nil {
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(finalReply))).
				Msg("immersive reply prepared without ctx")
		} else {
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(finalReply))).
				Int("reply_messages", sentMessages).
				Int("sent_chars", sentChars).
				Msg("immersive reply sent")
		}
		recordReply(finalReply, "reply_action")
	}

finalizeState:
	delay := time.Duration(defaultPendingFlushDelayMS) * time.Millisecond
	state.mu.Lock()
	state.inFlight = false
	state.processingBatch = nil
	state.waitRounds = 0
	pending := len(state.nextBatch) > 0
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

// RecordTimelineEvent appends an event to the runtime buffer without queuing or flushing.
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
	state.runtimeBuffer = appendTimelineMessage(state.runtimeBuffer, msg, b.runtimeBufferLimit())
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

func assistantSpeakerLabel(identity botIdentity) string {
	name := "assistant"
	for _, candidate := range identity.ConfigNicknames {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			name = trimmed
			break
		}
	}
	if name == "assistant" {
		if nick := strings.TrimSpace(identity.AccountNickname); nick != "" {
			name = nick
		}
	}
	var id string
	for _, candidate := range identity.AccountIDs {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			id = trimmed
			break
		}
	}
	if id == "" {
		return "name=" + name
	}
	return "name=" + name + ";id=" + id
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

func (b *ImmersiveBuffer) runtimeBufferLimit() int {
	return b.cfg.RuntimeBuffer.MaxMessages
}

func previewForLog(text string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = immersiveLogPreviewChars
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + "...(truncated)"
}

func firstLineForLog(text string) string {
	if text == "" {
		return ""
	}
	line := text
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}
