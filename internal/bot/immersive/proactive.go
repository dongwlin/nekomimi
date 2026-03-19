package immersive

import (
	"context"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/rs/zerolog/log"
)

const (
	defaultColdOpenMaxMessages     = 2
	defaultFollowupEnergyThreshold = 35.0
	defaultFollowupMaxRecentMsgs   = 3
)

// detectColdOpenEligibilityLocked checks whether the current message arrives
// after a long quiet period and marks the session as cold-open eligible.
// Must be called with session lock held.
func (b *ImmersiveBuffer) detectColdOpenEligibilityLocked(state *immersiveSession, now time.Time) {
	if state == nil {
		return
	}
	quietThreshold := time.Duration(b.cfg.Scheduler.QuietThresholdMS) * time.Millisecond
	if !state.lastMessageAt.IsZero() && now.Sub(state.lastMessageAt) >= quietThreshold {
		eligible := state.nextColdOpenEligibleAt.IsZero() || !now.Before(state.nextColdOpenEligibleAt)
		if eligible {
			state.coldOpenEligible = true
			state.coldOpenActivityCount = 0
			state.recordProactiveLocked("cold_open", "triggered", "quiet_window_reopened", false, now)
			log.Info().
				Str("proactive_kind", "cold_open").
				Str("proactive_status", "triggered").
				Str("reason_code", "quiet_window_reopened").
				Msg("immersive proactive decision")
			b.recordImmersiveMetrics(metrics.ImmersiveRecord{
				ProactiveKind:   "cold_open",
				ProactiveStatus: "triggered",
			})
		} else {
			state.recordProactiveLocked("cold_open", "skipped", "cooldown_active", true, now)
			log.Info().
				Str("proactive_kind", "cold_open").
				Str("proactive_status", "skipped").
				Str("reason_code", "cooldown_active").
				Msg("immersive proactive decision")
			b.recordImmersiveMetrics(metrics.ImmersiveRecord{
				ProactiveKind:   "cold_open",
				ProactiveStatus: "skipped",
			})
		}
	}
	state.lastMessageAt = now
	if state.coldOpenEligible {
		state.coldOpenActivityCount++
		if state.coldOpenActivityCount > defaultColdOpenMaxMessages {
			state.coldOpenEligible = false
			state.recordProactiveLocked("cold_open", "skipped", "window_exhausted", true, now)
			log.Info().
				Str("proactive_kind", "cold_open").
				Str("proactive_status", "skipped").
				Str("reason_code", "window_exhausted").
				Msg("immersive proactive decision")
			b.recordImmersiveMetrics(metrics.ImmersiveRecord{
				ProactiveKind:   "cold_open",
				ProactiveStatus: "skipped",
			})
		}
	}
	state.refreshDebugLocked(now)
}

// tryFollowup fires when the one-shot followup timer expires. It performs a
// secondary gate check and, if conditions are met, generates a brief follow-up
// reply without going through the normal flush pipeline.
func (b *ImmersiveBuffer) tryFollowup(sessionKey string) {
	state := b.session(sessionKey)
	state.mu.Lock()

	state.followupTimer = nil

	if state.inFlight {
		state.recordProactiveLocked("followup", "skipped", "inflight", true, time.Now())
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			ProactiveKind:   "followup",
			ProactiveStatus: "skipped",
		})
		return
	}

	if !state.pendingQuestion || state.mode != ModeWaitingUser {
		state.recordProactiveLocked("followup", "canceled", "no_pending_question", false, time.Now())
		state.clearPendingQuestionLocked()
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			ProactiveKind:   "followup",
			ProactiveStatus: "canceled",
		})
		return
	}

	if state.followupBudget <= 0 {
		state.recordProactiveLocked("followup", "skipped", "budget_exhausted", true, time.Now())
		state.clearPendingQuestionLocked()
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			ProactiveKind:   "followup",
			ProactiveStatus: "skipped",
		})
		return
	}

	now := time.Now()
	state.settleEnergyLocked(now, "followup_recovery")

	if state.energy < defaultFollowupEnergyThreshold {
		state.recordProactiveLocked("followup", "skipped", "energy_below_threshold", true, now)
		state.clearPendingQuestionLocked()
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			ProactiveKind:   "followup",
			ProactiveStatus: "skipped",
		})
		log.Info().
			Str("session", sessionKey).
			Str("proactive_kind", "followup").
			Str("proactive_status", "skipped").
			Str("reason_code", "energy_below_threshold").
			Float64("energy", state.energy).
			Msg("immersive followup skipped: energy below threshold")
		return
	}

	recentUserMsgs := countRecentUserMessages(state.runtimeBuffer, state.lastBotReplyAt)
	if recentUserMsgs > defaultFollowupMaxRecentMsgs {
		state.recordProactiveLocked("followup", "skipped", "topic_shift_detected", true, now)
		state.clearPendingQuestionLocked()
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			ProactiveKind:   "followup",
			ProactiveStatus: "skipped",
		})
		log.Info().
			Str("session", sessionKey).
			Str("proactive_kind", "followup").
			Str("proactive_status", "skipped").
			Str("reason_code", "topic_shift_detected").
			Int("recent_user_msgs", recentUserMsgs).
			Msg("immersive followup skipped: too many recent messages, likely new topic")
		return
	}

	state.followupBudget--
	state.inFlight = true
	state.recordProactiveLocked("followup", "triggered", "followup_timer", false, now)
	sendFn := state.sendFn
	runtimeSnapshot := make([]queuedMessage, len(state.runtimeBuffer))
	copy(runtimeSnapshot, state.runtimeBuffer)
	behavior := state.snapshotBehaviorLocked(now)
	state.mu.Unlock()
	b.recordImmersiveMetrics(metrics.ImmersiveRecord{
		Action:          "reply",
		ReasonCode:      "followup_reply",
		ProactiveKind:   "followup",
		ProactiveStatus: "triggered",
	})

	defer func() {
		state.mu.Lock()
		state.inFlight = false
		if state.followupTimer != nil {
			state.followupTimer.Stop()
			state.followupTimer = nil
		}
		b.reschedulePendingBatchLocked(sessionKey, state, time.Now())
		state.mu.Unlock()
	}()

	if !b.llm.IsEnabled() || !b.llm.IsImmersive(sessionKey) {
		state.mu.Lock()
		state.recordFinalActionLocked("early_drop", "immersive_disabled", "followup aborted because immersive mode is disabled", "", time.Now())
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "early_drop",
			ReasonCode: "immersive_disabled",
		})
		return
	}

	identity := b.currentIdentity()
	b.llm.SetAssistantSpeaker(assistantSpeakerLabel(identity))

	gate := speakGateDecision{
		Allow:            true,
		Reason:           "followup_timer",
		ReplyTier:        "brief",
		MaxReplySegments: 1,
		FollowupAllowed:  false,
	}
	immersiveCtx := buildImmersiveContext(nil, runtimeSnapshot, identity, behavior, gate)

	log.Info().
		Str("session", sessionKey).
		Str("proactive_kind", "followup").
		Str("proactive_status", "triggered").
		Str("mode", string(behavior.Mode)).
		Int("energy_value", behavior.EnergyValue).
		Int("energy_target", behavior.EnergyTarget).
		Int("followup_budget", behavior.FollowupBudget).
		Msg("immersive followup triggered")

	recordReply := func(reply, reason string, delivered bool) {
		trimmed := b.applyReplyBookkeeping(sessionKey, state, 0, reply, delivered)
		if trimmed == "" {
			return
		}
		state.mu.Lock()
		state.recordFinalActionLocked("reply", reason, "followup reply delivered", trimmed, time.Now())
		state.mu.Unlock()
	}

	replyCtx, cancelReply := context.WithCancel(context.Background())
	defer cancelReply()

	replyPrompt := buildImmersiveReplyPrompt(immersiveCtx)
	reply, replyErr := b.llm.ReplyStreamWithExtraPromptAllowTools(
		replyCtx, "", sessionKey, "", replyPrompt, nil, immersiveCtx,
	)
	if replyErr != nil {
		if state := b.lookupSession(sessionKey); state != nil {
			state.mu.Lock()
			state.recordFinalActionLocked("early_drop", "followup_reply_error", "followup reply generation failed", "", time.Now())
			state.mu.Unlock()
		}
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "early_drop",
			ReasonCode: "followup_reply_error",
		})
		log.Warn().
			Str("session", sessionKey).
			Err(replyErr).
			Msg("immersive followup reply failed")
		return
	}

	reply = strings.TrimSpace(reply)
	if reply == "" {
		reply = immersiveEmptyReplyFallback
	}

	canSend := sendFn != nil
	if canSend {
		sendFn(reply)
	} else {
		log.Warn().
			Str("session", sessionKey).
			Str("delivery", "followup").
			Msg("immersive send function missing, skipping outbound send")
	}

	log.Info().
		Str("session", sessionKey).
		Str("proactive_kind", "followup").
		Str("proactive_status", "triggered").
		Int("reply_chars", len([]rune(reply))).
		Bool("delivered", canSend).
		Msg("immersive followup reply sent")

	recordReply(reply, "followup_reply", canSend)
}

// countRecentUserMessages counts user messages in the buffer that arrived
// after the given timestamp.
func countRecentUserMessages(buffer []queuedMessage, since time.Time) int {
	count := 0
	for _, msg := range buffer {
		if msg.kind != EventUserMessage {
			continue
		}
		if !since.IsZero() && msg.ts.Before(since) {
			continue
		}
		count++
	}
	return count
}
