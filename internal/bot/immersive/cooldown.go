package immersive

import (
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

const (
	defaultPostReplyDelayMS      = 8000
	defaultStrongCallMinDelayMS  = 200
	defaultFollowupWaitMS        = 90000
	defaultColdOpenMinIntervalMS = 600000
	defaultQuietThresholdMS      = 300000
)

func normalizeImmersiveConfig(cfg config.ImmersiveConfig) config.ImmersiveConfig {
	if cfg.RuntimeBuffer.MaxMessages <= 0 {
		cfg.RuntimeBuffer.MaxMessages = defaultRuntimeBufferMaxMessages
	}
	if cfg.FlushPolicy.MinBatchWaitMS < 0 {
		cfg.FlushPolicy.MinBatchWaitMS = 0
	}
	if cfg.FlushPolicy.MaxBatchWaitMS < 0 {
		cfg.FlushPolicy.MaxBatchWaitMS = 0
	}
	if cfg.FlushPolicy.MaxBatchSize < 0 {
		cfg.FlushPolicy.MaxBatchSize = 0
	}
	if cfg.PokeReaction.WindowMS <= 0 {
		cfg.PokeReaction.WindowMS = defaultPokeReactionWindowMS
	}
	if cfg.PokeReaction.MildThreshold <= 0 {
		cfg.PokeReaction.MildThreshold = defaultPokeReactionMildThresh
	}
	if cfg.PokeReaction.AnnoyedThreshold <= 0 {
		cfg.PokeReaction.AnnoyedThreshold = defaultPokeReactionAnnoyedThresh
	}
	if cfg.PokeReaction.AnnoyedThreshold < cfg.PokeReaction.MildThreshold {
		cfg.PokeReaction.AnnoyedThreshold = cfg.PokeReaction.MildThreshold
	}
	if cfg.Scheduler.PostReplyDelayMS <= 0 {
		cfg.Scheduler.PostReplyDelayMS = defaultPostReplyDelayMS
	}
	if cfg.Scheduler.StrongCallMinDelayMS <= 0 {
		cfg.Scheduler.StrongCallMinDelayMS = defaultStrongCallMinDelayMS
	}
	if cfg.Scheduler.FollowupWaitMS <= 0 {
		cfg.Scheduler.FollowupWaitMS = defaultFollowupWaitMS
	}
	if cfg.Scheduler.ColdOpenMinIntervalMS <= 0 {
		cfg.Scheduler.ColdOpenMinIntervalMS = defaultColdOpenMinIntervalMS
	}
	if cfg.Scheduler.QuietThresholdMS <= 0 {
		cfg.Scheduler.QuietThresholdMS = defaultQuietThresholdMS
	}
	return cfg
}

// computeFlushDecision returns an adaptive scheduling decision based on session
// state, signal strength, cooldown, and configured flush policy.
func (b *ImmersiveBuffer) computeFlushDecision(sessionKey string, session *immersiveSession, now time.Time) FlushDecision {
	if isPrivateSessionKey(sessionKey) {
		return FlushDecision{Delay: 0, Reason: "private_session", Priority: "immediate"}
	}

	policy := b.cfg.FlushPolicy
	sched := b.cfg.Scheduler

	if session == nil {
		return FlushDecision{
			Delay:    time.Duration(policy.MinBatchWaitMS) * time.Millisecond,
			Reason:   "no_session",
			Priority: "normal",
		}
	}

	if policy.MaxBatchSize > 0 && len(session.nextBatch) >= policy.MaxBatchSize {
		return FlushDecision{Delay: 0, Reason: "batch_full", Priority: "immediate"}
	}

	var maxDeadlineRemaining time.Duration = -1
	if !session.batchStartTime.IsZero() && policy.MaxBatchWaitMS > 0 {
		maxDeadline := session.batchStartTime.Add(time.Duration(policy.MaxBatchWaitMS) * time.Millisecond)
		maxDeadlineRemaining = maxDeadline.Sub(now)
		if maxDeadlineRemaining <= 0 {
			return FlushDecision{Delay: 0, Reason: "max_batch_deadline", Priority: "immediate"}
		}
	}

	hasStrongSignal := batchHasStrongSignal(session.nextBatch)
	baseDelay := time.Duration(policy.MinBatchWaitMS) * time.Millisecond

	if hasStrongSignal {
		fastDelay := time.Duration(sched.StrongCallMinDelayMS) * time.Millisecond
		if baseDelay > 0 && fastDelay > baseDelay {
			fastDelay = baseDelay
		}
		if maxDeadlineRemaining >= 0 && fastDelay > maxDeadlineRemaining {
			fastDelay = maxDeadlineRemaining
		}
		return FlushDecision{Delay: fastDelay, Reason: "strong_call", Priority: "fast"}
	}

	if session.mode == ModeCoolingDown && sched.PostReplyDelayMS > 0 {
		var sinceReply time.Duration
		if !session.lastBotReplyAt.IsZero() {
			sinceReply = now.Sub(session.lastBotReplyAt)
		}
		postReplyWindow := time.Duration(sched.PostReplyDelayMS) * time.Millisecond
		if sinceReply < postReplyWindow {
			deferredDelay := postReplyWindow - sinceReply
			if deferredDelay < baseDelay {
				deferredDelay = baseDelay
			}
			if maxDeadlineRemaining >= 0 && deferredDelay > maxDeadlineRemaining {
				deferredDelay = maxDeadlineRemaining
			}
			return FlushDecision{Delay: deferredDelay, Reason: "post_reply_cooldown", Priority: "deferred"}
		}
	}

	delay := baseDelay
	if maxDeadlineRemaining >= 0 && (delay <= 0 || maxDeadlineRemaining < delay) {
		delay = maxDeadlineRemaining
	}
	return FlushDecision{Delay: delay, Reason: "normal_debounce", Priority: "normal"}
}

// computeFlushDelay returns the scheduling delay for backward compatibility.
func (b *ImmersiveBuffer) computeFlushDelay(sessionKey string, session *immersiveSession, now time.Time) time.Duration {
	return b.computeFlushDecision(sessionKey, session, now).Delay
}

// mergeWaitDecision combines a local scheduler decision with an LLM wait
// suggestion, using the longer delay to respect both model advice and local policy.
func mergeWaitDecision(local FlushDecision, llmWaitMS int) FlushDecision {
	if llmWaitMS <= 0 {
		return local
	}
	llmDelay := time.Duration(llmWaitMS) * time.Millisecond
	if llmDelay > local.Delay {
		return FlushDecision{
			Delay:    llmDelay,
			Reason:   "model_wait",
			Priority: local.Priority,
		}
	}
	return local
}

func batchHasStrongSignal(batch []queuedMessage) bool {
	for _, msg := range batch {
		if msg.isMentionBot || msg.nicknamePosition >= NickStart {
			return true
		}
	}
	return false
}

func batchStartTimeFromQueue(queue []queuedMessage) time.Time {
	var start time.Time
	for _, msg := range queue {
		if msg.ts.IsZero() {
			continue
		}
		if start.IsZero() || msg.ts.Before(start) {
			start = msg.ts
		}
	}
	return start
}

func isPrivateSessionKey(sessionKey string) bool {
	return strings.HasPrefix(strings.TrimSpace(sessionKey), "private:")
}
