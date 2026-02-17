package immersive

import (
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestCalcCooldown_BasicCalculation(t *testing.T) {
	cfg := config.ImmersiveConfig{
		CooldownMinMS:    800,
		CooldownMaxMS:    3500,
		CooldownBaseMS:   1200,
		PrivateBaseMS:    200,
		ImmediateDelayMS: 120,
		JitterMS:         0,
	}
	buffer := &ImmersiveBuffer{cfg: cfg}

	cooldown := buffer.calcCooldown(false, false, false, 0, 0, 100)

	if cooldown != time.Duration(cfg.CooldownBaseMS)*time.Millisecond {
		t.Errorf("expected cooldown %dms, got %v", cfg.CooldownBaseMS, cooldown)
	}
}

func TestCalcCooldown_PrivateMessage(t *testing.T) {
	cfg := config.ImmersiveConfig{
		CooldownMinMS:    100,
		CooldownMaxMS:    3500,
		CooldownBaseMS:   1200,
		PrivateBaseMS:    200,
		ImmediateDelayMS: 120,
		JitterMS:         0,
	}
	buffer := &ImmersiveBuffer{cfg: cfg}

	cooldown := buffer.calcCooldown(true, false, false, 0, 0, 100)

	if cooldown != time.Duration(cfg.PrivateBaseMS)*time.Millisecond {
		t.Errorf("expected private cooldown %dms, got %v", cfg.PrivateBaseMS, cooldown)
	}
}

func TestCalcCooldown_WithMention_ReducesCooldown(t *testing.T) {
	cfg := config.ImmersiveConfig{
		CooldownMinMS:    50,
		CooldownMaxMS:    3500,
		CooldownBaseMS:   1200,
		PrivateBaseMS:    200,
		ImmediateDelayMS: 120,
		JitterMS:         0,
	}
	buffer := &ImmersiveBuffer{cfg: cfg}

	cooldown := buffer.calcCooldown(false, true, false, 0, 0, 100)

	base := cfg.CooldownBaseMS - mentionBonusMS
	if cooldown != time.Duration(base)*time.Millisecond {
		t.Errorf("expected cooldown %dms (base - mentionBonus), got %v", base, cooldown)
	}
}

func TestCalcCooldown_WithQuestion_ReducesCooldown(t *testing.T) {
	cfg := config.ImmersiveConfig{
		CooldownMinMS:    50,
		CooldownMaxMS:    3500,
		CooldownBaseMS:   1200,
		PrivateBaseMS:    200,
		ImmediateDelayMS: 120,
		JitterMS:         0,
	}
	buffer := &ImmersiveBuffer{cfg: cfg}

	cooldown := buffer.calcCooldown(false, false, true, 0, 0, 100)

	base := cfg.CooldownBaseMS - mentionBonusMS
	if cooldown != time.Duration(base)*time.Millisecond {
		t.Errorf("expected cooldown %dms (base - mentionBonus), got %v", base, cooldown)
	}
}

func TestCalcCooldown_ShortMessage_Penalty(t *testing.T) {
	cfg := config.ImmersiveConfig{
		CooldownMinMS:    800,
		CooldownMaxMS:    3500,
		CooldownBaseMS:   1200,
		PrivateBaseMS:    200,
		ImmediateDelayMS: 120,
		JitterMS:         0,
	}
	buffer := &ImmersiveBuffer{cfg: cfg}

	cooldown := buffer.calcCooldown(false, false, false, 0, 0, 5)

	expected := cfg.CooldownBaseMS + shortMsgPenaltyMS
	if cooldown != time.Duration(expected)*time.Millisecond {
		t.Errorf("expected cooldown %dms (base + shortMsgPenalty), got %v", expected, cooldown)
	}
}

func TestCalcCooldown_RecentActivity(t *testing.T) {
	cfg := config.ImmersiveConfig{
		CooldownMinMS:    800,
		CooldownMaxMS:    3500,
		CooldownBaseMS:   1200,
		PrivateBaseMS:    200,
		ImmediateDelayMS: 120,
		JitterMS:         0,
	}
	buffer := &ImmersiveBuffer{cfg: cfg}

	cooldown := buffer.calcCooldown(false, false, false, 5, 100, 100)

	if cooldown <= time.Duration(cfg.CooldownBaseMS)*time.Millisecond {
		t.Errorf("expected recent activity penalty, got %v", cooldown)
	}
}

func TestCalcCooldown_RespectsMinMax(t *testing.T) {
	cfg := config.ImmersiveConfig{
		CooldownMinMS:    800,
		CooldownMaxMS:    1000,
		CooldownBaseMS:   5000,
		PrivateBaseMS:    200,
		ImmediateDelayMS: 120,
		JitterMS:         0,
	}
	buffer := &ImmersiveBuffer{cfg: cfg}

	cooldown := buffer.calcCooldown(false, false, false, 0, 0, 100)

	if cooldown > time.Duration(cfg.CooldownMaxMS)*time.Millisecond {
		t.Errorf("expected cooldown <= max %dms, got %v", cfg.CooldownMaxMS, cooldown)
	}
	if cooldown < time.Duration(cfg.CooldownMinMS)*time.Millisecond {
		t.Errorf("expected cooldown >= min %dms, got %v", cfg.CooldownMinMS, cooldown)
	}
}

func TestNormalizeImmersiveConfig_DefaultValues(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{})

	if cfg.CooldownMinMS != defaultCooldownMinMS {
		t.Errorf("expected CooldownMinMS %d, got %d", defaultCooldownMinMS, cfg.CooldownMinMS)
	}
	if cfg.CooldownMaxMS != defaultCooldownMaxMS {
		t.Errorf("expected CooldownMaxMS %d, got %d", defaultCooldownMaxMS, cfg.CooldownMaxMS)
	}
	if cfg.CooldownBaseMS != defaultCooldownBaseMS {
		t.Errorf("expected CooldownBaseMS %d, got %d", defaultCooldownBaseMS, cfg.CooldownBaseMS)
	}
	if cfg.WindowMS != defaultWindowMS {
		t.Errorf("expected WindowMS %d, got %d", defaultWindowMS, cfg.WindowMS)
	}
	if cfg.JitterMS != defaultJitterMS {
		t.Errorf("expected JitterMS %d, got %d", defaultJitterMS, cfg.JitterMS)
	}
	if cfg.MaxBatchMessages != defaultMaxBatchMessages {
		t.Errorf("expected MaxBatchMessages %d, got %d", defaultMaxBatchMessages, cfg.MaxBatchMessages)
	}
	if cfg.MaxBatchChars != defaultMaxBatchChars {
		t.Errorf("expected MaxBatchChars %d, got %d", defaultMaxBatchChars, cfg.MaxBatchChars)
	}
	if cfg.ImmediateDelayMS != defaultImmediateDelayMS {
		t.Errorf("expected ImmediateDelayMS %d, got %d", defaultImmediateDelayMS, cfg.ImmediateDelayMS)
	}
}

func TestNormalizeImmersiveConfig_PreservesValidValues(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		CooldownMinMS:    1000,
		CooldownMaxMS:    5000,
		CooldownBaseMS:   2000,
		WindowMS:         3000,
		JitterMS:         100,
		MaxBatchMessages: 20,
		MaxBatchChars:    2000,
		ImmediateDelayMS: 200,
	})

	if cfg.CooldownMinMS != 1000 {
		t.Errorf("expected CooldownMinMS 1000, got %d", cfg.CooldownMinMS)
	}
	if cfg.CooldownMaxMS != 5000 {
		t.Errorf("expected CooldownMaxMS 5000, got %d", cfg.CooldownMaxMS)
	}
	if cfg.CooldownBaseMS != 2000 {
		t.Errorf("expected CooldownBaseMS 2000, got %d", cfg.CooldownBaseMS)
	}
	if cfg.WindowMS != 3000 {
		t.Errorf("expected WindowMS 3000, got %d", cfg.WindowMS)
	}
	if cfg.JitterMS != 100 {
		t.Errorf("expected JitterMS 100, got %d", cfg.JitterMS)
	}
	if cfg.MaxBatchMessages != 20 {
		t.Errorf("expected MaxBatchMessages 20, got %d", cfg.MaxBatchMessages)
	}
	if cfg.MaxBatchChars != 2000 {
		t.Errorf("expected MaxBatchChars 2000, got %d", cfg.MaxBatchChars)
	}
	if cfg.ImmediateDelayMS != 200 {
		t.Errorf("expected ImmediateDelayMS 200, got %d", cfg.ImmediateDelayMS)
	}
}

func TestNormalizeImmersiveConfig_PreservesUnlimitedPostCooldownRounds(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		PostCooldownJudge: config.PostCooldownJudgeConfig{
			MaxRounds: 0,
		},
	})

	if cfg.PostCooldownJudge.MaxRounds != 0 {
		t.Errorf("expected PostCooldownJudge.MaxRounds 0 (unlimited), got %d", cfg.PostCooldownJudge.MaxRounds)
	}
}
