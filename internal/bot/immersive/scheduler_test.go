package immersive

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestComputeFlushDecision_StrongCallFastDelay(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 600,
			MaxBatchWaitMS: 3000,
			MaxBatchSize:   15,
		},
		Scheduler: config.SchedulerConfig{
			StrongCallMinDelayMS: 200,
		},
	}, nil, []string{"neko"})

	now := time.Now()
	session := &immersiveSession{
		nextBatch: []queuedMessage{
			{text: "neko 在吗", isMentionBot: true, ts: now},
		},
		batchStartTime: now,
	}

	decision := buffer.computeFlushDecision("group:1", session, now)
	if decision.Priority != "fast" {
		t.Fatalf("expected priority=fast for strong call, got %q", decision.Priority)
	}
	if decision.Delay != 200*time.Millisecond {
		t.Fatalf("expected 200ms delay for strong call, got %s", decision.Delay)
	}
	if decision.Reason != "strong_call" {
		t.Fatalf("expected reason=strong_call, got %q", decision.Reason)
	}
}

func TestComputeFlushDecision_PrivateSessionImmediate(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 600,
		},
	}, nil, nil)

	decision := buffer.computeFlushDecision("private:1", nil, time.Now())
	if decision.Priority != "immediate" {
		t.Fatalf("expected priority=immediate for private session, got %q", decision.Priority)
	}
	if decision.Delay != 0 {
		t.Fatalf("expected 0 delay for private session, got %s", decision.Delay)
	}
}

func TestComputeFlushDecision_BatchFullImmediate(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 600,
			MaxBatchSize:   2,
		},
	}, nil, nil)

	now := time.Now()
	session := &immersiveSession{
		nextBatch:      []queuedMessage{{text: "a"}, {text: "b"}},
		batchStartTime: now,
	}

	decision := buffer.computeFlushDecision("group:1", session, now)
	if decision.Priority != "immediate" {
		t.Fatalf("expected priority=immediate for batch full, got %q", decision.Priority)
	}
}

func TestComputeFlushDecision_CooldownDeferredDelay(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 600,
			MaxBatchWaitMS: 30000,
		},
		Scheduler: config.SchedulerConfig{
			PostReplyDelayMS: 8000,
		},
	}, nil, nil)

	now := time.Now()
	session := &immersiveSession{
		nextBatch:      []queuedMessage{{text: "路过", ts: now}},
		batchStartTime: now,
		mode:           ModeCoolingDown,
		lastBotReplyAt: now.Add(-2 * time.Second),
	}

	decision := buffer.computeFlushDecision("group:1", session, now)
	if decision.Priority != "deferred" {
		t.Fatalf("expected priority=deferred during cooldown, got %q", decision.Priority)
	}
	if decision.Delay < 5*time.Second {
		t.Fatalf("expected deferred delay to be substantial during cooldown, got %s", decision.Delay)
	}
}

func TestComputeFlushDecision_CooldownStrongCallOverrides(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 600,
			MaxBatchWaitMS: 30000,
		},
		Scheduler: config.SchedulerConfig{
			PostReplyDelayMS:     8000,
			StrongCallMinDelayMS: 200,
		},
	}, nil, nil)

	now := time.Now()
	session := &immersiveSession{
		nextBatch: []queuedMessage{
			{text: "neko 来", isMentionBot: true, ts: now},
		},
		batchStartTime: now,
		mode:           ModeCoolingDown,
		lastBotReplyAt: now.Add(-2 * time.Second),
	}

	decision := buffer.computeFlushDecision("group:1", session, now)
	if decision.Priority != "fast" {
		t.Fatalf("expected strong call to override cooldown, got priority=%q", decision.Priority)
	}
	if decision.Delay > 600*time.Millisecond {
		t.Fatalf("expected short delay for strong call in cooldown, got %s", decision.Delay)
	}
}

func TestComputeFlushDecision_NormalDebounce(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 600,
		},
	}, nil, nil)

	now := time.Now()
	session := &immersiveSession{
		nextBatch:      []queuedMessage{{text: "hello", ts: now}},
		batchStartTime: now,
		mode:           ModeIdle,
	}

	decision := buffer.computeFlushDecision("group:1", session, now)
	if decision.Priority != "normal" {
		t.Fatalf("expected priority=normal for regular message, got %q", decision.Priority)
	}
	if decision.Delay != 600*time.Millisecond {
		t.Fatalf("expected 600ms normal debounce, got %s", decision.Delay)
	}
}

func TestMergeWaitDecision_ModelWaitOverridesNormal(t *testing.T) {
	local := FlushDecision{Delay: 600 * time.Millisecond, Reason: "normal_debounce", Priority: "normal"}
	merged := mergeWaitDecision(local, 2000)

	if merged.Delay != 2000*time.Millisecond {
		t.Fatalf("expected model wait to override normal debounce, got %s", merged.Delay)
	}
	if merged.Reason != "model_wait" {
		t.Fatalf("expected reason=model_wait, got %q", merged.Reason)
	}
}

func TestMergeWaitDecision_CooldownOverridesModelWait(t *testing.T) {
	local := FlushDecision{Delay: 6 * time.Second, Reason: "post_reply_cooldown", Priority: "deferred"}
	merged := mergeWaitDecision(local, 600)

	if merged.Delay != 6*time.Second {
		t.Fatalf("expected cooldown to override shorter model wait, got %s", merged.Delay)
	}
	if merged.Reason != "post_reply_cooldown" {
		t.Fatalf("expected original cooldown reason, got %q", merged.Reason)
	}
}

func TestMergeWaitDecision_ZeroLLMWait(t *testing.T) {
	local := FlushDecision{Delay: 600 * time.Millisecond, Reason: "normal_debounce", Priority: "normal"}
	merged := mergeWaitDecision(local, 0)

	if merged.Delay != local.Delay {
		t.Fatalf("expected zero llm wait to use local delay, got %s", merged.Delay)
	}
}

func TestComputeFlushDecision_MaxDeadlineRespected(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 600,
			MaxBatchWaitMS: 100,
		},
	}, nil, nil)

	now := time.Now()
	session := &immersiveSession{
		nextBatch:      []queuedMessage{{text: "hello", ts: now}},
		batchStartTime: now.Add(-200 * time.Millisecond),
	}

	decision := buffer.computeFlushDecision("group:1", session, now)
	if decision.Delay != 0 {
		t.Fatalf("expected immediate flush after max deadline, got %s", decision.Delay)
	}
}

func TestColdOpenEligibility_QuietBreakDetected(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		Scheduler: config.SchedulerConfig{
			QuietThresholdMS: 300000,
		},
	}, nil, nil)

	now := time.Now()
	session := newImmersiveSession(now)

	session.mu.Lock()
	session.lastMessageAt = now.Add(-6 * time.Minute)
	buffer.detectColdOpenEligibilityLocked(session, now)
	eligible := session.coldOpenEligible
	actCount := session.coldOpenActivityCount
	session.mu.Unlock()

	if !eligible {
		t.Fatal("expected cold-open eligible after quiet break")
	}
	if actCount != 1 {
		t.Fatalf("expected activity count=1, got %d", actCount)
	}
}

func TestColdOpenEligibility_ClearsAfterMaxMessages(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		Scheduler: config.SchedulerConfig{
			QuietThresholdMS: 300000,
		},
	}, nil, nil)

	now := time.Now()
	session := newImmersiveSession(now)

	session.mu.Lock()
	session.lastMessageAt = now.Add(-6 * time.Minute)
	buffer.detectColdOpenEligibilityLocked(session, now)
	buffer.detectColdOpenEligibilityLocked(session, now.Add(1*time.Second))
	buffer.detectColdOpenEligibilityLocked(session, now.Add(2*time.Second))
	eligible := session.coldOpenEligible
	session.mu.Unlock()

	if eligible {
		t.Fatal("expected cold-open to clear after max messages exceeded")
	}
}

func TestColdOpenEligibility_RespectsCooldown(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		Scheduler: config.SchedulerConfig{
			QuietThresholdMS:      300000,
			ColdOpenMinIntervalMS: 600000,
		},
	}, nil, nil)

	now := time.Now()
	session := newImmersiveSession(now)

	session.mu.Lock()
	session.lastMessageAt = now.Add(-6 * time.Minute)
	session.nextColdOpenEligibleAt = now.Add(5 * time.Minute)
	buffer.detectColdOpenEligibilityLocked(session, now)
	eligible := session.coldOpenEligible
	session.mu.Unlock()

	if eligible {
		t.Fatal("expected cold-open blocked by cooldown interval")
	}
}

func TestColdOpenSignal_BoostsScore(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	meta := queueMeta{
		MessagesCount: 1,
		LastSpeaker:   "name=alice",
	}

	withColdOpen := behaviorSnapshot{Mode: ModeIdle, ColdOpenEligible: true}
	withoutColdOpen := behaviorSnapshot{Mode: ModeIdle, ColdOpenEligible: false}

	scoreWith := scoreSignals("group:1", meta, withColdOpen, now)
	scoreWithout := scoreSignals("group:1", meta, withoutColdOpen, now)

	if scoreWith.TotalScore <= scoreWithout.TotalScore {
		t.Fatalf("cold-open should boost score: with=%d without=%d", scoreWith.TotalScore, scoreWithout.TotalScore)
	}
}

func TestFollowupTimer_SetOnBotQuestion(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		Scheduler: config.SchedulerConfig{
			FollowupWaitMS: 5000,
		},
	}, nil, []string{"neko"})

	sessionKey := "group:followup"
	state := buffer.session(sessionKey)

	state.mu.Lock()
	state.mode = ModeAddressed
	state.focusSpeaker = "name=alice"
	state.energy = 60
	state.energyTarget = 60
	state.mu.Unlock()

	buffer.noteAssistantDelivered(sessionKey, "你觉得呢？")

	state.mu.Lock()
	hasPending := state.pendingQuestion
	hasTimer := state.followupTimer != nil
	if state.followupTimer != nil {
		state.followupTimer.Stop()
		state.followupTimer = nil
	}
	state.mu.Unlock()

	if !hasPending {
		t.Fatal("expected pendingQuestion to be set after bot question")
	}
	if !hasTimer {
		t.Fatal("expected followup timer to be scheduled after bot question")
	}
}

func TestFollowupTimer_NotSetOnNonQuestion(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		Scheduler: config.SchedulerConfig{
			FollowupWaitMS: 5000,
		},
	}, nil, []string{"neko"})

	sessionKey := "group:nofollowup"
	state := buffer.session(sessionKey)

	state.mu.Lock()
	state.mode = ModeAddressed
	state.focusSpeaker = "name=alice"
	state.energy = 60
	state.energyTarget = 60
	state.mu.Unlock()

	buffer.noteAssistantDelivered(sessionKey, "好的，没问题。")

	state.mu.Lock()
	hasPending := state.pendingQuestion
	hasTimer := state.followupTimer != nil
	state.mu.Unlock()

	if hasPending {
		t.Fatal("expected no pendingQuestion for non-question reply")
	}
	if hasTimer {
		t.Fatal("expected no followup timer for non-question reply")
	}
}

func TestFollowupTimer_CancelledOnClearPendingQuestion(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{
		Scheduler: config.SchedulerConfig{
			FollowupWaitMS: 60000,
		},
	}, nil, []string{"neko"})

	sessionKey := "group:cancel"
	state := buffer.session(sessionKey)

	state.mu.Lock()
	state.mode = ModeAddressed
	state.focusSpeaker = "name=alice"
	state.energy = 60
	state.energyTarget = 60
	state.mu.Unlock()

	buffer.noteAssistantDelivered(sessionKey, "你觉得呢？")

	state.mu.Lock()
	hasTimerBefore := state.followupTimer != nil
	state.clearPendingQuestionLocked()
	hasTimerAfter := state.followupTimer != nil
	state.mu.Unlock()

	if !hasTimerBefore {
		t.Fatal("expected timer before clear")
	}
	if hasTimerAfter {
		t.Fatal("expected timer cancelled after clearPendingQuestion")
	}
}

func TestFlush_CooldownWeakSignalDeferredSchedule(t *testing.T) {
	var callCount int64
	control := mustMarshalJSON(t, map[string]any{
		"action": "skip",
		"reason": "quiet",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		atomic.AddInt64(&callCount, 1)
		return scriptedResponse{JSONBody: responsesOutputTextJSON(control)}
	})
	defer server.Close()

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{
		Scheduler: config.SchedulerConfig{
			PostReplyDelayMS: 8000,
		},
	})
	state := buffer.session(sessionKey)
	now := time.Now()
	msg := queuedMessage{
		text:    "今天天气不错",
		speaker: "name=bob",
		ts:      now,
		chars:   len([]rune("今天天气不错")),
	}

	state.mu.Lock()
	state.noteAssistantDeliveredLocked("好的", now.Add(-10*time.Second))
	state.observeIncomingMessageLocked(msg, false, now)
	state.mu.Unlock()

	seedQueueForFlushTest(buffer, sessionKey, 0, msg)
	buffer.flush(sessionKey)

	if got := atomic.LoadInt64(&callCount); got != 0 {
		t.Fatalf("expected speak gate to skip weak signal during cooling down, got %d calls", got)
	}
}

func TestFlush_MergedWaitUsesLongerDelay(t *testing.T) {
	intent := mustMarshalJSON(t, map[string]any{
		"action":  "wait",
		"wait_ms": 120,
		"reason":  "user is still typing",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		return scriptedResponse{JSONBody: responsesOutputTextJSON(intent)}
	})
	defer server.Close()

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 600,
			MaxBatchWaitMS: 3000,
		},
	})
	seedQueueForFlushTest(buffer, sessionKey, 0)
	buffer.flush(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	if got := len(state.nextBatch); got != 1 {
		t.Fatalf("expected requeue on wait, got %d", got)
	}
	if state.waitRounds != 1 {
		t.Fatalf("expected waitRounds=1, got %d", state.waitRounds)
	}
	if state.timer == nil {
		t.Fatal("expected wait timer to be scheduled")
	}
	state.timer.Stop()
	state.timer = nil
}

func TestNormalizeImmersiveConfig_SchedulerDefaults(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{})

	if cfg.Scheduler.PostReplyDelayMS != defaultPostReplyDelayMS {
		t.Fatalf("expected PostReplyDelayMS=%d, got %d", defaultPostReplyDelayMS, cfg.Scheduler.PostReplyDelayMS)
	}
	if cfg.Scheduler.StrongCallMinDelayMS != defaultStrongCallMinDelayMS {
		t.Fatalf("expected StrongCallMinDelayMS=%d, got %d", defaultStrongCallMinDelayMS, cfg.Scheduler.StrongCallMinDelayMS)
	}
	if cfg.Scheduler.FollowupWaitMS != defaultFollowupWaitMS {
		t.Fatalf("expected FollowupWaitMS=%d, got %d", defaultFollowupWaitMS, cfg.Scheduler.FollowupWaitMS)
	}
	if cfg.Scheduler.ColdOpenMinIntervalMS != defaultColdOpenMinIntervalMS {
		t.Fatalf("expected ColdOpenMinIntervalMS=%d, got %d", defaultColdOpenMinIntervalMS, cfg.Scheduler.ColdOpenMinIntervalMS)
	}
	if cfg.Scheduler.QuietThresholdMS != defaultQuietThresholdMS {
		t.Fatalf("expected QuietThresholdMS=%d, got %d", defaultQuietThresholdMS, cfg.Scheduler.QuietThresholdMS)
	}
}

func TestNormalizeImmersiveConfig_SchedulerPreservesCustomValues(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		Scheduler: config.SchedulerConfig{
			PostReplyDelayMS:      5000,
			StrongCallMinDelayMS:  100,
			FollowupWaitMS:        60000,
			ColdOpenMinIntervalMS: 300000,
			QuietThresholdMS:      120000,
		},
	})

	if cfg.Scheduler.PostReplyDelayMS != 5000 {
		t.Fatalf("expected preserved PostReplyDelayMS=5000, got %d", cfg.Scheduler.PostReplyDelayMS)
	}
	if cfg.Scheduler.StrongCallMinDelayMS != 100 {
		t.Fatalf("expected preserved StrongCallMinDelayMS=100, got %d", cfg.Scheduler.StrongCallMinDelayMS)
	}
}

func TestCountRecentUserMessages(t *testing.T) {
	base := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	buffer := []queuedMessage{
		{kind: EventUserMessage, ts: base.Add(-10 * time.Second)},
		{kind: EventUserMessage, ts: base.Add(-5 * time.Second)},
		{kind: EventAssistantText, ts: base.Add(-3 * time.Second)},
		{kind: EventUserMessage, ts: base.Add(1 * time.Second)},
		{kind: EventUserMessage, ts: base.Add(2 * time.Second)},
	}

	count := countRecentUserMessages(buffer, base)
	if count != 2 {
		t.Fatalf("expected 2 recent user messages after base, got %d", count)
	}
}

func TestBatchHasStrongSignal(t *testing.T) {
	strong := []queuedMessage{
		{text: "hello", isMentionBot: true},
	}
	weak := []queuedMessage{
		{text: "hello"},
	}
	nickStart := []queuedMessage{
		{text: "neko hi", nicknamePosition: NickStart},
	}

	if !batchHasStrongSignal(strong) {
		t.Fatal("expected mention to be strong signal")
	}
	if batchHasStrongSignal(weak) {
		t.Fatal("expected no-signal batch to not be strong")
	}
	if !batchHasStrongSignal(nickStart) {
		t.Fatal("expected nickname at start to be strong signal")
	}
}
