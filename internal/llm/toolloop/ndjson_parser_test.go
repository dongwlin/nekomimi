package toolloop

import (
	"encoding/json"
	"testing"
)

func TestNDJSONParser_CrossChunkFrames(t *testing.T) {
	parser := NewNDJSONParser()

	items, err := parser.Feed(`{"version":"v2","type":"delta","delta":{"text":"he`)
	if err != nil {
		t.Fatalf("feed chunk1 failed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("chunk1 should not produce complete frame, got %d", len(items))
	}

	items, err = parser.Feed(`llo"}}` + "\n" + `{"version":"v2","type":"final","final":{"content":"hello","stop_reason":"final"}}` + "\n")
	if err != nil {
		t.Fatalf("feed chunk2 failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("chunk2 item count mismatch: got %d, want 2", len(items))
	}
	if items[0].Frame == nil || items[0].Frame.Type != MessageTypeDelta {
		t.Fatalf("first item should be delta frame")
	}
	if items[0].Frame.Delta == nil || items[0].Frame.Delta.Text != "hello" {
		t.Fatalf("unexpected first delta text: %+v", items[0].Frame.Delta)
	}
	if items[1].Frame == nil || items[1].Frame.Type != MessageTypeFinal {
		t.Fatalf("second item should be final frame")
	}
}

func TestNDJSONParser_MultiLineOrderAndWhitespace(t *testing.T) {
	parser := NewNDJSONParser()
	input := "\n \n" +
		`{"version":"v2","type":"delta","delta":{"text":"a"}}` + "\n" +
		`{"version":"v2","type":"delta","delta":{"text":"b"}}` + "\n"

	items, err := parser.Feed(input)
	if err != nil {
		t.Fatalf("feed failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("item count mismatch: got %d, want 2", len(items))
	}
	if items[0].Frame == nil || items[0].Frame.Delta == nil || items[0].Frame.Delta.Text != "a" {
		t.Fatalf("first frame mismatch: %+v", items[0].Frame)
	}
	if items[1].Frame == nil || items[1].Frame.Delta == nil || items[1].Frame.Delta.Text != "b" {
		t.Fatalf("second frame mismatch: %+v", items[1].Frame)
	}
}

func TestNDJSONParser_NonJSONFallbackDelta(t *testing.T) {
	parser := NewNDJSONParser()

	items, err := parser.Feed("plain text line\n")
	if err != nil {
		t.Fatalf("feed failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("item count mismatch: got %d, want 1", len(items))
	}
	if items[0].Frame != nil {
		t.Fatalf("non-json line should not parse as frame")
	}
	if items[0].Text != "plain text line" {
		t.Fatalf("fallback text mismatch: got %q", items[0].Text)
	}
}

func TestNDJSONParser_InvalidJSONFrame_ReturnsTextFallback(t *testing.T) {
	parser := NewNDJSONParser()

	items, err := parser.Feed(`{"version":"v2","type":"delta","delta":{"text":"x"` + "\n")
	if err != nil {
		t.Fatalf("feed failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("item count mismatch: got %d, want 1", len(items))
	}
	if items[0].Frame != nil {
		t.Fatalf("invalid json should fallback to text")
	}
	if items[0].Text == "" {
		t.Fatalf("fallback text should not be empty")
	}
}

func TestNDJSONParser_EmptyFlush(t *testing.T) {
	parser := NewNDJSONParser()

	items, err := parser.Flush()
	if err != nil {
		t.Fatalf("flush failed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty flush should produce no items, got %d", len(items))
	}
}

func TestNDJSONParser_InvalidStructuredFrame(t *testing.T) {
	parser := NewNDJSONParser()

	_, err := parser.Feed(`{"version":"v2","type":"final"}` + "\n")
	if err == nil {
		t.Fatal("expected invalid structured frame error, got nil")
	}
}

func TestNDJSONParser_NormalizesLegacyToolCallFrame(t *testing.T) {
	parser := NewNDJSONParser()

	items, err := parser.Feed(`{"version":"v1","type":"tool_call","tool_call":{"name":"internal/read_diary","arguments":"{\"session_key\":\"group:1\"}"}}` + "\n")
	if err != nil {
		t.Fatalf("feed failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("item count mismatch: got %d, want 1", len(items))
	}
	frame := items[0].Frame
	if frame == nil {
		t.Fatal("expected parsed frame")
	}
	if frame.Version != StreamProtocolVersion {
		t.Fatalf("version mismatch: got %q, want %q", frame.Version, StreamProtocolVersion)
	}
	if frame.Type != MessageTypeToolCall || frame.ToolCall == nil {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if frame.ToolCall.CallID == "" {
		t.Fatal("call_id should be auto-filled when model omits it")
	}
	var args map[string]any
	if err := json.Unmarshal(frame.ToolCall.Arguments, &args); err != nil {
		t.Fatalf("arguments should be object: %v", err)
	}
	if got := args["session_key"]; got != "group:1" {
		t.Fatalf("session_key mismatch: got %#v, want %q", got, "group:1")
	}
}
