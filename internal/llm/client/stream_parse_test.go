package client

import "testing"

func TestParseResponsesStreamEvent_Delta(t *testing.T) {
	delta, done, err := parseResponsesStreamEvent(`{"type":"response.output_text.delta","delta":"你好"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Fatalf("expected done=false")
	}
	if delta != "你好" {
		t.Fatalf("unexpected delta: %q", delta)
	}
}

func TestParseResponsesStreamEvent_Completed(t *testing.T) {
	delta, done, err := parseResponsesStreamEvent(`{"type":"response.completed"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Fatalf("expected done=true")
	}
	if delta != "" {
		t.Fatalf("expected empty delta, got %q", delta)
	}
}

func TestParseResponsesStreamEvent_Error(t *testing.T) {
	_, _, err := parseResponsesStreamEvent(`{"type":"response.error","error":{"message":"boom"}}`)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseResponsesStreamEvent_IgnoresNonOutputDeltaEvents(t *testing.T) {
	tests := []string{
		`{"type":"response.reasoning_text.delta","delta":"thinking"}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"summary"}`,
		`{"type":"response.function_call_arguments.delta","delta":"{\"k\":1}"}`,
		`{"type":"response.custom_tool_call_input.delta","delta":"{\"k\":1}"}`,
	}

	for _, input := range tests {
		delta, done, err := parseResponsesStreamEvent(input)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", input, err)
		}
		if done {
			t.Fatalf("expected done=false for %s", input)
		}
		if delta != "" {
			t.Fatalf("expected empty delta for %s, got %q", input, delta)
		}
	}
}

func TestParseResponsesStreamEvent_RefusalDelta(t *testing.T) {
	delta, done, err := parseResponsesStreamEvent(`{"type":"response.refusal.delta","delta":"nope"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Fatalf("expected done=false")
	}
	if delta != "nope" {
		t.Fatalf("unexpected delta: %q", delta)
	}
}

func TestParseChatCompletionsStreamEvent_Deltas(t *testing.T) {
	deltas, err := parseChatCompletionsStreamEvent(`{"choices":[{"delta":{"content":"你"}},{"delta":{"content":"好"}}]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d", len(deltas))
	}
	if deltas[0]+deltas[1] != "你好" {
		t.Fatalf("unexpected combined deltas: %q", deltas[0]+deltas[1])
	}
}

func TestParseChatCompletionsStreamEvent_Error(t *testing.T) {
	_, err := parseChatCompletionsStreamEvent(`{"error":{"message":"bad"}}`)
	if err == nil {
		t.Fatalf("expected error")
	}
}
