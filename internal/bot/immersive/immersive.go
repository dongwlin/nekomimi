package immersive

import (
	"context"
	"math/rand"
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
	defaultCooldownMinMS            = 800
	defaultCooldownMaxMS            = 3500
	defaultCooldownBaseMS           = 1200
	defaultPrivateBaseMS            = 200
	defaultWindowMS                 = 5000
	defaultJitterMS                 = 200
	defaultMaxBatchMessages         = 10
	defaultMaxBatchChars            = 1200
	defaultImmediateDelayMS         = 120
	defaultPostShortWaitMS          = 1200
	defaultPostLongWaitMS           = 5000
	defaultPostMaxRounds            = 3
	defaultSpeakJudgeTimeoutMS      = 1200
	defaultTimelineMaxMessages      = 200
	defaultTimelineOverflowMessages = 50
	defaultTimelineCompressBatch    = 100
	timelineFallbackSummaryLen      = 1000
	defaultContinuousMinChars       = 12
	defaultContinuousMaxChars       = 80
	defaultContinuousMinMS          = 300
	defaultContinuousMaxMS          = 900

	activeMsgPenaltyMS  = 150
	activeCharPenaltyMS = 4
	shortMsgLen         = 12
	shortMsgPenaltyMS   = 200
	mentionBonusMS      = 400
)

// NewImmersiveBuffer creates a new ImmersiveBuffer with the given configuration,
// LLM manager, and bot nicknames.
func NewImmersiveBuffer(cfg config.ImmersiveConfig, llmManager *llm.Manager, nicknames []string) *ImmersiveBuffer {
	normalized := normalizeImmersiveConfig(cfg)
	rand.Seed(time.Now().UnixNano())
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

// Enqueue adds a new message to the session buffer and schedules a flush
// based on the calculated cooldown. It detects message signals (mentions,
// addressed to bot, questions) to determine response urgency.
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
	state.recent = append(state.recent, recentSample{ts: now, chars: charCount})
	state.recent = trimRecent(state.recent, now, b.cfg.WindowMS)

	recentCount, recentChars := summarizeRecent(state.recent)
	cooldown := b.calcCooldown(isPrivate, addressed, question, recentCount, recentChars, charCount)
	queueSnapshot := make([]queuedMessage, len(state.queue))
	copy(queueSnapshot, state.queue)

	judgeEnabled := mention && !isPrivate && b.cfg.MentionJudge.Enabled
	if (addressed || question) && !judgeEnabled {
		cooldown = minDuration(cooldown, time.Duration(b.cfg.ImmediateDelayMS)*time.Millisecond)
	}
	log.Info().
		Str("session", sessionKey).
		Bool("is_private", isPrivate).
		Bool("mention", mention).
		Bool("addressed", addressed).
		Bool("question", question).
		Int("queue_len", len(queueSnapshot)).
		Int("queue_chars", state.queueChars).
		Int("recent_count", recentCount).
		Int("recent_chars", recentChars).
		Int64("cooldown_ms", cooldown.Milliseconds()).
		Msg("immersive enqueue scheduled")
	state.mu.Unlock()

	if judgeEnabled {
		preview := buildRecentPreview(queueSnapshot, 4, b.currentIdentity())
		immediate, err := b.llm.JudgeMentionImmediate(context.Background(), trimmed, speaker, preview)
		if err == nil && immediate {
			cooldown = minDuration(cooldown, time.Duration(b.cfg.ImmediateDelayMS)*time.Millisecond)
			log.Info().
				Str("session", sessionKey).
				Int64("cooldown_ms", cooldown.Milliseconds()).
				Msg("mention judge requested immediate reply")
		} else if err != nil {
			log.Warn().
				Err(err).
				Str("session", sessionKey).
				Msg("mention judge failed")
		}
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.queueChars >= b.cfg.MaxBatchChars || len(state.queue) >= b.cfg.MaxBatchMessages {
		cooldown = minDuration(cooldown, 200*time.Millisecond)
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.timer = time.AfterFunc(cooldown, func() {
		b.flush(sessionKey)
	})
}

// Clear removes all queued messages and resets the session state for the given session key.
func (b *ImmersiveBuffer) Clear(sessionKey string) {
	if b == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	state := b.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.queue = nil
	state.queueChars = 0
	state.timeline = nil
	state.timelineSummary = ""
	state.recent = nil
	state.postRounds = 0
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
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
		state.mu.Unlock()
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
	postRounds := state.postRounds
	ctx := state.lastCtx
	state.mu.Unlock()
	log.Info().
		Str("session", sessionKey).
		Int("batch_messages", len(queue)).
		Int("batch_chars", sumQueueChars(queue)).
		Int("post_rounds", postRounds).
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
		state.inFlight = false
		state.postRounds = 0
		pending := len(state.queue) > 0
		state.mu.Unlock()
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
		Str("suppress_reason", gate.reason).
		Msg("immersive speak gate evaluated")
	if !gate.shouldSpeak {
		cooldownDelay := time.Duration(b.cfg.PostCooldownJudge.ShortWaitMS) * time.Millisecond
		if cooldownDelay <= 0 {
			cooldownDelay = time.Duration(b.cfg.ImmediateDelayMS) * time.Millisecond
		}
		state.mu.Lock()
		state.inFlight = false
		state.postRounds = 0
		pending := len(state.queue) > 0
		if state.timer != nil {
			state.timer.Stop()
		}
		if pending {
			state.timer = time.AfterFunc(cooldownDelay, func() {
				b.flush(sessionKey)
			})
		}
		state.mu.Unlock()
		log.Info().
			Str("session", sessionKey).
			Int64("next_cooldown_ms", cooldownDelay.Milliseconds()).
			Bool("pending_messages", pending).
			Bool("should_speak", false).
			Str("assistant_status", gate.assistantStatus).
			Str("suppress_reason", gate.reason).
			Msg("immersive flush skipped by speak gate")
		return
	}
	decision := llm.DecisionReplyNow
	cooldownDelay := time.Duration(0)
	// MaxRounds == 0 means unlimited cooldown rounds.
	canJudgePostCooldown := b.cfg.PostCooldownJudge.MaxRounds == 0 || postRounds < b.cfg.PostCooldownJudge.MaxRounds
	if input != "" && b.cfg.PostCooldownJudge.Enabled && canJudgePostCooldown {
		lastSpeaker := queue[len(queue)-1].speaker
		recent := buildRecentPreview(timeline, 8, b.currentIdentity())
		judgeDecision, err := b.llm.JudgePostCooldown(context.Background(), input, lastSpeaker, recent)
		if err == nil {
			decision = judgeDecision
			log.Info().
				Str("session", sessionKey).
				Str("decision", string(decision)).
				Bool("should_speak", gate.shouldSpeak).
				Str("assistant_status", gate.assistantStatus).
				Str("suppress_reason", gate.reason).
				Int("post_rounds", postRounds).
				Msg("post-cooldown judge decided")
		} else if !b.cfg.PostCooldownJudge.FailOpen {
			decision = llm.DecisionCooldownShort
			log.Warn().
				Err(err).
				Str("session", sessionKey).
				Bool("fail_open", false).
				Msg("post-cooldown judge failed, fallback to short cooldown")
		} else {
			log.Warn().
				Err(err).
				Str("session", sessionKey).
				Bool("fail_open", true).
				Msg("post-cooldown judge failed, continue reply")
		}
		if decision == llm.DecisionCooldownShort {
			cooldownDelay = time.Duration(b.cfg.PostCooldownJudge.ShortWaitMS) * time.Millisecond
		}
		if decision == llm.DecisionCooldownLong {
			cooldownDelay = time.Duration(b.cfg.PostCooldownJudge.LongWaitMS) * time.Millisecond
		}
	}
	if decision != llm.DecisionReplyNow {
		state.mu.Lock()
		state.queue = prependMessages(queue, state.queue)
		state.queueChars += sumQueueChars(queue)
		state.inFlight = false
		state.postRounds++
		if state.timer != nil {
			state.timer.Stop()
		}
		if cooldownDelay <= 0 {
			cooldownDelay = time.Duration(b.cfg.PostCooldownJudge.ShortWaitMS) * time.Millisecond
		}
		state.timer = time.AfterFunc(cooldownDelay, func() {
			b.flush(sessionKey)
		})
		state.mu.Unlock()
		log.Info().
			Str("session", sessionKey).
			Str("decision", string(decision)).
			Bool("should_speak", gate.shouldSpeak).
			Str("assistant_status", gate.assistantStatus).
			Str("suppress_reason", gate.reason).
			Int64("next_cooldown_ms", cooldownDelay.Milliseconds()).
			Msg("immersive flush deferred by post-cooldown judge")
		return
	}
	if input != "" {
		if ctx == nil {
			reply, err := b.llm.Reply(context.Background(), input, sessionKey, "")
			if err != nil {
				log.Error().
					Err(err).
					Str("session", sessionKey).
					Msg("immersive reply failed")
			} else {
				log.Info().
					Str("session", sessionKey).
					Int("reply_chars", len([]rune(reply))).
					Msg("immersive reply generated without ctx")
			}
		} else if b.cfg.ContinuousSpeech.Enabled {
			if err := b.sendContinuousReply(ctx, sessionKey, input); err != nil {
				log.Error().
					Err(err).
					Str("session", sessionKey).
					Msg("immersive continuous reply failed")
				ctx.Send("LLM调用失败: " + llm.UserVisibleError(err))
			}
		} else {
			reply, err := b.llm.Reply(context.Background(), input, sessionKey, "")
			if err != nil {
				log.Error().
					Err(err).
					Str("session", sessionKey).
					Msg("immersive reply failed")
				ctx.Send("LLM调用失败: " + llm.UserVisibleError(err))
			} else {
				ctx.Send(reply)
				b.recordAssistantUtterance(sessionKey, reply)
				log.Info().
					Str("session", sessionKey).
					Int("reply_chars", len([]rune(reply))).
					Msg("immersive reply sent")
			}
		}
	}

	state.mu.Lock()
	state.inFlight = false
	state.postRounds = 0
	pending := len(state.queue) > 0
	state.mu.Unlock()
	if pending {
		log.Info().Str("session", sessionKey).Msg("pending messages detected, flushing again")
		b.flush(sessionKey)
	}
}

func (b *ImmersiveBuffer) sendContinuousReply(ctx *zero.Ctx, sessionKey, input string) error {
	acc := newStreamChunkAccumulator(b.cfg.ContinuousSpeech)
	sentMessages := 0
	sentChars := 0
	reply, err := b.llm.ReplyStream(context.Background(), input, sessionKey, "", func(delta string) error {
		chunks := acc.Append(delta)
		for _, chunk := range chunks {
			if sentMessages > 0 {
				time.Sleep(nextContinuousSpeechDelay(b.cfg.ContinuousSpeech))
			}
			ctx.Send(chunk)
			sentMessages++
			sentChars += len([]rune(chunk))
		}
		return nil
	})
	if err != nil {
		if !b.cfg.ContinuousSpeech.RequireStream {
			fallbackReply, fallbackErr := b.llm.Reply(context.Background(), input, sessionKey, "")
			if fallbackErr != nil {
				return fallbackErr
			}
			ctx.Send(fallbackReply)
			b.recordAssistantUtterance(sessionKey, fallbackReply)
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(fallbackReply))).
				Msg("immersive continuous reply fallback sent")
			return nil
		}
		return err
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
	return nil
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
				reasons = append(reasons, "assistant_error_fail_open_allow(override_yes)")
				assistantStatus = "error_allow"
			} else {
				should = false
				reasons = append(reasons, "assistant_error_block(override_no)")
				assistantStatus = "error_block"
			}
		} else if judged {
			should = assistantDecision
			if assistantDecision {
				reasons = append(reasons, "assistant_yes(override)")
				assistantStatus = "yes"
			} else {
				reasons = append(reasons, "assistant_no(override)")
				assistantStatus = "no"
			}
		} else if b.cfg.SpeakGate.FailOpen {
			should = true
			reasons = append(reasons, "assistant_unjudged_fail_open_allow(override_yes)")
			assistantStatus = "unjudged_allow"
		} else {
			should = false
			reasons = append(reasons, "assistant_unjudged_block(override_no)")
			assistantStatus = "unjudged_block"
		}
	} else {
		reasons = append(reasons, "assistant_not_enabled_allow(default_yes)")
	}
	return speakGateResult{
		shouldSpeak:       should,
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
