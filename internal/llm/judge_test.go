package llm

import "testing"

func TestParseSpeakGateDecision_ParseReplyAndSkip(t *testing.T) {
	replyDecision, ok := parseSpeakGateDecision("REPLY")
	if !ok {
		t.Fatalf("expected REPLY to parse")
	}
	if replyDecision.Action != SpeakGateActionReply {
		t.Fatalf("expected REPLY action, got %q", replyDecision.Action)
	}

	skipDecision, ok := parseSpeakGateDecision("SKIP")
	if !ok {
		t.Fatalf("expected SKIP to parse")
	}
	if skipDecision.Action != SpeakGateActionSkip {
		t.Fatalf("expected SKIP action, got %q", skipDecision.Action)
	}
}

func TestParseSpeakGateDecision_ParseWaitAndClamp(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected int
	}{
		{name: "normal", raw: "WAIT:1200", expected: 1200},
		{name: "too_large", raw: "WAIT:5000", expected: 3000},
		{name: "zero", raw: "WAIT:0", expected: 1},
		{name: "missing_number", raw: "WAIT", expected: 1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision, ok := parseSpeakGateDecision(tc.raw)
			if !ok {
				t.Fatalf("expected parse success for %q", tc.raw)
			}
			if decision.Action != SpeakGateActionWait {
				t.Fatalf("expected WAIT action, got %q", decision.Action)
			}
			normalized := decision.Normalized()
			if normalized.WaitMS != tc.expected {
				t.Fatalf("expected wait=%d, got %d", tc.expected, normalized.WaitMS)
			}
		})
	}
}
