package buffer

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

// Default configuration values for the immersive buffer.
const (
	defaultCooldownMinMS       = 800
	defaultCooldownMaxMS       = 3500
	defaultCooldownBaseMS      = 1200
	defaultPrivateBaseMS       = 200
	defaultWindowMS            = 5000
	defaultJitterMS            = 200
	defaultMaxBatchMessages    = 10
	defaultMaxBatchChars       = 1200
	defaultImmediateDelayMS    = 120
	defaultPostShortWaitMS     = 1200
	defaultPostLongWaitMS      = 5000
	defaultPostMaxRounds       = 3
	defaultSpeakJudgeTimeoutMS = 1200

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
	return &ImmersiveBuffer{
		cfg:       normalized,
		llm:       llmManager,
		nicknames: nicknames,
		sessions:  make(map[string]*immersiveSession),
	}
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
		preview := buildRecentPreview(queueSnapshot, 4)
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

	input := buildCombinedInput(queue)
	gate := b.shouldSpeak(state, queue)
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
	if input != "" && b.cfg.PostCooldownJudge.Enabled && postRounds < b.cfg.PostCooldownJudge.MaxRounds {
		lastSpeaker := queue[len(queue)-1].speaker
		recent := buildRecentPreview(queue, 6)
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
		reply, err := b.llm.Reply(context.Background(), input, sessionKey, "")
		if err != nil {
			log.Error().
				Err(err).
				Str("session", sessionKey).
				Msg("immersive reply failed")
			if ctx != nil {
				ctx.Send("LLM调用失败: " + llm.UserVisibleError(err))
			}
		} else if ctx != nil {
			ctx.Send(reply)
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(reply))).
				Msg("immersive reply sent")
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
func (b *ImmersiveBuffer) shouldSpeak(state *immersiveSession, queue []queuedMessage) speakGateResult {
	_ = state
	meta := summarizeQueueMeta(queue, time.Now(), b.nicknames)
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
			buildCombinedInput(queue),
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
