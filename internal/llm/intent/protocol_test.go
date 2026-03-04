package intent

import (
	"errors"
	"testing"
)

func TestParse_SkipWaitReply(t *testing.T) {
	t.Run("skip", func(t *testing.T) {
		value, err := Parse(`{"action":"skip","reason":"low value"}`)
		if err != nil {
			t.Fatalf("parse skip failed: %v", err)
		}
		if value.Action != ActionSkip {
			t.Fatalf("action mismatch: got %q", value.Action)
		}
		if value.Reason != "low value" {
			t.Fatalf("reason mismatch: got %q", value.Reason)
		}
	})

	t.Run("wait with clamp", func(t *testing.T) {
		value, err := Parse(`{"action":"wait","wait_ms":6000,"reason":"still typing"}`)
		if err != nil {
			t.Fatalf("parse wait failed: %v", err)
		}
		if value.Action != ActionWait {
			t.Fatalf("action mismatch: got %q", value.Action)
		}
		if value.WaitMS != MaxWaitMS {
			t.Fatalf("wait_ms mismatch: got %d, want %d", value.WaitMS, MaxWaitMS)
		}
	})

	t.Run("reply", func(t *testing.T) {
		value, err := Parse(`{"action":"reply","reply_plan":"brief"}`)
		if err != nil {
			t.Fatalf("parse reply failed: %v", err)
		}
		if value.Action != ActionReply {
			t.Fatalf("action mismatch: got %q", value.Action)
		}
		if value.ReplyPlan != "brief" {
			t.Fatalf("reply_plan mismatch: got %q", value.ReplyPlan)
		}
	})
}

func TestParse_ProtocolErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{
			name:    "invalid payload",
			input:   `hello`,
			wantErr: ErrInvalidPayload,
		},
		{
			name:    "invalid action",
			input:   `{"action":"noop"}`,
			wantErr: ErrInvalidAction,
		},
		{
			name:    "wait missing wait_ms",
			input:   `{"action":"wait","reason":"typing"}`,
			wantErr: ErrWaitMSRequired,
		},
		{
			name:    "skip missing reason",
			input:   `{"action":"skip"}`,
			wantErr: ErrReasonRequired,
		},
		{
			name:    "unknown field",
			input:   `{"action":"reply","unknown":1}`,
			wantErr: ErrInvalidPayload,
		},
		{
			name:    "version field is rejected",
			input:   `{"version":"v1","action":"reply"}`,
			wantErr: ErrInvalidPayload,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.input)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("expected protocol error, got %v", err)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestParse_CodeFenceAndEmbeddedJSON(t *testing.T) {
	value, err := Parse("```json\n{\"action\":\"reply\"}\n```")
	if err != nil {
		t.Fatalf("parse codefence failed: %v", err)
	}
	if value.Action != ActionReply {
		t.Fatalf("action mismatch: got %q", value.Action)
	}

	value, err = Parse("prefix {\"action\":\"skip\",\"reason\":\"x\"} suffix")
	if err != nil {
		t.Fatalf("parse embedded object failed: %v", err)
	}
	if value.Action != ActionSkip {
		t.Fatalf("action mismatch: got %q", value.Action)
	}
}
