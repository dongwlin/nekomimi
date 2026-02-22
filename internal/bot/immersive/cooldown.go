package immersive

import (
	"github.com/dongwlin/nekomimi/internal/config"
)

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
	if cfg.Timeline.MaxMessages <= 0 {
		cfg.Timeline.MaxMessages = defaultTimelineMaxMessages
	}
	if cfg.Timeline.OverflowMessages <= 0 {
		cfg.Timeline.OverflowMessages = defaultTimelineOverflowMessages
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
