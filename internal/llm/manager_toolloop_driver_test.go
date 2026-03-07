package llm

import (
	"encoding/json"
	"testing"

	"github.com/dongwlin/nekomimi/internal/llm/toolloop"
)

func TestParseToolLoopFrame_NormalizesLegacyToolCall(t *testing.T) {
	frame, err := parseToolLoopFrame(`{"version":"v2","type":"tool_call","tool_call":{"name":"internal/read_diary","arguments":"{\"session_key\":\"group:1\",\"limit\":1}"}}`)
	if err != nil {
		t.Fatalf("expected tool-loop frame to parse: %v", err)
	}
	if frame.Version != "" {
		t.Fatalf("version should be cleared, got %q", frame.Version)
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
	frame, err := parseToolLoopFrame(`{"type":"final","final":{"content":"ok"}}`)
	if err != nil {
		t.Fatalf("expected final frame to parse: %v", err)
	}
	if frame.Version != "" {
		t.Fatalf("version should be cleared, got %q", frame.Version)
	}
	if frame.Final == nil {
		t.Fatal("final payload should exist")
	}
	if frame.Final.StopReason != toolloop.StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", frame.Final.StopReason, toolloop.StopReasonFinal)
	}
}

func TestParseToolLoopFrame_RejectsOrdinaryJSON(t *testing.T) {
	if _, err := parseToolLoopFrame(`{"type":"final","content":"plain json"}`); err == nil {
		t.Fatal("ordinary JSON should be rejected as invalid protocol")
	}
}

func TestParseToolLoopFrame_VersionedInvalidProtocolReturnsError(t *testing.T) {
	if _, err := parseToolLoopFrame(`{"version":"v1","type":"final"}`); err == nil {
		t.Fatal("expected invalid protocol error")
	}
}
