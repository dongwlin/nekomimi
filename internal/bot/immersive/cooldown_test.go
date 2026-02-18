package immersive

import (
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestNormalizeImmersiveConfig_DefaultValues(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{})

	if cfg.MaxBatchMessages != defaultMaxBatchMessages {
		t.Errorf("expected MaxBatchMessages %d, got %d", defaultMaxBatchMessages, cfg.MaxBatchMessages)
	}
	if cfg.MaxBatchChars != defaultMaxBatchChars {
		t.Errorf("expected MaxBatchChars %d, got %d", defaultMaxBatchChars, cfg.MaxBatchChars)
	}
	if cfg.ImmediateDelayMS != defaultImmediateDelayMS {
		t.Errorf("expected ImmediateDelayMS %d, got %d", defaultImmediateDelayMS, cfg.ImmediateDelayMS)
	}
	if !cfg.ContinuousSpeech.Enabled {
		t.Errorf("expected ContinuousSpeech.Enabled true by default")
	}
	if cfg.ContinuousSpeech.MinChunkChars != defaultContinuousMinChars {
		t.Errorf("expected MinChunkChars %d, got %d", defaultContinuousMinChars, cfg.ContinuousSpeech.MinChunkChars)
	}
	if cfg.ContinuousSpeech.MaxChunkChars != defaultContinuousMaxChars {
		t.Errorf("expected MaxChunkChars %d, got %d", defaultContinuousMaxChars, cfg.ContinuousSpeech.MaxChunkChars)
	}
	if cfg.ContinuousSpeech.MinIntervalMS != defaultContinuousMinMS {
		t.Errorf("expected MinIntervalMS %d, got %d", defaultContinuousMinMS, cfg.ContinuousSpeech.MinIntervalMS)
	}
	if cfg.ContinuousSpeech.MaxIntervalMS != defaultContinuousMaxMS {
		t.Errorf("expected MaxIntervalMS %d, got %d", defaultContinuousMaxMS, cfg.ContinuousSpeech.MaxIntervalMS)
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
	if cfg.PokeReaction.MaxReplyChars != defaultPokeReactionMaxReplyChars {
		t.Errorf("expected PokeReaction.MaxReplyChars %d, got %d", defaultPokeReactionMaxReplyChars, cfg.PokeReaction.MaxReplyChars)
	}
}

func TestNormalizeImmersiveConfig_PreservesValidValues(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		MaxBatchMessages: 20,
		MaxBatchChars:    2000,
		ImmediateDelayMS: 200,
		ContinuousSpeech: config.ContinuousSpeechConfig{
			Enabled:       false,
			MinChunkChars: 16,
			MaxChunkChars: 64,
			MinIntervalMS: 350,
			MaxIntervalMS: 1000,
			RequireStream: true,
		},
		PokeReaction: config.PokeReactionConfig{
			WindowMS:         60000,
			MildThreshold:    4,
			AnnoyedThreshold: 7,
			MaxReplyChars:    24,
		},
	})

	if cfg.MaxBatchMessages != 20 {
		t.Errorf("expected MaxBatchMessages 20, got %d", cfg.MaxBatchMessages)
	}
	if cfg.MaxBatchChars != 2000 {
		t.Errorf("expected MaxBatchChars 2000, got %d", cfg.MaxBatchChars)
	}
	if cfg.ImmediateDelayMS != 200 {
		t.Errorf("expected ImmediateDelayMS 200, got %d", cfg.ImmediateDelayMS)
	}
	if cfg.ContinuousSpeech.Enabled {
		t.Errorf("expected ContinuousSpeech.Enabled false, got true")
	}
	if cfg.ContinuousSpeech.MinChunkChars != 16 {
		t.Errorf("expected MinChunkChars 16, got %d", cfg.ContinuousSpeech.MinChunkChars)
	}
	if cfg.ContinuousSpeech.MaxChunkChars != 64 {
		t.Errorf("expected MaxChunkChars 64, got %d", cfg.ContinuousSpeech.MaxChunkChars)
	}
	if cfg.ContinuousSpeech.MinIntervalMS != 350 {
		t.Errorf("expected MinIntervalMS 350, got %d", cfg.ContinuousSpeech.MinIntervalMS)
	}
	if cfg.ContinuousSpeech.MaxIntervalMS != 1000 {
		t.Errorf("expected MaxIntervalMS 1000, got %d", cfg.ContinuousSpeech.MaxIntervalMS)
	}
	if !cfg.ContinuousSpeech.RequireStream {
		t.Errorf("expected RequireStream true, got false")
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
	if cfg.PokeReaction.MaxReplyChars != 24 {
		t.Errorf("expected PokeReaction.MaxReplyChars 24, got %d", cfg.PokeReaction.MaxReplyChars)
	}
}

func TestNormalizeImmersiveConfig_PokeReactionThresholdOrder(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		PokeReaction: config.PokeReactionConfig{
			WindowMS:         30000,
			MildThreshold:    5,
			AnnoyedThreshold: 2,
			MaxReplyChars:    18,
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
