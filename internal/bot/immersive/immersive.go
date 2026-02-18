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
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

// Default configuration values for the immersive buffer.
const (
	defaultMaxBatchMessages         = 10
	defaultMaxBatchChars            = 1200
	defaultImmediateDelayMS         = 120
	defaultSpeakJudgeTimeoutMS      = 1200
	defaultTimelineMaxMessages      = 200
	defaultTimelineOverflowMessages = 50
	defaultTimelineCompressBatch    = 100
	timelineFallbackSummaryLen      = 1000
	defaultContinuousMinChars       = 12
	defaultContinuousMaxChars       = 80
	defaultContinuousMinMS          = 300
	defaultContinuousMaxMS          = 900
	maxPreGenerateRegensPerRound    = 3
	preGenerateWaitTimeout          = 35 * time.Second
)

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

	b.startOrRestartPreGenerate(sessionKey, false)
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
	state.timelineSummary = ""
	state.waitRounds = 0
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	cancel := b.resetPreGenerateLocked(state)
	state.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	log.Info().Str("session", sessionKey).Msg("immersive session buffer cleared")
}

// flush processes the queued messages for a session. It evaluates the speak gate,
// optionally calls the LLM to judge if a response should be made, and sends a reply
// if appropriate. If the decision is to wait, it re-queues the messages for later.
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
		cancel := b.resetPreGenerateLocked(state)
		state.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	queue := make([]queuedMessage, len(state.queue))
	copy(queue, state.queue)
	timeline := make([]queuedMessage, len(state.timeline))
	copy(timeline, state.timeline)
	timelineSnapshotLen := len(timeline)
	timelineSummary := strings.TrimSpace(state.timelineSummary)
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

	timeline, timelineSummary = b.compactTimelineSnapshot(timeline, timelineSummary)
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
	state.timelineSummary = timelineSummary
	state.mu.Unlock()
	input := buildCombinedInputWithSummary(timeline, timelineSummary, b.currentIdentity())
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
		cancel := b.resetPreGenerateLocked(state)
		state.inFlight = false
		state.waitRounds = 0
		pending := len(state.queue) > 0
		state.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if pending {
			log.Info().Str("session", sessionKey).Msg("pending messages detected, flushing again")
			b.flush(sessionKey)
		}
		return
	}
	gate := b.shouldSpeak(state, queue, input)
	log.Info().
		Str("session", sessionKey).
		Bool("should_speak", gate.shouldSpeak).
		Str("assistant_status", gate.assistantStatus).
		Int("mentions_to_bot", gate.mentionsToBot).
		Int("addressed_to_bot", gate.addressedToBot).
		Int("questions_count", gate.questionsCount).
		Int("directed_questions", gate.directedQuestions).
		Int("messages_count", gate.messagesCount).
		Int("participants_count", gate.participantsCount).
		Int("wait_ms", gate.waitMS).
		Int("wait_rounds", waitRounds).
		Str("suppress_reason", gate.reason).
		Msg("immersive speak gate evaluated")
	if gate.waitMS > 0 {
		waitDelay := time.Duration(gate.waitMS) * time.Millisecond
		state.mu.Lock()
		cancel := b.resetPreGenerateLocked(state)
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
		if cancel != nil {
			cancel()
		}
		log.Info().
			Str("session", sessionKey).
			Int64("next_wait_ms", waitDelay.Milliseconds()).
			Int("wait_rounds", waitRounds+1).
			Str("assistant_status", gate.assistantStatus).
			Str("suppress_reason", gate.reason).
			Msg("immersive flush deferred by speak gate wait")
		return
	}
	if !gate.shouldSpeak {
		delay := time.Duration(b.cfg.ImmediateDelayMS) * time.Millisecond
		state.mu.Lock()
		cancel := b.resetPreGenerateLocked(state)
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
		if cancel != nil {
			cancel()
		}
		log.Info().
			Str("session", sessionKey).
			Int64("next_delay_ms", delay.Milliseconds()).
			Bool("pending_messages", pending).
			Bool("should_speak", false).
			Int("wait_ms", gate.waitMS).
			Str("assistant_status", gate.assistantStatus).
			Str("suppress_reason", gate.reason).
			Msg("immersive flush skipped by speak gate")
		return
	}
	if input != "" {
		reply, err := b.awaitPreGeneratedReply(sessionKey, input)
		if err != nil {
			log.Error().
				Err(err).
				Str("session", sessionKey).
				Msg("immersive pre-generated reply unavailable")
		} else if strings.TrimSpace(reply) == "" {
			log.Warn().
				Str("session", sessionKey).
				Msg("immersive pre-generated reply is empty")
		} else if ctx == nil {
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(reply))).
				Msg("immersive reply prepared without ctx")
		} else if b.cfg.ContinuousSpeech.Enabled {
			b.sendBufferedContinuousReply(ctx, sessionKey, reply)
		} else {
			ctx.Send(reply)
			b.recordAssistantUtterance(sessionKey, reply)
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(reply))).
				Msg("immersive reply sent")
		}
	}

	state.mu.Lock()
	state.inFlight = false
	state.waitRounds = 0
	pending := len(state.queue) > 0
	state.mu.Unlock()
	if pending {
		log.Info().Str("session", sessionKey).Msg("pending messages detected, flushing again")
		b.flush(sessionKey)
	}
}

func (b *ImmersiveBuffer) sendBufferedContinuousReply(ctx *zero.Ctx, sessionKey, reply string) {
	acc := newStreamChunkAccumulator(b.cfg.ContinuousSpeech)
	sentMessages := 0
	sentChars := 0
	chunks := acc.Append(reply)
	for _, chunk := range chunks {
		if sentMessages > 0 {
			time.Sleep(nextContinuousSpeechDelay(b.cfg.ContinuousSpeech))
		}
		ctx.Send(chunk)
		sentMessages++
		sentChars += len([]rune(chunk))
	}
	for _, chunk := range acc.FlushTail() {
		if sentMessages > 0 {
			time.Sleep(nextContinuousSpeechDelay(b.cfg.ContinuousSpeech))
		}
		ctx.Send(chunk)
		sentMessages++
		sentChars += len([]rune(chunk))
	}
	log.Info().
		Str("session", sessionKey).
		Int("reply_chars", len([]rune(reply))).
		Int("reply_messages", sentMessages).
		Int("sent_chars", sentChars).
		Msg("immersive continuous reply sent")
	b.recordAssistantUtterance(sessionKey, reply)
}

func (b *ImmersiveBuffer) startOrRestartPreGenerate(sessionKey string, force bool) {
	state := b.session(sessionKey)
	state.mu.Lock()
	input := b.buildPreGenerateInputLocked(state)
	state.mu.Unlock()
	b.startOrRestartPreGenerateWithInput(sessionKey, input, force)
}

func (b *ImmersiveBuffer) startOrRestartPreGenerateWithInput(sessionKey, input string, force bool) {
	trimmedInput := strings.TrimSpace(input)
	state := b.session(sessionKey)
	state.mu.Lock()
	if trimmedInput == "" {
		cancel := b.resetPreGenerateLocked(state)
		state.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	if !b.llm.IsEnabled() || !b.llm.IsImmersive(sessionKey) {
		cancel := b.resetPreGenerateLocked(state)
		state.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	if state.pregen.running && !force && state.pregen.regenCount >= maxPreGenerateRegensPerRound {
		regenCount := state.pregen.regenCount
		state.mu.Unlock()
		log.Info().
			Str("session", sessionKey).
			Int("regen_count", regenCount).
			Int("regen_limit", maxPreGenerateRegensPerRound).
			Msg("immersive pre-generate restart skipped due to limit")
		return
	}
	var cancelPrev context.CancelFunc
	regenCount := 0
	if state.pregen.running {
		cancelPrev = state.pregen.cancel
		regenCount = state.pregen.regenCount
		if !force {
			regenCount++
		}
	}
	state.pregen.version++
	version := state.pregen.version
	done := make(chan struct{})
	taskCtx, cancelTask := context.WithCancel(context.Background())
	state.pregen.input = trimmedInput
	state.pregen.reply = ""
	state.pregen.err = nil
	state.pregen.done = done
	state.pregen.cancel = cancelTask
	state.pregen.running = true
	state.pregen.regenCount = regenCount
	state.mu.Unlock()

	if cancelPrev != nil {
		cancelPrev()
	}
	go b.runPreGenerate(sessionKey, version, trimmedInput, taskCtx, done)
}

func (b *ImmersiveBuffer) runPreGenerate(sessionKey string, version uint64, input string, taskCtx context.Context, done chan struct{}) {
	defer close(done)
	reply, err := b.llm.Reply(taskCtx, input, sessionKey, "")
	state := b.session(sessionKey)
	state.mu.Lock()
	if state.pregen.version == version {
		state.pregen.running = false
		state.pregen.cancel = nil
		state.pregen.reply = reply
		state.pregen.err = err
	}
	state.mu.Unlock()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Info().Str("session", sessionKey).Msg("immersive pre-generate canceled")
			return
		}
		log.Warn().Err(err).Str("session", sessionKey).Msg("immersive pre-generate failed")
		return
	}
	log.Info().
		Str("session", sessionKey).
		Int("reply_chars", len([]rune(reply))).
		Msg("immersive pre-generate completed")
}

func (b *ImmersiveBuffer) awaitPreGeneratedReply(sessionKey, input string) (string, error) {
	startedAt := time.Now()
	trimmedInput := strings.TrimSpace(input)
	for {
		state := b.session(sessionKey)
		state.mu.Lock()
		if strings.TrimSpace(state.pregen.input) != trimmedInput || state.pregen.done == nil {
			state.mu.Unlock()
			b.startOrRestartPreGenerateWithInput(sessionKey, trimmedInput, true)
			continue
		}
		if state.pregen.running {
			done := state.pregen.done
			state.mu.Unlock()
			remaining := preGenerateWaitTimeout - time.Since(startedAt)
			if remaining <= 0 {
				b.cancelPreGenerate(sessionKey)
				return "", fmt.Errorf("wait pre-generated reply timeout after %s", preGenerateWaitTimeout)
			}
			timer := time.NewTimer(remaining)
			select {
			case <-done:
				timer.Stop()
				continue
			case <-timer.C:
				b.cancelPreGenerate(sessionKey)
				return "", fmt.Errorf("wait pre-generated reply timeout after %s", preGenerateWaitTimeout)
			}
		}
		reply := state.pregen.reply
		err := state.pregen.err
		state.mu.Unlock()
		return reply, err
	}
}

func (b *ImmersiveBuffer) cancelPreGenerate(sessionKey string) {
	state := b.session(sessionKey)
	state.mu.Lock()
	cancel := b.resetPreGenerateLocked(state)
	state.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *ImmersiveBuffer) resetPreGenerateLocked(state *immersiveSession) context.CancelFunc {
	cancel := state.pregen.cancel
	state.pregen = preGenerateState{}
	return cancel
}

func (b *ImmersiveBuffer) buildPreGenerateInputLocked(state *immersiveSession) string {
	timeline := make([]queuedMessage, len(state.timeline))
	copy(timeline, state.timeline)
	queue := make([]queuedMessage, len(state.queue))
	copy(queue, state.queue)
	summary := strings.TrimSpace(state.timelineSummary)
	input := buildCombinedInputWithSummary(timeline, summary, b.currentIdentity())
	if strings.TrimSpace(input) == "" {
		input = buildCombinedInput(queue, b.currentIdentity())
	}
	return strings.TrimSpace(input)
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

// shouldSpeak evaluates whether the bot should speak based on the queued messages
// and the configured speak gate. It may call the LLM to make the decision.
func (b *ImmersiveBuffer) shouldSpeak(state *immersiveSession, queue []queuedMessage, judgeInput string) speakGateResult {
	_ = state
	meta := summarizeQueueMeta(queue, time.Now(), b.currentIdentity())
	reasons := make([]string, 0, 4)
	directedQuestions := 0
	for _, msg := range queue {
		if msg.isQuestion && msg.isAddressedToBot {
			directedQuestions++
		}
	}
	should := true
	assistantStatus := "not_enabled"
	if b.llm != nil && b.cfg.SpeakGate.Enabled {
		assistantStatus = "enabled"
		assistantDecision, judged, err := b.llm.JudgeSpeakGate(
			context.Background(),
			judgeInput,
		)
		if err != nil {
			if b.cfg.SpeakGate.FailOpen {
				should = true
				reasons = append(reasons, "assistant_error_fail_open_reply")
				assistantStatus = "error_allow"
			} else {
				should = false
				reasons = append(reasons, "assistant_error_block_skip")
				assistantStatus = "error_block"
			}
		} else if judged {
			switch assistantDecision.Action {
			case llm.SpeakGateActionReply:
				should = true
				reasons = append(reasons, "assistant_reply")
				assistantStatus = "reply"
			case llm.SpeakGateActionSkip:
				should = false
				reasons = append(reasons, "assistant_skip")
				assistantStatus = "skip"
			case llm.SpeakGateActionWait:
				should = false
				reasons = append(reasons, fmt.Sprintf("assistant_wait_%dms", assistantDecision.WaitMS))
				assistantStatus = "wait"
				return speakGateResult{
					shouldSpeak:       false,
					waitMS:            assistantDecision.WaitMS,
					reason:            strings.Join(reasons, ","),
					assistantStatus:   assistantStatus,
					mentionsToBot:     meta.MentionsToBot,
					addressedToBot:    meta.AddressedToBot,
					questionsCount:    meta.QuestionsCount,
					directedQuestions: directedQuestions,
					messagesCount:     meta.MessagesCount,
					participantsCount: len(meta.Participants),
				}
			default:
				if b.cfg.SpeakGate.FailOpen {
					should = true
					reasons = append(reasons, "assistant_unknown_fail_open_reply")
					assistantStatus = "unknown_allow"
				} else {
					should = false
					reasons = append(reasons, "assistant_unknown_block_skip")
					assistantStatus = "unknown_block"
				}
			}
		} else if b.cfg.SpeakGate.FailOpen {
			should = true
			reasons = append(reasons, "assistant_unjudged_fail_open_reply")
			assistantStatus = "unjudged_allow"
		} else {
			should = false
			reasons = append(reasons, "assistant_unjudged_block_skip")
			assistantStatus = "unjudged_block"
		}
	} else {
		reasons = append(reasons, "assistant_not_enabled_allow_reply")
	}
	return speakGateResult{
		shouldSpeak:       should,
		waitMS:            0,
		reason:            strings.Join(reasons, ","),
		assistantStatus:   assistantStatus,
		mentionsToBot:     meta.MentionsToBot,
		addressedToBot:    meta.AddressedToBot,
		questionsCount:    meta.QuestionsCount,
		directedQuestions: directedQuestions,
		messagesCount:     meta.MessagesCount,
		participantsCount: len(meta.Participants),
	}
}

func (b *ImmersiveBuffer) recordAssistantUtterance(sessionKey, text string) {
	if strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(text) == "" {
		return
	}
	state := b.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	msg := queuedMessage{
		text:    strings.TrimSpace(text),
		speaker: b.botPrimaryName(),
		ts:      time.Now(),
		chars:   len([]rune(strings.TrimSpace(text))),
	}
	state.timeline = appendTimelineMessage(state.timeline, msg, b.timelineLimit())
}

func (b *ImmersiveBuffer) compactTimelineSnapshot(timeline []queuedMessage, summary string) ([]queuedMessage, string) {
	trimmedSummary := strings.TrimSpace(summary)
	working := make([]queuedMessage, len(timeline))
	copy(working, timeline)
	changed := false
	maxMessages := b.cfg.Timeline.MaxMessages
	compressBatch := b.cfg.Timeline.CompressBatch
	limit := b.timelineLimit()
	for len(working) >= maxMessages {
		if len(working) < compressBatch {
			break
		}
		chunk := make([]queuedMessage, compressBatch)
		copy(chunk, working[:compressBatch])
		nextSummary := b.summarizeTimelineChunk(trimmedSummary, chunk)
		if strings.TrimSpace(nextSummary) == "" {
			// Keep recent context if summarization is unavailable.
			break
		}
		trimmedSummary = strings.TrimSpace(nextSummary)
		working = working[compressBatch:]
		changed = true
	}
	if !changed && len(working) <= limit {
		return working, trimmedSummary
	}
	working = trimTimelineTail(working, limit)
	return working, trimmedSummary
}

func (b *ImmersiveBuffer) summarizeTimelineChunk(previousSummary string, chunk []queuedMessage) string {
	if len(chunk) == 0 {
		return strings.TrimSpace(previousSummary)
	}
	messages := make([]llm.Message, 0, len(chunk))
	botName := strings.ToLower(strings.TrimSpace(b.botPrimaryName()))
	for _, msg := range chunk {
		content := strings.TrimSpace(msg.text)
		if content == "" {
			continue
		}
		role := "user"
		speaker := strings.TrimSpace(msg.speaker)
		if speaker != "" && strings.ToLower(speaker) == botName {
			role = "assistant"
		}
		text := content
		if speaker != "" {
			text = "[" + speaker + "]: " + content
		}
		messages = append(messages, llm.Message{Role: role, Content: text})
	}
	if len(messages) == 0 {
		return strings.TrimSpace(previousSummary)
	}
	if b.llm != nil {
		if summary := b.llm.SummarizeImmersiveTimeline(context.Background(), previousSummary, messages); strings.TrimSpace(summary) != "" {
			return strings.TrimSpace(summary)
		}
	}
	return buildTimelineFallbackSummary(previousSummary, chunk, timelineFallbackSummaryLen)
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
