package immersive

import (
	"errors"
	"strings"
	"testing"
)

func TestControlHeaderParserSkip(t *testing.T) {
	parser := newControlHeaderParser()

	decision, body, ready, err := parser.Consume("SKIP\n")
	if err != nil {
		t.Fatalf("consume skip failed: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true")
	}
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
	if decision.action != controlActionSkip {
		t.Fatalf("expected skip action, got %q", decision.action)
	}
}

func TestControlHeaderParserWait1200(t *testing.T) {
	parser := newControlHeaderParser()

	decision, body, ready, err := parser.Consume("WAIT:1200\n")
	if err != nil {
		t.Fatalf("consume wait failed: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true")
	}
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
	if decision.action != controlActionWait {
		t.Fatalf("expected wait action, got %q", decision.action)
	}
	if decision.waitMS != 1200 {
		t.Fatalf("expected wait_ms=1200, got %d", decision.waitMS)
	}
}

func TestControlHeaderParserWaitInvalidAndClamp(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantWaitMS int
		wantErr    error
	}{
		{
			name:    "invalid number",
			input:   "WAIT:abc\n",
			wantErr: errControlHeaderInvalid,
		},
		{
			name:       "clamp low",
			input:      "WAIT:0\n",
			wantWaitMS: 1,
		},
		{
			name:       "clamp high",
			input:      "WAIT:6000\n",
			wantWaitMS: 3000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := newControlHeaderParser()
			decision, _, ready, err := parser.Consume(tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ready {
				t.Fatalf("expected ready=true")
			}
			if decision.action != controlActionWait {
				t.Fatalf("expected wait action, got %q", decision.action)
			}
			if decision.waitMS != tc.wantWaitMS {
				t.Fatalf("expected wait_ms=%d, got %d", tc.wantWaitMS, decision.waitMS)
			}
		})
	}
}

func TestControlHeaderParserReplyAndBodyStreaming(t *testing.T) {
	parser := newControlHeaderParser()

	decision, body, ready, err := parser.Consume("REPLY\nhello")
	if err != nil {
		t.Fatalf("consume reply failed: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true")
	}
	if decision.action != controlActionReply {
		t.Fatalf("expected reply action, got %q", decision.action)
	}
	if body != "hello" {
		t.Fatalf("expected first body segment 'hello', got %q", body)
	}

	decision, body, ready, err = parser.Consume(" world")
	if err != nil {
		t.Fatalf("consume second delta failed: %v", err)
	}
	if !ready {
		t.Fatalf("expected ready=true for second delta")
	}
	if decision.action != controlActionReply {
		t.Fatalf("expected reply action on second delta, got %q", decision.action)
	}
	if body != " world" {
		t.Fatalf("expected forwarded body delta, got %q", body)
	}
}

func TestControlHeaderParserFirstLineTooLong(t *testing.T) {
	parser := newControlHeaderParser()

	_, _, _, err := parser.Consume(strings.Repeat("A", 65))
	if !errors.Is(err, errControlHeaderTooLong) {
		t.Fatalf("expected too-long error, got %v", err)
	}
}

func TestControlHeaderParserMissingNewline(t *testing.T) {
	parser := newControlHeaderParser()

	_, _, ready, err := parser.Consume("REPLY")
	if err != nil {
		t.Fatalf("unexpected consume error: %v", err)
	}
	if ready {
		t.Fatalf("expected ready=false before newline")
	}

	_, err = parser.Finalize()
	if !errors.Is(err, errControlHeaderMissingNewline) {
		t.Fatalf("expected missing-newline error, got %v", err)
	}
}
