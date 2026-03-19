package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/ctxasm"
	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
)

func TestDecideImmersiveIntent_ParsesValidIntent(t *testing.T) {
	server := newAnthropicJSONServerForIntent(t, func(call int64, body map[string]any) string {
		return anthropicTextResponseJSONForIntent(t, `{"action":"wait","wait_ms":120,"reason":"still typing"}`)
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

	decision, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent", "alice", nil)
	if err != nil {
		t.Fatalf("decide immersive intent failed: %v", err)
	}
	if decision.Action != llmintent.ActionWait {
		t.Fatalf("intent action mismatch: got %q, want %q", decision.Action, llmintent.ActionWait)
	}
	if decision.WaitMS != 120 {
		t.Fatalf("intent wait_ms mismatch: got %d, want %d", decision.WaitMS, 120)
	}
	if decision.Reason != "still typing" {
		t.Fatalf("intent reason mismatch: got %q", decision.Reason)
	}
}

func TestDecideImmersiveIntent_ProtocolError(t *testing.T) {
	server := newAnthropicJSONServerForIntent(t, func(call int64, body map[string]any) string {
		return anthropicTextResponseJSONForIntent(t, "hello")
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})

	_, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent-bad", "alice", nil)
	if err == nil {
		t.Fatal("expected protocol error, got nil")
	}
	if !errors.Is(err, llmintent.ErrProtocol) {
		t.Fatalf("expected protocol error, got %v", err)
	}
}

func TestDecideImmersiveIntent_DoesNotUseToolLoop(t *testing.T) {
	var callCount int64
	server := newAnthropicJSONServerForIntent(t, func(call int64, body map[string]any) string {
		atomic.StoreInt64(&callCount, call)
		return anthropicTextResponseJSONForIntent(t, `{"action":"reply"}`)
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

	_, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent-toolloop", "alice", nil)
	if err != nil {
		t.Fatalf("decide immersive intent failed: %v", err)
	}
	if atomic.LoadInt64(&callCount) != 1 {
		t.Fatalf("expected one model call for intent decision, got %d", atomic.LoadInt64(&callCount))
	}
}

func TestDecideImmersiveIntent_DisablesReasoningAndThinking(t *testing.T) {
	server := newAnthropicJSONServerForIntent(t, func(call int64, body map[string]any) string {
		if _, ok := body["reasoning"]; ok {
			t.Fatalf("immersive intent should disable reasoning, body=%+v", body)
		}
		if _, ok := body["thinking"]; ok {
			t.Fatalf("immersive intent should disable thinking, body=%+v", body)
		}
		return anthropicTextResponseJSONForIntent(t, `{"action":"reply"}`)
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
		Thinking: config.ThinkingConfig{
			Type: "adaptive",
		},
		OutputConfig: config.LLMOutputConfig{
			Effort: "high",
		},
	}, ManagerDeps{})

	if _, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent-fast", "alice", nil); err != nil {
		t.Fatalf("decide immersive intent failed: %v", err)
	}
}

func newAnthropicJSONServerForIntent(t *testing.T, script func(call int64, body map[string]any) string) *httptest.Server {
	t.Helper()
	var callCount int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAnthropicMessagesRequestPath(r.URL.Path) {
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
		responseBody := script(call, body)
		if responseBody == "" {
			responseBody = anthropicTextResponseJSONForIntent(t, `{"action":"skip","reason":"default"}`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}))
}

func anthropicTextResponseJSONForIntent(t *testing.T, text string) string {
	t.Helper()
	payload := map[string]any{
		"id":    "msg_test_intent",
		"type":  "message",
		"role":  "assistant",
		"model": "claude-test",
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  1,
			"output_tokens": 1,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal intent response failed: %v", err)
	}
	return string(data)
}

func TestDecideImmersiveIntent_ImmersiveContextReachesModel(t *testing.T) {
	var capturedInputs []string
	var mu sync.Mutex

	server := newAnthropicJSONServerForIntent(t, func(call int64, body map[string]any) string {
		mu.Lock()
		capturedInputs = extractAnthropicInputTexts(body)
		mu.Unlock()
		return anthropicTextResponseJSONForIntent(t, `{"action":"reply","reason":"mentioned"}`)
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})

	if _, ok := manager.AppendUserEvent("session-intent-ic", "neko what time is it?", "alice"); !ok {
		t.Fatal("append user event failed")
	}

	ic := &ctxasm.ImmersiveContext{
		MessagesCount:      4,
		Participants:       []string{"alice", "bob"},
		MentionsToBot:      2,
		AddressedToBot:     1,
		QuestionsCount:     1,
		LastSpeaker:        "alice",
		TimeSpanMS:         3000,
		ConversationMode:   "addressed",
		SignalScore:        9,
		SystemEventSummary: "[kind=poke_notice]: actor_name=alice direction=inbound",
	}

	decision, err := manager.DecideImmersiveIntent(context.Background(), "", "session-intent-ic", "alice", ic)
	if err != nil {
		t.Fatalf("decide immersive intent failed: %v", err)
	}
	if decision.Action != llmintent.ActionReply {
		t.Fatalf("intent action mismatch: got %q, want %q", decision.Action, llmintent.ActionReply)
	}

	mu.Lock()
	allText := strings.Join(capturedInputs, "\n")
	mu.Unlock()

	signals := []string{
		"[immersive_state]",
		"[immersive_batch]",
		"[immersive_signals]",
		"[immersive_events]",
		"mentions_to_bot: 2",
		"addressed_to_bot: 1",
		"questions_count: 1",
		"last_speaker: alice",
		"conversation_mode: addressed",
		"[kind=poke_notice]: actor_name=alice direction=inbound",
		"neko what time is it?",
	}
	for _, signal := range signals {
		if !strings.Contains(allText, signal) {
			t.Errorf("expected signal %q in model input, got:\n%s", signal, allText)
		}
	}
	if strings.Count(allText, "neko what time is it?") != 1 {
		t.Fatalf("expected current batch text once, got:\n%s", allText)
	}
}

func TestDecideImmersiveIntent_NilImmersiveContext_NoSignalsBlock(t *testing.T) {
	var capturedInputs []string
	var mu sync.Mutex

	server := newAnthropicJSONServerForIntent(t, func(call int64, body map[string]any) string {
		mu.Lock()
		capturedInputs = extractAnthropicInputTexts(body)
		mu.Unlock()
		return anthropicTextResponseJSONForIntent(t, `{"action":"skip","reason":"quiet"}`)
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "gpt-4.1-mini",
		API:     server.URL + "/responses",
		Key:     "test-key",
	}, ManagerDeps{})

	_, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent-no-ic", "alice", nil)
	if err != nil {
		t.Fatalf("decide immersive intent failed: %v", err)
	}

	mu.Lock()
	allText := strings.Join(capturedInputs, "\n")
	mu.Unlock()

	for _, blockName := range []string{
		ctxasm.BlockImmersiveState,
		ctxasm.BlockImmersiveBatch,
		ctxasm.BlockImmersiveSignals,
	} {
		if strings.Contains(allText, blockName) {
			t.Fatalf("%s should not be present when ImmersiveContext is nil:\n%s", blockName, allText)
		}
	}
}

func extractAnthropicInputTexts(body map[string]any) []string {
	var texts []string
	appendContentTexts := func(list []any) {
		for _, item := range list {
			msg, ok := item.(map[string]any)
			if !ok {
				continue
			}
			contentList, ok := msg["content"].([]any)
			if !ok {
				continue
			}
			for _, c := range contentList {
				cMap, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := cMap["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
	}

	if messageList, ok := body["messages"].([]any); ok {
		appendContentTexts(messageList)
	}
	if systemList, ok := body["system"].([]any); ok {
		for _, item := range systemList {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := block["text"].(string); ok {
				texts = append(texts, text)
			}
		}
	}
	return texts
}

func isAnthropicMessagesRequestPath(path string) bool {
	return strings.HasSuffix(strings.TrimSpace(path), "/v1/messages")
}
