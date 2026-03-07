package immersive

import (
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestDebugSnapshot_TracksSkipWaitReplyFlows(t *testing.T) {
	t.Run("skip", func(t *testing.T) {
		intent := mustMarshalJSON(t, map[string]any{
			"action": "skip",
			"reason": "low_value",
		})
		server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
			return scriptedResponse{JSONBody: responsesOutputTextJSON(intent)}
		})
		defer server.Close()

		buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
		seedQueueForFlushTest(buffer, sessionKey, 0)
		buffer.flush(sessionKey)

		snapshot := buffer.DebugSnapshot(sessionKey)
		if snapshot.LastFinalAction != "skip" {
			t.Fatalf("expected final action skip, got %q", snapshot.LastFinalAction)
		}
		if snapshot.LastControlAction != string(controlActionSkip) {
			t.Fatalf("expected control action skip, got %q", snapshot.LastControlAction)
		}
		if snapshot.LastControlReasonCode != "model" {
			t.Fatalf("expected control reason code model, got %q", snapshot.LastControlReasonCode)
		}
	})

	t.Run("wait", func(t *testing.T) {
		intent := mustMarshalJSON(t, map[string]any{
			"action":  "wait",
			"wait_ms": 120,
			"reason":  "still_typing",
		})
		server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
			return scriptedResponse{JSONBody: responsesOutputTextJSON(intent)}
		})
		defer server.Close()

		buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
		seedQueueForFlushTest(buffer, sessionKey, 0)
		buffer.flush(sessionKey)

		state := buffer.session(sessionKey)
		state.mu.Lock()
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		state.mu.Unlock()

		snapshot := buffer.DebugSnapshot(sessionKey)
		if snapshot.LastFinalAction != "wait" {
			t.Fatalf("expected final action wait, got %q", snapshot.LastFinalAction)
		}
		if snapshot.LastControlAction != string(controlActionWait) {
			t.Fatalf("expected control action wait, got %q", snapshot.LastControlAction)
		}
		if snapshot.LastSchedulerReason == "" {
			t.Fatal("expected merged scheduler reason to be recorded")
		}
	})

	t.Run("reply", func(t *testing.T) {
		intent := mustMarshalJSON(t, map[string]any{
			"action": "reply",
		})
		longReply := strings.Repeat("reply-preview-", 20)
		server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
			if !stream {
				return scriptedResponse{JSONBody: responsesOutputTextJSON(intent)}
			}
			return scriptedResponse{
				SSEEvents: []string{
					mustResponsesDeltaEventRaw(t, longReply),
					`{"type":"response.completed"}`,
				},
			}
		})
		defer server.Close()

		buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
		seedQueueForFlushTest(buffer, sessionKey, 0)
		buffer.flush(sessionKey)

		snapshot := buffer.DebugSnapshot(sessionKey)
		if snapshot.LastFinalAction != "reply" {
			t.Fatalf("expected final action reply, got %q", snapshot.LastFinalAction)
		}
		if snapshot.LastControlAction != string(controlActionReply) {
			t.Fatalf("expected control action reply, got %q", snapshot.LastControlAction)
		}
		if snapshot.LastReplyPreview == "" {
			t.Fatal("expected reply preview to be recorded")
		}
		if snapshot.LastReplyPreview == longReply {
			t.Fatal("expected reply preview to be truncated, got full reply")
		}
		if len([]rune(snapshot.LastReplyPreview)) > debugReplyPreviewChars+3 {
			t.Fatalf("reply preview too long: %d", len([]rune(snapshot.LastReplyPreview)))
		}
	})
}

func TestDebugSnapshot_TracksFastRecoveryAndProactiveKind(t *testing.T) {
	now := time.Date(2026, 3, 7, 13, 0, 0, 0, time.Local)
	session := newImmersiveSession(now)

	session.mu.Lock()
	session.energy = 10
	session.energyTarget = 60
	if !session.maybeFastRecoveryLocked(1.0, 10, "test_fast_recovery", now) {
		session.mu.Unlock()
		t.Fatal("expected fast recovery to trigger")
	}
	session.recordProactiveLocked("followup", "scheduled", "assistant_question", false, now)
	snapshot := session.snapshotDebugLocked("group:debug", now)
	session.mu.Unlock()

	if snapshot.EnergyLastFastRecoverReason != "test_fast_recovery" {
		t.Fatalf("unexpected fast recover reason: %q", snapshot.EnergyLastFastRecoverReason)
	}
	if snapshot.LastProactiveKind != "followup" {
		t.Fatalf("unexpected proactive kind: %q", snapshot.LastProactiveKind)
	}
	if snapshot.LastProactiveStatus != "scheduled" {
		t.Fatalf("unexpected proactive status: %q", snapshot.LastProactiveStatus)
	}
}
