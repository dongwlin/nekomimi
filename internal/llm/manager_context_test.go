package llm

import (
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm/token"
)

func TestSessionContextUsage_WithLimit(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model:        "gpt-4.1-mini",
		SystemPrompt: "test prompt",
		ContextMax:   1000,
	})
	sessionKey := "group:123"
	m.appendHistory(sessionKey, "你好", "世界")

	used, max, percent, messageCount := m.SessionContextUsage(sessionKey)
	expectedUsed := token.EstimateContextTokens(m.systemPrompt, m.historySnapshot(sessionKey))
	expectedPercent := float64(expectedUsed) * 100 / 1000

	if used != expectedUsed {
		t.Fatalf("used tokens mismatch: got %d, want %d", used, expectedUsed)
	}
	if max != 1000 {
		t.Fatalf("max tokens mismatch: got %d, want %d", max, 1000)
	}
	if percent != expectedPercent {
		t.Fatalf("usage percent mismatch: got %f, want %f", percent, expectedPercent)
	}
	if messageCount != 2 {
		t.Fatalf("message count mismatch: got %d, want %d", messageCount, 2)
	}
}

func TestSessionContextUsage_WithoutLimit(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model:        "gpt-4.1-mini",
		SystemPrompt: "test prompt",
		ContextMax:   0,
	})
	sessionKey := "private:1"
	m.appendHistory(sessionKey, "hi", "hello")

	_, max, percent, _ := m.SessionContextUsage(sessionKey)
	if max != 0 {
		t.Fatalf("max tokens mismatch: got %d, want %d", max, 0)
	}
	if percent != 0 {
		t.Fatalf("usage percent mismatch: got %f, want %f", percent, 0.0)
	}
}
