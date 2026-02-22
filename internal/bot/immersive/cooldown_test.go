package immersive

import (
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestNormalizeImmersiveConfig_DefaultValues(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{})

	if cfg.RuntimeBuffer.MaxMessages != defaultRuntimeBufferMaxMessages {
		t.Errorf(
			"expected RuntimeBuffer.MaxMessages %d, got %d",
			defaultRuntimeBufferMaxMessages,
			cfg.RuntimeBuffer.MaxMessages,
		)
	}
	if cfg.PokeReaction.WindowMS != defaultPokeReactionWindowMS {
		t.Errorf("expected PokeReaction.WindowMS %d, got %d", defaultPokeReactionWindowMS, cfg.PokeReaction.WindowMS)
	}
	if cfg.PokeReaction.MildThreshold != defaultPokeReactionMildThresh {
		t.Errorf("expected PokeReaction.MildThreshold %d, got %d", defaultPokeReactionMildThresh, cfg.PokeReaction.MildThreshold)
	}
	if cfg.PokeReaction.AnnoyedThreshold != defaultPokeReactionAnnoyedThresh {
		t.Errorf("expected PokeReaction.AnnoyedThreshold %d, got %d", defaultPokeReactionAnnoyedThresh, cfg.PokeReaction.AnnoyedThreshold)
	}
}

func TestNormalizeImmersiveConfig_PreservesValidValues(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		RuntimeBuffer: config.RuntimeBufferConfig{
			MaxMessages: 123,
		},
		PokeReaction: config.PokeReactionConfig{
			WindowMS:         60000,
			MildThreshold:    4,
			AnnoyedThreshold: 7,
		},
	})

	if cfg.RuntimeBuffer.MaxMessages != 123 {
		t.Errorf("expected RuntimeBuffer.MaxMessages 123, got %d", cfg.RuntimeBuffer.MaxMessages)
	}
	if cfg.PokeReaction.WindowMS != 60000 {
		t.Errorf("expected PokeReaction.WindowMS 60000, got %d", cfg.PokeReaction.WindowMS)
	}
	if cfg.PokeReaction.MildThreshold != 4 {
		t.Errorf("expected PokeReaction.MildThreshold 4, got %d", cfg.PokeReaction.MildThreshold)
	}
	if cfg.PokeReaction.AnnoyedThreshold != 7 {
		t.Errorf("expected PokeReaction.AnnoyedThreshold 7, got %d", cfg.PokeReaction.AnnoyedThreshold)
	}
}

func TestNormalizeImmersiveConfig_PokeReactionThresholdOrder(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		PokeReaction: config.PokeReactionConfig{
			WindowMS:         30000,
			MildThreshold:    5,
			AnnoyedThreshold: 2,
		},
	})
	if cfg.PokeReaction.AnnoyedThreshold != cfg.PokeReaction.MildThreshold {
		t.Fatalf(
			"expected AnnoyedThreshold to be clamped to MildThreshold, got annoyed=%d mild=%d",
			cfg.PokeReaction.AnnoyedThreshold,
			cfg.PokeReaction.MildThreshold,
		)
	}
}
