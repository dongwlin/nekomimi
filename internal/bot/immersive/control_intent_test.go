package immersive

import (
	"errors"
	"testing"

	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
)

func TestDecisionFromIntent(t *testing.T) {
	tests := []struct {
		name       string
		input      llmintent.ControlIntent
		wantAction controlAction
		wantWaitMS int
		wantReason string
	}{
		{
			name: "skip",
			input: llmintent.ControlIntent{
				Action: llmintent.ActionSkip,
				Reason: "low value",
			},
			wantAction: controlActionSkip,
			wantReason: "low value",
		},
		{
			name: "wait",
			input: llmintent.ControlIntent{
				Action: llmintent.ActionWait,
				WaitMS: 500,
				Reason: "more context",
			},
			wantAction: controlActionWait,
			wantWaitMS: 500,
			wantReason: "more context",
		},
		{
			name: "reply",
			input: llmintent.ControlIntent{
				Action: llmintent.ActionReply,
			},
			wantAction: controlActionReply,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := decisionFromIntent(tc.input)
			if decision.action != tc.wantAction {
				t.Fatalf("action mismatch: got %q, want %q", decision.action, tc.wantAction)
			}
			if decision.waitMS != tc.wantWaitMS {
				t.Fatalf("wait_ms mismatch: got %d, want %d", decision.waitMS, tc.wantWaitMS)
			}
			if decision.reason != tc.wantReason {
				t.Fatalf("reason mismatch: got %q, want %q", decision.reason, tc.wantReason)
			}
		})
	}
}

func TestIsControlIntentProtocolError(t *testing.T) {
	if !isControlIntentProtocolError(llmintent.ErrProtocol) {
		t.Fatal("expected protocol sentinel error to be detected")
	}
	wrapped := errors.New("base")
	if !isControlIntentProtocolError(errors.Join(wrapped, llmintent.ErrProtocol)) {
		t.Fatal("expected wrapped sentinel error to be treated as protocol error")
	}
	if isControlIntentProtocolError(errors.New("network timeout")) {
		t.Fatal("expected non-protocol error to be rejected")
	}
}
