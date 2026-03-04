package llm

import (
	"encoding/json"
	"testing"

	"github.com/dongwlin/nekomimi/internal/llm/toolloop"
)

func TestParseToolLoopFrame_NormalizesLegacyToolCall(t *testing.T) {
	frame, ok := parseToolLoopFrame(`{"version":"v2","type":"tool_call","tool_call":{"name":"internal/read_diary","arguments":"{\"session_key\":\"group:1\",\"limit\":1}"}}`)
	if !ok {
		t.Fatal("expected tool-loop frame to parse")
	}
	if frame.Version != toolloop.ProtocolVersion {
		t.Fatalf("version mismatch: got %q, want %q", frame.Version, toolloop.ProtocolVersion)
	}
	if frame.Type != toolloop.MessageTypeToolCall || frame.ToolCall == nil {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if frame.ToolCall.CallID == "" {
		t.Fatal("call_id should be auto-filled when model omits it")
	}

	var arguments map[string]any
	if err := json.Unmarshal(frame.ToolCall.Arguments, &arguments); err != nil {
		t.Fatalf("arguments should be a JSON object, got %q (%v)", string(frame.ToolCall.Arguments), err)
	}
	if got := arguments["session_key"]; got != "group:1" {
		t.Fatalf("session_key mismatch: got %#v, want %q", got, "group:1")
	}
}

func TestParseToolLoopFrame_DefaultsFinalStopReason(t *testing.T) {
	frame, ok := parseToolLoopFrame(`{"type":"final","final":{"content":"ok"}}`)
	if !ok {
		t.Fatal("expected final frame to parse")
	}
	if frame.Version != toolloop.ProtocolVersion {
		t.Fatalf("version mismatch: got %q, want %q", frame.Version, toolloop.ProtocolVersion)
	}
	if frame.Final == nil {
		t.Fatal("final payload should exist")
	}
	if frame.Final.StopReason != toolloop.StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", frame.Final.StopReason, toolloop.StopReasonFinal)
	}
}
