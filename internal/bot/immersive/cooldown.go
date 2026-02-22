package immersive

import (
	"github.com/dongwlin/nekomimi/internal/config"
)

// normalizeImmersiveConfig ensures all configuration values have sensible defaults
// for the immersive buffer to function correctly.
func normalizeImmersiveConfig(cfg config.ImmersiveConfig) config.ImmersiveConfig {
	if cfg.Timeline.MaxMessages <= 0 {
		cfg.Timeline.MaxMessages = defaultTimelineMaxMessages
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
