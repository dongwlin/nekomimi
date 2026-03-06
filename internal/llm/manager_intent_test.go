package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
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

	decision, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent", "alice")
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

	_, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent-bad", "alice")
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

	_, err := manager.DecideImmersiveIntent(context.Background(), "hello", "session-intent-toolloop", "alice")
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
