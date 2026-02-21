package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestReplyStream_ToolsDisabled_UsesProviderStreaming(t *testing.T) {
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		return []string{
			mustResponsesDeltaEvent(t, "你"),
			mustResponsesDeltaEvent(t, "好"),
			`{"type":"response.completed"}`,
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      server.URL + "/responses",
		Key:      "test-key",
	})

	events := make([]StreamEvent, 0, 4)
	reply, err := manager.ReplyStream(context.Background(), "hello", "session-tools-off", "alice", func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("reply stream failed: %v", err)
	}
	if reply != "你好" {
		t.Fatalf("reply mismatch: got %q, want %q", reply, "你好")
	}
	if len(events) < 3 {
		t.Fatalf("event count should be >=3, got %d", len(events))
	}
	if events[0].Type != StreamEventDelta || events[1].Type != StreamEventDelta {
		t.Fatalf("first events should be delta: %+v", events)
	}
	if events[len(events)-1].Type != StreamEventFinal {
		t.Fatalf("last event should be final: %+v", events[len(events)-1])
	}
}

func TestReplyStreamWithExtraPrompt_DoesNotAppendHistory(t *testing.T) {
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		return []string{
			mustResponsesDeltaEvent(t, "ok"),
			`{"type":"response.completed"}`,
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      server.URL + "/responses",
		Key:      "test-key",
	})

	_, err := manager.ReplyStreamWithExtraPrompt(context.Background(), "hello", "session-extra", "alice", "extra", func(event StreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("reply stream with extra prompt failed: %v", err)
	}

	usage := manager.SessionContextUsage("session-extra")
	if usage.RecentChatCount != 0 {
		t.Fatalf("recent chat should remain empty for extra prompt stream, got %d", usage.RecentChatCount)
	}
}

func TestReplyStream_ToolsEnabled_EmitsToolEvents(t *testing.T) {
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		switch call {
		case 1:
			return []string{
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"delta","delta":{"text":"thinking"}}`+"\n"),
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"tool_call","tool_call":{"call_id":"c1","name":"internal/read_diary","arguments":{"session_key":"session-tools-on","limit":1}}}`+"\n"),
				`{"type":"response.completed"}`,
			}
		case 2:
			return []string{
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"delta","delta":{"text":"done"}}`+"\n"),
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"final","final":{"content":"answer","stop_reason":"final"}}`+"\n"),
				`{"type":"response.completed"}`,
			}
		default:
			return []string{
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"error","error":{"code":"internal_error","message":"unexpected call","retryable":false}}`+"\n"),
				`{"type":"response.completed"}`,
			}
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      server.URL + "/responses",
		Key:      "test-key",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	})

	events := make([]StreamEvent, 0, 8)
	reply, err := manager.ReplyStream(context.Background(), "hello", "session-tools-on", "alice", func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("reply stream failed: %v", err)
	}
	if reply != "answer" {
		t.Fatalf("reply mismatch: got %q, want %q", reply, "answer")
	}
	if len(events) < 5 {
		t.Fatalf("event count should be >=5, got %d", len(events))
	}

	types := make([]StreamEventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	expectedPrefix := []StreamEventType{
		StreamEventDelta,
		StreamEventToolCall,
		StreamEventToolResult,
		StreamEventDelta,
		StreamEventFinal,
	}
	for i, want := range expectedPrefix {
		if i >= len(types) {
			t.Fatalf("missing expected event at index %d: want %q", i, want)
		}
		if types[i] != want {
			t.Fatalf("event type mismatch at index %d: got %q, want %q", i, types[i], want)
		}
	}
}

func newResponsesStreamServer(t *testing.T, script func(call int64, body map[string]any) []string) *httptest.Server {
	t.Helper()
	var callCount int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		call := atomic.AddInt64(&callCount, 1)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, event := range script(call, body) {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
}

func mustResponsesDeltaEvent(t *testing.T, text string) string {
	t.Helper()
	payload := map[string]any{
		"type":  "response.output_text.delta",
		"delta": text,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal delta event failed: %v", err)
	}
	return string(data)
}
