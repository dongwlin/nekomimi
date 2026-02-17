package immersive

import (
	"math/rand"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

// calcCooldown calculates the cooldown duration for the next response based on
// message characteristics and recent activity. It considers factors like:
// - Whether the message is private
// - Whether the message mentions or addresses the bot
// - Whether the message is a question
// - Recent message count and character volume
func (b *ImmersiveBuffer) calcCooldown(isPrivate, mention, question bool, recentCount, recentChars, msgChars int) time.Duration {
	base := b.cfg.CooldownBaseMS
	if isPrivate {
		base = b.cfg.PrivateBaseMS
	}
	cooldown := base
	cooldown += recentCount * activeMsgPenaltyMS
	cooldown += (recentChars / 10) * activeCharPenaltyMS
	if msgChars <= shortMsgLen {
		cooldown += shortMsgPenaltyMS
	}
	if mention || question {
		cooldown -= mentionBonusMS
	}
	if b.cfg.JitterMS > 0 {
		jitter := rand.Intn(b.cfg.JitterMS*2+1) - b.cfg.JitterMS
		cooldown += jitter
	}
	if cooldown < b.cfg.CooldownMinMS {
		cooldown = b.cfg.CooldownMinMS
	}
	if cooldown > b.cfg.CooldownMaxMS {
		cooldown = b.cfg.CooldownMaxMS
	}
	if mention || question {
		if cooldown < b.cfg.ImmediateDelayMS {
			cooldown = b.cfg.ImmediateDelayMS
		}
	}
	return time.Duration(cooldown) * time.Millisecond
}

// normalizeImmersiveConfig ensures all configuration values have sensible defaults
// for the immersive buffer to function correctly.
func normalizeImmersiveConfig(cfg config.ImmersiveConfig) config.ImmersiveConfig {
	if !cfg.ContinuousSpeech.Enabled &&
		cfg.ContinuousSpeech.MinChunkChars == 0 &&
		cfg.ContinuousSpeech.MaxChunkChars == 0 &&
		cfg.ContinuousSpeech.MinIntervalMS == 0 &&
		cfg.ContinuousSpeech.MaxIntervalMS == 0 &&
		!cfg.ContinuousSpeech.RequireStream {
		cfg.ContinuousSpeech.Enabled = true
	}
	if cfg.CooldownMinMS <= 0 {
		cfg.CooldownMinMS = defaultCooldownMinMS
	}
	if cfg.CooldownMaxMS <= 0 {
		cfg.CooldownMaxMS = defaultCooldownMaxMS
	}
	if cfg.CooldownBaseMS <= 0 {
		cfg.CooldownBaseMS = defaultCooldownBaseMS
	}
	if cfg.PrivateBaseMS <= 0 {
		cfg.PrivateBaseMS = defaultPrivateBaseMS
	}
	if cfg.WindowMS <= 0 {
		cfg.WindowMS = defaultWindowMS
	}
	if cfg.JitterMS <= 0 {
		cfg.JitterMS = defaultJitterMS
	}
	if cfg.MaxBatchMessages <= 0 {
		cfg.MaxBatchMessages = defaultMaxBatchMessages
	}
	if cfg.MaxBatchChars <= 0 {
		cfg.MaxBatchChars = defaultMaxBatchChars
	}
	if cfg.ImmediateDelayMS <= 0 {
		cfg.ImmediateDelayMS = defaultImmediateDelayMS
	}
	if cfg.Timeline.MaxMessages <= 0 {
		cfg.Timeline.MaxMessages = defaultTimelineMaxMessages
	}
	if cfg.Timeline.OverflowMessages <= 0 {
		cfg.Timeline.OverflowMessages = defaultTimelineOverflowMessages
	}
	if cfg.Timeline.CompressBatch <= 0 {
		cfg.Timeline.CompressBatch = defaultTimelineCompressBatch
	}
	if cfg.Timeline.CompressBatch > cfg.Timeline.MaxMessages {
		cfg.Timeline.CompressBatch = cfg.Timeline.MaxMessages
	}
	if cfg.ContinuousSpeech.MinChunkChars <= 0 {
		cfg.ContinuousSpeech.MinChunkChars = defaultContinuousMinChars
	}
	if cfg.ContinuousSpeech.MaxChunkChars <= 0 {
		cfg.ContinuousSpeech.MaxChunkChars = defaultContinuousMaxChars
	}
	if cfg.ContinuousSpeech.MaxChunkChars < cfg.ContinuousSpeech.MinChunkChars {
		cfg.ContinuousSpeech.MaxChunkChars = cfg.ContinuousSpeech.MinChunkChars
	}
	if cfg.ContinuousSpeech.MinIntervalMS <= 0 {
		cfg.ContinuousSpeech.MinIntervalMS = defaultContinuousMinMS
	}
	if cfg.ContinuousSpeech.MaxIntervalMS <= 0 {
		cfg.ContinuousSpeech.MaxIntervalMS = defaultContinuousMaxMS
	}
	if cfg.ContinuousSpeech.MaxIntervalMS < cfg.ContinuousSpeech.MinIntervalMS {
		cfg.ContinuousSpeech.MaxIntervalMS = cfg.ContinuousSpeech.MinIntervalMS
	}
	if cfg.SpeakGate.TimeoutMS <= 0 {
		cfg.SpeakGate.TimeoutMS = defaultSpeakJudgeTimeoutMS
	}
	if cfg.PostCooldownJudge.TimeoutMS <= 0 {
		cfg.PostCooldownJudge.TimeoutMS = 1200
	}
	if cfg.PostCooldownJudge.ShortWaitMS <= 0 {
		cfg.PostCooldownJudge.ShortWaitMS = defaultPostShortWaitMS
	}
	if cfg.PostCooldownJudge.LongWaitMS <= 0 {
		cfg.PostCooldownJudge.LongWaitMS = defaultPostLongWaitMS
	}
	if cfg.PostCooldownJudge.MaxRounds < 0 {
		cfg.PostCooldownJudge.MaxRounds = defaultPostMaxRounds
	}
	return cfg
}
