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
}
