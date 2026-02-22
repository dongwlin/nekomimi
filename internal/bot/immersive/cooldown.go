package immersive

import (
	"github.com/dongwlin/nekomimi/internal/config"
)

// normalizeImmersiveConfig ensures all configuration values have sensible defaults
// for the immersive buffer to function correctly.
func normalizeImmersiveConfig(cfg config.ImmersiveConfig) config.ImmersiveConfig {
	// Prefer runtime_buffer.max_messages, but allow legacy timeline.max_messages.
	if cfg.RuntimeBuffer.MaxMessages <= 0 {
		cfg.RuntimeBuffer.MaxMessages = cfg.Timeline.MaxMessages
	}
	if cfg.RuntimeBuffer.MaxMessages <= 0 {
		cfg.RuntimeBuffer.MaxMessages = defaultRuntimeBufferMaxMessages
	}
	// Keep legacy alias mirrored for internal compatibility.
	cfg.Timeline.MaxMessages = cfg.RuntimeBuffer.MaxMessages
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
