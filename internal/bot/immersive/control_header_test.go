package immersive

import (
	"errors"
	"strings"
	"testing"
)

func TestControlHeaderParserSkipWithReason(t *testing.T) {
	parser := newControlHeaderParser()

	decision, body, ready, err := parser.Consume("SKIP\nnot needed now\n")
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
	if decision.reason != "not needed now" {
		t.Fatalf("unexpected reason: %q", decision.reason)
	}
}

func TestControlHeaderParserWait1200WithReason(t *testing.T) {
	parser := newControlHeaderParser()

	decision, body, ready, err := parser.Consume("WAIT:1200\nuser still typing\n")
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
	if decision.reason != "user still typing" {
		t.Fatalf("unexpected reason: %q", decision.reason)
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
			input:   "WAIT:abc\nreason\n",
			wantErr: errControlHeaderInvalid,
		},
		{
			name:       "clamp low",
			input:      "WAIT:0\nreason\n",
			wantWaitMS: 1,
		},
		{
			name:       "clamp high",
			input:      "WAIT:6000\nreason\n",
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
			if decision.reason != "reason" {
				t.Fatalf("unexpected reason: %q", decision.reason)
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
	if decision.reason != "" {
		t.Fatalf("reply reason should be empty, got %q", decision.reason)
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

func TestControlHeaderParserMissingFirstLineNewline(t *testing.T) {
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

func TestControlHeaderParserMissingReason(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "skip", input: "SKIP\n"},
		{name: "wait", input: "WAIT:100\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := newControlHeaderParser()
			_, _, ready, err := parser.Consume(tc.input)
			if err != nil {
				t.Fatalf("unexpected consume error: %v", err)
			}
			if ready {
				t.Fatalf("expected ready=false before reason")
			}
			_, err = parser.Finalize()
			if !errors.Is(err, errControlHeaderReasonMissing) {
				t.Fatalf("expected missing-reason error, got %v", err)
			}
		})
	}
}

func TestControlHeaderParserInvalidReason(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "skip blank reason", input: "SKIP\n   \n"},
		{name: "wait blank reason", input: "WAIT:100\n \t \n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := newControlHeaderParser()
			_, _, _, err := parser.Consume(tc.input)
			if !errors.Is(err, errControlHeaderReasonInvalid) {
				t.Fatalf("expected invalid-reason error, got %v", err)
			}
		})
	}
}

func TestControlHeaderParserReasonTooLong(t *testing.T) {
	parser := newControlHeaderParser()
	_, _, _, err := parser.Consume("SKIP\n" + strings.Repeat("A", 201))
	if !errors.Is(err, errControlHeaderReasonTooLong) {
		t.Fatalf("expected reason-too-long error, got %v", err)
	}
}

func TestControlHeaderParserUnexpectedContentAfterSkipOrWait(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "skip with third line content",
			input: "SKIP\nlow value\nfoo",
		},
		{
			name:  "wait with third line content",
			input: "WAIT:100\nneed one more message\nbar",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := newControlHeaderParser()
			_, _, _, err := parser.Consume(tc.input)
			if !errors.Is(err, errControlHeaderUnexpectedContent) {
				t.Fatalf("expected unexpected-content error, got %v", err)
			}
		})
	}
}

func TestControlHeaderParserCaseAndWhitespaceVariants(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAction controlAction
		wantWaitMS int
		wantReason string
		wantBody   string
	}{
		{
			name:       "lower skip with padded reason",
			input:      "skip\n  low value intervention  \n",
			wantAction: controlActionSkip,
			wantReason: "low value intervention",
		},
		{
			name:       "wait with spaces and crlf",
			input:      " wait: 10 \r\n user still typing \r\n",
			wantAction: controlActionWait,
			wantWaitMS: 10,
			wantReason: "user still typing",
		},
		{
			name:       "reply with trailing space and crlf",
			input:      "reply \r\nhello",
			wantAction: controlActionReply,
			wantBody:   "hello",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := newControlHeaderParser()
			decision, body, ready, err := parser.Consume(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ready {
				t.Fatal("expected ready=true")
			}
			if decision.action != tc.wantAction {
				t.Fatalf("unexpected action: got %q, want %q", decision.action, tc.wantAction)
			}
			if decision.waitMS != tc.wantWaitMS {
				t.Fatalf("unexpected wait_ms: got %d, want %d", decision.waitMS, tc.wantWaitMS)
			}
			if decision.reason != tc.wantReason {
				t.Fatalf("unexpected reason: got %q, want %q", decision.reason, tc.wantReason)
			}
			if body != tc.wantBody {
				t.Fatalf("unexpected body: got %q, want %q", body, tc.wantBody)
			}
		})
	}
}

func TestParseControlHeaderFallback(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantOK     bool
		wantAction controlAction
		wantWaitMS int
		wantReason string
		wantBody   string
	}{
		{
			name:       "reply inline with separator",
			input:      "REPLY: hello",
			wantOK:     true,
			wantAction: controlActionReply,
			wantBody:   "hello",
		},
		{
			name:       "reply multiline body",
			input:      "REPLY\nhello",
			wantOK:     true,
			wantAction: controlActionReply,
			wantBody:   "hello",
		},
		{
			name:       "wait with reason",
			input:      "WAIT: 600\nbecause user is still typing",
			wantOK:     true,
			wantAction: controlActionWait,
			wantWaitMS: 600,
			wantReason: "because user is still typing",
		},
		{
			name:       "skip with reason",
			input:      "SKIP\nintervention is unnecessary",
			wantOK:     true,
			wantAction: controlActionSkip,
			wantReason: "intervention is unnecessary",
		},
		{
			name:   "wait missing reason",
			input:  "WAIT: 600",
			wantOK: false,
		},
		{
			name:   "wait with extra content",
			input:  "WAIT: 600\nneed more context\nextra",
			wantOK: false,
		},
		{
			name:   "invalid plain text",
			input:  "hello there",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision, body, ok := parseControlHeaderFallback(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("unexpected ok: got %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if decision.action != tc.wantAction {
				t.Fatalf("unexpected action: got %q, want %q", decision.action, tc.wantAction)
			}
			if decision.waitMS != tc.wantWaitMS {
				t.Fatalf("unexpected wait_ms: got %d, want %d", decision.waitMS, tc.wantWaitMS)
			}
			if decision.reason != tc.wantReason {
				t.Fatalf("unexpected reason: got %q, want %q", decision.reason, tc.wantReason)
			}
			if body != tc.wantBody {
				t.Fatalf("unexpected body: got %q, want %q", body, tc.wantBody)
			}
		})
	}
}
