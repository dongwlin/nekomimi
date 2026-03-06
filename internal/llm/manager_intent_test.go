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
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
)

func TestDecideImmersiveIntent_ParsesValidIntent(t *testing.T) {
	server := newResponsesJSONServerForIntent(t, func(call int64, body map[string]any) string {
		return responsesOutputTextJSONForIntent(t, `{"action":"wait","wait_ms":120,"reason":"still typing"}`)
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
	server := newResponsesJSONServerForIntent(t, func(call int64, body map[string]any) string {
		return responsesOutputTextJSONForIntent(t, "hello")
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      server.URL + "/responses",
		Key:      "test-key",
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
	server := newResponsesJSONServerForIntent(t, func(call int64, body map[string]any) string {
		atomic.StoreInt64(&callCount, call)
		return responsesOutputTextJSONForIntent(t, `{"action":"reply"}`)
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
	}, ManagerDeps{})

	_, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent-toolloop", "alice", nil)
	if err != nil {
		t.Fatalf("decide immersive intent failed: %v", err)
	}
	if atomic.LoadInt64(&callCount) != 1 {
		t.Fatalf("expected one provider call for intent decision, got %d", atomic.LoadInt64(&callCount))
	}
}

func newResponsesJSONServerForIntent(t *testing.T, script func(call int64, body map[string]any) string) *httptest.Server {
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
		responseBody := script(call, body)
		if responseBody == "" {
			responseBody = responsesOutputTextJSONForIntent(t, `{"action":"skip","reason":"default"}`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}))
}

func responsesOutputTextJSONForIntent(t *testing.T, text string) string {
	t.Helper()
	textJSON, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("marshal intent text failed: %v", err)
	}
	return `{"output":[{"content":[{"type":"output_text","text":` + string(textJSON) + `}]}]}`
}

func TestDecideImmersiveIntent_ImmersiveContextReachesModel(t *testing.T) {
	var capturedInputs []string
	var mu sync.Mutex

	server := newResponsesJSONServerForIntent(t, func(call int64, body map[string]any) string {
		mu.Lock()
		capturedInputs = extractResponsesInputTexts(body)
		mu.Unlock()
		return responsesOutputTextJSONForIntent(t, `{"action":"reply","reason":"mentioned"}`)
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      server.URL + "/responses",
		Key:      "test-key",
	}, ManagerDeps{})

	if _, ok := manager.AppendUserEvent("session-intent-ic", "neko what time is it?", "alice"); !ok {
		t.Fatal("append user event failed")
	}

	ic := &contextassemble.ImmersiveContext{
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

	server := newResponsesJSONServerForIntent(t, func(call int64, body map[string]any) string {
		mu.Lock()
		capturedInputs = extractResponsesInputTexts(body)
		mu.Unlock()
		return responsesOutputTextJSONForIntent(t, `{"action":"skip","reason":"quiet"}`)
	})
	defer server.Close()

	manager := NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      server.URL + "/responses",
		Key:      "test-key",
	}, ManagerDeps{})

	_, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent-no-ic", "alice", nil)
	if err != nil {
		t.Fatalf("decide immersive intent failed: %v", err)
	}

	mu.Lock()
	allText := strings.Join(capturedInputs, "\n")
	mu.Unlock()

	for _, blockName := range []string{
		contextassemble.BlockImmersiveState,
		contextassemble.BlockImmersiveBatch,
		contextassemble.BlockImmersiveSignals,
	} {
		if strings.Contains(allText, blockName) {
			t.Fatalf("%s should not be present when ImmersiveContext is nil:\n%s", blockName, allText)
		}
	}
}

// extractResponsesInputTexts extracts all text content from a responses API request body.
func extractResponsesInputTexts(body map[string]any) []string {
	var texts []string
	inputList, ok := body["input"].([]any)
	if !ok {
		return texts
	}
	for _, item := range inputList {
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
	return texts
}
