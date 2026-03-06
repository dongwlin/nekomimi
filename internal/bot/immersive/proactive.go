package immersive

import (
	"context"
	"strings"
	"time"

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
		}
	}
	state.lastMessageAt = now
	if state.coldOpenEligible {
		state.coldOpenActivityCount++
		if state.coldOpenActivityCount > defaultColdOpenMaxMessages {
			state.coldOpenEligible = false
		}
	}
}

// tryFollowup fires when the one-shot followup timer expires. It performs a
// secondary gate check and, if conditions are met, generates a brief follow-up
// reply without going through the normal flush pipeline.
func (b *ImmersiveBuffer) tryFollowup(sessionKey string) {
	state := b.session(sessionKey)
	state.mu.Lock()

	state.followupTimer = nil

	if state.inFlight {
		state.mu.Unlock()
		return
	}

	if !state.pendingQuestion || state.mode != ModeWaitingUser {
		state.clearPendingQuestionLocked()
		state.mu.Unlock()
		return
	}

	if state.followupBudget <= 0 {
		state.clearPendingQuestionLocked()
		state.mu.Unlock()
		return
	}

	now := time.Now()
	state.settleEnergyLocked(now, "followup_recovery")

	if state.energy < defaultFollowupEnergyThreshold {
		state.clearPendingQuestionLocked()
		state.mu.Unlock()
		log.Info().
			Str("session", sessionKey).
			Float64("energy", state.energy).
			Msg("immersive followup skipped: energy below threshold")
		return
	}

	recentUserMsgs := countRecentUserMessages(state.runtimeBuffer, state.lastBotReplyAt)
	if recentUserMsgs > defaultFollowupMaxRecentMsgs {
		state.clearPendingQuestionLocked()
		state.mu.Unlock()
		log.Info().
			Str("session", sessionKey).
			Int("recent_user_msgs", recentUserMsgs).
			Msg("immersive followup skipped: too many recent messages, likely new topic")
		return
	}

	state.followupBudget--
	state.inFlight = true
	sendFn := state.sendFn
	runtimeSnapshot := make([]queuedMessage, len(state.runtimeBuffer))
	copy(runtimeSnapshot, state.runtimeBuffer)
	behavior := state.snapshotBehaviorLocked(now)
	state.mu.Unlock()

	defer func() {
		state.mu.Lock()
		state.inFlight = false
		if state.followupTimer != nil {
			state.followupTimer.Stop()
			state.followupTimer = nil
		}
		pending := len(state.nextBatch) > 0
		if pending {
			if state.batchStartTime.IsZero() {
				state.batchStartTime = batchStartTimeFromQueue(state.nextBatch)
			}
			decision := b.computeFlushDecision(sessionKey, state, time.Now())
			if state.timer != nil {
				state.timer.Stop()
				state.timer = nil
			}
			state.timer = time.AfterFunc(decision.Delay, func() {
				b.flush(sessionKey)
			})
		}
		state.mu.Unlock()
	}()

	if !b.llm.IsEnabled() || !b.llm.IsImmersive(sessionKey) {
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
		Str("mode", string(behavior.Mode)).
		Int("energy_value", behavior.EnergyValue).
		Int("followup_budget", behavior.FollowupBudget).
		Msg("immersive followup triggered")

	recordReply := func(reply, reason string, delivered bool) {
		trimmed := strings.TrimSpace(reply)
		if trimmed == "" {
			return
		}
		b.recordAssistantUtterance(sessionKey, trimmed)
		_ = b.llm.AppendAssistantEvent(sessionKey, trimmed, 0)
		if delivered {
			b.noteAssistantDelivered(sessionKey, trimmed)
		}
	}

	replyCtx, cancelReply := context.WithCancel(context.Background())
	defer cancelReply()

	reply, replyErr := b.llm.ReplyStreamWithExtraPromptAllowTools(
		replyCtx, "", sessionKey, "", "", nil, immersiveCtx,
	)
	if replyErr != nil {
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
