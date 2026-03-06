package immersive

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestBehaviorTransitions_IdleAddressedInThreadCoolingDown(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	session := newImmersiveSession(now)

	session.mu.Lock()
	session.observeIncomingMessageLocked(queuedMessage{
		text:             "neko 在吗",
		speaker:          "name=alice",
		ts:               now,
		chars:            len([]rune("neko 在吗")),
		isAddressedToBot: true,
		isQuestion:       true,
	}, false, now)
	snapshot := session.snapshotBehaviorLocked(now)
	if snapshot.Mode != ModeAddressed {
		t.Fatalf("expected addressed mode, got %q", snapshot.Mode)
	}
	if snapshot.FocusSpeaker != "name=alice" {
		t.Fatalf("expected focus speaker alice, got %q", snapshot.FocusSpeaker)
	}

	threadAt := now.Add(15 * time.Second)
	session.observeIncomingMessageLocked(queuedMessage{
		text:       "刚才那个功能我再问一下",
		speaker:    "name=alice",
		ts:         threadAt,
		chars:      len([]rune("刚才那个功能我再问一下")),
		isQuestion: true,
	}, false, threadAt)
	snapshot = session.snapshotBehaviorLocked(threadAt)
	if snapshot.Mode != ModeInThread {
		t.Fatalf("expected in_thread mode, got %q", snapshot.Mode)
	}

	replyAt := threadAt.Add(2 * time.Second)
	session.noteAssistantDeliveredLocked("好的，我看看", replyAt)
	snapshot = session.snapshotBehaviorLocked(replyAt)
	session.mu.Unlock()

	if snapshot.Mode != ModeCoolingDown {
		t.Fatalf("expected cooling_down mode after reply, got %q", snapshot.Mode)
	}
	if snapshot.PendingQuestion {
		t.Fatal("non-question reply should not leave pending question")
	}
}

func TestEnergySettlesSlowlyTowardTarget(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	session := newImmersiveSession(now)

	session.mu.Lock()
	session.mode = ModeIdle
	session.energy = 20
	session.energyTarget = energyTargetForMode(ModeIdle, session.energyBaseline)
	session.lastEnergyUpdateAt = now.Add(-60 * time.Second)
	session.settleEnergyLocked(now, "test_recovery")
	got := session.energy
	target := session.energyTarget
	session.mu.Unlock()

	if got <= 20 {
		t.Fatalf("expected energy to recover above 20, got %.2f", got)
	}
	if got >= target {
		t.Fatalf("expected energy to recover gradually below target %.2f, got %.2f", target, got)
	}
}

func TestWeakSignalsDoNotDrainEnergy(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	session := newImmersiveSession(now)

	session.mu.Lock()
	before := session.energy
	session.observeIncomingMessageLocked(queuedMessage{
		text:    "路过说一句",
		speaker: "name=bob",
		ts:      now.Add(10 * time.Second),
		chars:   len([]rune("路过说一句")),
	}, false, now.Add(10*time.Second))
	after := session.energy
	session.mu.Unlock()

	if after < before {
		t.Fatalf("expected weak signal to keep or nudge energy, before=%.2f after=%.2f", before, after)
	}
	if after-before > 2 {
		t.Fatalf("expected weak signal nudge to stay small, before=%.2f after=%.2f", before, after)
	}
}

func TestFlush_CoolingDownSmallTalkSkipsBeforeLLM(t *testing.T) {
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

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
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
		t.Fatalf("expected speak gate to skip control call during cooling down, got %d", got)
	}
}

func TestFlush_StrongAddressedReopensCoolingDown(t *testing.T) {
	var callCount int64
	control := mustMarshalJSON(t, map[string]any{
		"action": "skip",
		"reason": "mentioned again",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		atomic.AddInt64(&callCount, 1)
		return scriptedResponse{JSONBody: responsesOutputTextJSON(control)}
	})
	defer server.Close()

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	state := buffer.session(sessionKey)
	now := time.Now()
	msg := queuedMessage{
		text:             "neko 在吗",
		speaker:          "name=alice",
		ts:               now,
		chars:            len([]rune("neko 在吗")),
		isAddressedToBot: true,
		isQuestion:       true,
	}

	var before, after float64
	state.mu.Lock()
	state.noteAssistantDeliveredLocked("先这样", now.Add(-10*time.Second))
	state.energy = 18
	state.energyTarget = energyTargetForMode(ModeCoolingDown, state.energyBaseline)
	before = state.energy
	state.observeIncomingMessageLocked(msg, false, now)
	after = state.energy
	state.mu.Unlock()

	seedQueueForFlushTest(buffer, sessionKey, 0, msg)
	buffer.flush(sessionKey)

	if got := atomic.LoadInt64(&callCount); got != 1 {
		t.Fatalf("expected explicit address to reopen control call during cooling down, got %d", got)
	}
	if after <= before {
		t.Fatalf("expected addressed message to rebound energy, before=%.2f after=%.2f", before, after)
	}
}

func TestFlush_WaitingUserNoiseSkipsBeforeLLM(t *testing.T) {
	var callCount int64
	control := mustMarshalJSON(t, map[string]any{
		"action": "skip",
		"reason": "still waiting",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		atomic.AddInt64(&callCount, 1)
		return scriptedResponse{JSONBody: responsesOutputTextJSON(control)}
	})
	defer server.Close()

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	state := buffer.session(sessionKey)
	now := time.Now()
	msg := queuedMessage{
		text:    "我先去吃饭了",
		speaker: "name=bob",
		ts:      now,
		chars:   len([]rune("我先去吃饭了")),
	}

	state.mu.Lock()
	state.focusSpeaker = "name=alice"
	state.noteAssistantDeliveredLocked("你觉得呢？", now.Add(-10*time.Second))
	state.observeIncomingMessageLocked(msg, false, now)
	snapshot := state.snapshotBehaviorLocked(now)
	state.mu.Unlock()

	if snapshot.Mode != ModeWaitingUser {
		t.Fatalf("expected waiting_user before flush, got %q", snapshot.Mode)
	}

	seedQueueForFlushTest(buffer, sessionKey, 0, msg)
	buffer.flush(sessionKey)

	if got := atomic.LoadInt64(&callCount); got != 0 {
		t.Fatalf("expected waiting_user ambient noise to be skipped before control call, got %d", got)
	}
}
