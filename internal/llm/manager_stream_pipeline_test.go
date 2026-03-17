package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/contextassemble"
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
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})

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

func TestReplyStream_ToolsDisabled_PreservesWhitespaceOnlyDelta(t *testing.T) {
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		return []string{
			mustResponsesDeltaEvent(t, "REPLY"),
			mustResponsesDeltaEvent(t, "\n"),
			mustResponsesDeltaEvent(t, "ok"),
			`{"type":"response.completed"}`,
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})

	deltas := make([]string, 0, 4)
	reply, err := manager.ReplyStream(context.Background(), "hello", "session-tools-off-whitespace", "alice", func(event StreamEvent) error {
		if event.Type == StreamEventDelta {
			deltas = append(deltas, event.Delta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reply stream failed: %v", err)
	}

	const want = "REPLY\nok"
	if reply != want {
		t.Fatalf("reply mismatch: got %q, want %q", reply, want)
	}
	if got := strings.Join(deltas, ""); got != want {
		t.Fatalf("delta stream mismatch: got %q, want %q", got, want)
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
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})

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

func TestReplyStreamWithExtraPromptAllowTools_ImmersiveContextUsesSingleBlockLayout(t *testing.T) {
	var capturedInputs []string
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		capturedInputs = extractResponsesInputTexts(body)
		return []string{
			mustResponsesDeltaEvent(t, "ok"),
			`{"type":"response.completed"}`,
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})

	if _, ok := manager.AppendUserEvent("session-immersive-reply-layout", "neko current batch", "alice"); !ok {
		t.Fatal("append user event failed")
	}

	ic := &contextassemble.ImmersiveContext{
		MessagesCount:    1,
		Participants:     []string{"alice"},
		MentionsToBot:    1,
		AddressedToBot:   1,
		QuestionsCount:   1,
		LastSpeaker:      "alice",
		ConversationMode: "addressed",
		SignalScore:      8,
	}

	reply, err := manager.ReplyStreamWithExtraPromptAllowTools(context.Background(), "", "session-immersive-reply-layout", "", "extra", func(event StreamEvent) error {
		return nil
	}, ic)
	if err != nil {
		t.Fatalf("reply stream with immersive context failed: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply mismatch: got %q, want %q", reply, "ok")
	}

	allText := strings.Join(capturedInputs, "\n")
	for _, blockName := range []string{
		contextassemble.BlockImmersiveState,
		contextassemble.BlockImmersiveBatch,
		contextassemble.BlockImmersiveSignals,
	} {
		if !strings.Contains(allText, "["+blockName+"]") {
			t.Fatalf("%s block missing from reply prompt:\n%s", blockName, allText)
		}
	}
	if strings.Count(allText, "neko current batch") != 1 {
		t.Fatalf("expected current batch text once in reply prompt, got:\n%s", allText)
	}
}

func TestReplyStreamWithExtraPromptAllowTools_DisablesReasoningAndThinking(t *testing.T) {
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		if _, ok := body["reasoning"]; ok {
			t.Fatalf("immersive reply should disable reasoning, body=%+v", body)
		}
		if _, ok := body["thinking"]; ok {
			t.Fatalf("immersive reply should disable thinking, body=%+v", body)
		}
		return []string{
			mustResponsesDeltaEvent(t, `{"version":"v2","type":"final","final":{"content":"ok","stop_reason":"final"}}`+"\n"),
			`{"type":"response.completed"}`,
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled:         true,
		Model:           "gpt-4.1-mini",
		API:             server.URL + "/responses",
		Key:             "test-key",
		ReasoningEffort: "medium",
		ThinkingType:    "enabled",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}, ManagerDeps{})

	reply, err := manager.ReplyStreamWithExtraPromptAllowTools(context.Background(), "hello", "session-extra-no-reasoning", "alice", "extra", func(event StreamEvent) error {
		return nil
	}, &contextassemble.ImmersiveContext{MaxReplySegments: 2})
	if err != nil {
		t.Fatalf("immersive reply stream failed: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply mismatch: got %q, want %q", reply, "ok")
	}
}

func TestReplyStreamWithExtraPrompt_DisablesToolLoop(t *testing.T) {
	var observedCall int64
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		atomic.StoreInt64(&observedCall, call)
		switch call {
		case 1:
			return []string{
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"tool_call","tool_call":{"call_id":"c1","name":"internal/read_diary","arguments":{"session_key":"session-extra-tools-off","limit":1}}}`+"\n"),
				`{"type":"response.completed"}`,
			}
		default:
			return []string{
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"final","final":{"content":"done","stop_reason":"final"}}`+"\n"),
				`{"type":"response.completed"}`,
			}
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}, ManagerDeps{})

	reply, err := manager.ReplyStreamWithExtraPrompt(context.Background(), "hello", "session-extra-tools-off", "alice", "extra", func(event StreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("reply stream with extra prompt failed: %v", err)
	}
	if atomic.LoadInt64(&observedCall) != 1 {
		t.Fatalf("expected exactly one provider call, got %d", atomic.LoadInt64(&observedCall))
	}
	if !strings.Contains(reply, `"type":"tool_call"`) {
		t.Fatalf("expected raw streamed content without tool-loop handling, got %q", reply)
	}
}

func TestReplyStreamWithExtraPromptAllowTools_UsesToolLoop(t *testing.T) {
	var observedCall int64
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		atomic.StoreInt64(&observedCall, call)
		switch call {
		case 1:
			return []string{
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"delta","delta":{"text":"thinking"}}`+"\n"),
				mustResponsesDeltaEvent(t, `{"version":"v2","type":"tool_call","tool_call":{"call_id":"c1","name":"internal/read_diary","arguments":{"session_key":"session-extra-tools-on","limit":1}}}`+"\n"),
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
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}, ManagerDeps{})

	events := make([]StreamEvent, 0, 8)
	reply, err := manager.ReplyStreamWithExtraPromptAllowTools(context.Background(), "hello", "session-extra-tools-on", "alice", "extra", func(event StreamEvent) error {
		events = append(events, event)
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("reply stream with extra prompt allow tools failed: %v", err)
	}
	if reply != "answer" {
		t.Fatalf("reply mismatch: got %q, want %q", reply, "answer")
	}
	if atomic.LoadInt64(&observedCall) != 2 {
		t.Fatalf("expected two provider calls, got %d", atomic.LoadInt64(&observedCall))
	}

	seenToolCall := false
	seenToolResult := false
	seenFinal := false
	for _, event := range events {
		switch event.Type {
		case StreamEventToolCall:
			seenToolCall = true
		case StreamEventToolResult:
			seenToolResult = true
		case StreamEventFinal:
			seenFinal = true
		}
	}
	if !seenToolCall {
		t.Fatal("expected tool_call event")
	}
	if !seenToolResult {
		t.Fatal("expected tool_result event")
	}
	if !seenFinal {
		t.Fatal("expected final event")
	}
}

func TestReplyStreamWithExtraPrompt_PreservesWhitespaceOnlyDelta(t *testing.T) {
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		return []string{
			mustResponsesDeltaEvent(t, "REPLY"),
			mustResponsesDeltaEvent(t, "\n"),
			mustResponsesDeltaEvent(t, "ok"),
			`{"type":"response.completed"}`,
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})

	deltas := make([]string, 0, 4)
	reply, err := manager.ReplyStreamWithExtraPrompt(context.Background(), "hello", "session-extra-whitespace", "alice", "extra", func(event StreamEvent) error {
		if event.Type == StreamEventDelta {
			deltas = append(deltas, event.Delta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reply stream with extra prompt failed: %v", err)
	}

	const want = "REPLY\nok"
	if reply != want {
		t.Fatalf("reply mismatch: got %q, want %q", reply, want)
	}
	if got := strings.Join(deltas, ""); got != want {
		t.Fatalf("delta stream mismatch: got %q, want %q", got, want)
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
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}, ManagerDeps{})

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

func TestReplyStream_ToolsEnabled_ProtocolErrorRetriesThenRecovers(t *testing.T) {
	var observedCall int64
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		atomic.StoreInt64(&observedCall, call)
		switch call {
		case 1:
			return []string{
				mustResponsesDeltaEvent(t, `{"final":{"content":"bad","stop_reason":"final"}}`+"\n"),
				`{"type":"response.completed"}`,
			}
		case 2:
			return []string{
				mustResponsesDeltaEvent(t, `{"type":"final","final":{"content":"answer","stop_reason":"final"}}`+"\n"),
				`{"type":"response.completed"}`,
			}
		default:
			return []string{
				mustResponsesDeltaEvent(t, `{"type":"error","error":{"code":"internal_error","message":"unexpected call","retryable":false}}`+"\n"),
				`{"type":"response.completed"}`,
			}
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}, ManagerDeps{})

	reply, err := manager.ReplyStream(context.Background(), "hello", "session-tools-plaintext", "alice", func(event StreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("reply stream failed: %v", err)
	}
	if atomic.LoadInt64(&observedCall) != 2 {
		t.Fatalf("expected two provider calls, got %d", atomic.LoadInt64(&observedCall))
	}
	if reply != "answer" {
		t.Fatalf("reply mismatch: got %q, want %q", reply, "answer")
	}
}

func TestReplyStream_ToolsEnabled_ProtocolErrorStopsAfterThreeAttempts(t *testing.T) {
	var observedCall int64
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		atomic.StoreInt64(&observedCall, call)
		return []string{
			mustResponsesDeltaEvent(t, `{"final":{"content":"bad","stop_reason":"final"}}`+"\n"),
			`{"type":"response.completed"}`,
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}, ManagerDeps{})

	_, err := manager.ReplyStream(context.Background(), "hello", "session-tools-protocol-error", "alice", func(event StreamEvent) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected reply stream to fail after protocol retry limit")
	}
	if atomic.LoadInt64(&observedCall) != 3 {
		t.Fatalf("expected three provider calls, got %d", atomic.LoadInt64(&observedCall))
	}
	if !strings.Contains(err.Error(), "protocol retry limit exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReplyStream_AppendsAtomicEventsWithAnchor(t *testing.T) {
	server := newResponsesStreamServer(t, func(call int64, body map[string]any) []string {
		return []string{
			mustResponsesDeltaEvent(t, "ok"),
			`{"type":"response.completed"}`,
		}
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})
	sessionKey := "session-stream-atomic"
	_, err := manager.ReplyStream(context.Background(), "hello-stream", sessionKey, "name=alice", func(event StreamEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("reply stream failed: %v", err)
	}

	result, err := manager.ListChatEvents(sessionKey, chatlog.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list chat events failed: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("event count mismatch: got %d, want %d", len(result.Entries), 2)
	}

	assistant := result.Entries[0]
	user := result.Entries[1]
	if assistant.Role != chatlog.RoleAssistant || user.Role != chatlog.RoleUser {
		t.Fatalf("unexpected event roles: assistant=%q user=%q", assistant.Role, user.Role)
	}
	if strings.Contains(user.Content, "batch_meta:") || strings.Contains(user.Content, "transcript:") {
		t.Fatalf("user content should be atomic message, got %q", user.Content)
	}
	if strings.TrimSpace(user.Metadata["event_time"]) == "" || strings.TrimSpace(assistant.Metadata["event_time"]) == "" {
		t.Fatalf("event_time metadata should exist on both events")
	}

	userSeq, err := strconv.ParseInt(user.Metadata["causal_seq"], 10, 64)
	if err != nil || userSeq <= 0 {
		t.Fatalf("invalid user causal_seq: %q", user.Metadata["causal_seq"])
	}
	assistantSeq, err := strconv.ParseInt(assistant.Metadata["causal_seq"], 10, 64)
	if err != nil || assistantSeq <= userSeq {
		t.Fatalf("assistant causal_seq should be > user causal_seq, got user=%d assistant=%q", userSeq, assistant.Metadata["causal_seq"])
	}
	if assistant.Metadata["reply_to_cutoff_seq"] != strconv.FormatInt(userSeq, 10) {
		t.Fatalf("reply_to_cutoff_seq mismatch: got %q, want %d", assistant.Metadata["reply_to_cutoff_seq"], userSeq)
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
