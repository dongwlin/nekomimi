package immersive

import (
	"testing"
	"time"

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
	if cfg.FlushPolicy.MinBatchWaitMS != 0 {
		t.Errorf("expected FlushPolicy.MinBatchWaitMS 0, got %d", cfg.FlushPolicy.MinBatchWaitMS)
	}
	if cfg.FlushPolicy.MaxBatchWaitMS != 0 {
		t.Errorf("expected FlushPolicy.MaxBatchWaitMS 0, got %d", cfg.FlushPolicy.MaxBatchWaitMS)
	}
	if cfg.FlushPolicy.MaxBatchSize != 0 {
		t.Errorf("expected FlushPolicy.MaxBatchSize 0, got %d", cfg.FlushPolicy.MaxBatchSize)
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
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 500,
			MaxBatchWaitMS: 2200,
			MaxBatchSize:   9,
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
	if cfg.FlushPolicy.MinBatchWaitMS != 500 {
		t.Errorf("expected FlushPolicy.MinBatchWaitMS 500, got %d", cfg.FlushPolicy.MinBatchWaitMS)
	}
	if cfg.FlushPolicy.MaxBatchWaitMS != 2200 {
		t.Errorf("expected FlushPolicy.MaxBatchWaitMS 2200, got %d", cfg.FlushPolicy.MaxBatchWaitMS)
	}
	if cfg.FlushPolicy.MaxBatchSize != 9 {
		t.Errorf("expected FlushPolicy.MaxBatchSize 9, got %d", cfg.FlushPolicy.MaxBatchSize)
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

func TestNormalizeImmersiveConfig_ClampNegativeFlushPolicy(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: -1,
			MaxBatchWaitMS: -2,
			MaxBatchSize:   -3,
		},
	})

	if cfg.FlushPolicy.MinBatchWaitMS != 0 {
		t.Fatalf("expected MinBatchWaitMS clamped to 0, got %d", cfg.FlushPolicy.MinBatchWaitMS)
	}
	if cfg.FlushPolicy.MaxBatchWaitMS != 0 {
		t.Fatalf("expected MaxBatchWaitMS clamped to 0, got %d", cfg.FlushPolicy.MaxBatchWaitMS)
	}
	if cfg.FlushPolicy.MaxBatchSize != 0 {
		t.Fatalf("expected MaxBatchSize clamped to 0, got %d", cfg.FlushPolicy.MaxBatchSize)
	}
}

func TestComputeFlushDelay_DefaultConfigImmediate(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{}, nil, nil)
	now := time.Now()
	session := &immersiveSession{
		nextBatch:      []queuedMessage{{text: "hello"}},
		batchStartTime: now,
	}

	if got := buffer.computeFlushDelay("group:1", session, now); got != 0 {
		t.Fatalf("expected immediate flush with default config, got %s", got)
	}
}

func TestComputeFlushDelay_UsesMinBatchWait(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 500,
		},
	}, nil, nil)
	now := time.Now()
	session := &immersiveSession{
		nextBatch:      []queuedMessage{{text: "hello"}},
		batchStartTime: now,
	}

	if got := buffer.computeFlushDelay("group:1", session, now); got != 500*time.Millisecond {
		t.Fatalf("expected 500ms debounce, got %s", got)
	}
}

func TestComputeFlushDelay_UsesMaxBatchDeadline(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 500,
			MaxBatchWaitMS: 100,
		},
	}, nil, nil)
	now := time.Now()
	session := &immersiveSession{
		nextBatch:      []queuedMessage{{text: "hello"}},
		batchStartTime: now.Add(-150 * time.Millisecond),
	}

	if got := buffer.computeFlushDelay("group:1", session, now); got != 0 {
		t.Fatalf("expected immediate flush after max wait deadline, got %s", got)
	}
}

func TestComputeFlushDelay_UsesMaxBatchSize(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 500,
			MaxBatchSize:   2,
		},
	}, nil, nil)
	now := time.Now()
	session := &immersiveSession{
		nextBatch: []queuedMessage{
			{text: "hello"},
			{text: "world"},
		},
		batchStartTime: now,
	}

	if got := buffer.computeFlushDelay("group:1", session, now); got != 0 {
		t.Fatalf("expected immediate flush at max batch size, got %s", got)
	}
}

func TestComputeFlushDelay_PrivateSessionImmediate(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 500,
			MaxBatchWaitMS: 3000,
			MaxBatchSize:   10,
		},
	}, nil, nil)
	now := time.Now()
	session := &immersiveSession{
		nextBatch:      []queuedMessage{{text: "hello"}},
		batchStartTime: now,
	}

	if got := buffer.computeFlushDelay("private:1", session, now); got != 0 {
		t.Fatalf("expected private session to flush immediately, got %s", got)
	}
}
