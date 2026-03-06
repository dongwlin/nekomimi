package immersive

import (
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

// normalizeImmersiveConfig ensures all configuration values have sensible defaults
// for the immersive buffer to function correctly.
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
	return cfg
}

func (b *ImmersiveBuffer) computeFlushDelay(sessionKey string, session *immersiveSession, now time.Time) time.Duration {
	if isPrivateSessionKey(sessionKey) {
		return 0
	}

	policy := b.cfg.FlushPolicy
	if session != nil && policy.MaxBatchSize > 0 && len(session.nextBatch) >= policy.MaxBatchSize {
		return 0
	}

	debounce := time.Duration(policy.MinBatchWaitMS) * time.Millisecond
	if session != nil && !session.batchStartTime.IsZero() && policy.MaxBatchWaitMS > 0 {
		maxDeadline := session.batchStartTime.Add(time.Duration(policy.MaxBatchWaitMS) * time.Millisecond)
		remaining := maxDeadline.Sub(now)
		if remaining <= 0 {
			return 0
		}
		if debounce <= 0 || remaining < debounce {
			return remaining
		}
	}

	return debounce
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
