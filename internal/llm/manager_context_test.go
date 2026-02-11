package llm

import (
	"testing"
	"time"

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

	usage := m.SessionContextUsage(sessionKey)
	expectedUsed := token.EstimateContextTokens(m.systemPrompt, m.historySnapshot(sessionKey))
	expectedPercent := float64(expectedUsed) * 100 / 1000

	if usage.UsedTokens != expectedUsed {
		t.Fatalf("used tokens mismatch: got %d, want %d", usage.UsedTokens, expectedUsed)
	}
	if usage.MaxTokens != 1000 {
		t.Fatalf("max tokens mismatch: got %d, want %d", usage.MaxTokens, 1000)
	}
	if usage.UsagePercent != expectedPercent {
		t.Fatalf("usage percent mismatch: got %f, want %f", usage.UsagePercent, expectedPercent)
	}
	if usage.MessageCount != 2 {
		t.Fatalf("message count mismatch: got %d, want %d", usage.MessageCount, 2)
	}
	if usage.SessionStartedAt.IsZero() {
		t.Fatalf("session start time should be set")
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

	usage := m.SessionContextUsage(sessionKey)
	if usage.MaxTokens != 0 {
		t.Fatalf("max tokens mismatch: got %d, want %d", usage.MaxTokens, 0)
	}
	if usage.UsagePercent != 0 {
		t.Fatalf("usage percent mismatch: got %f, want %f", usage.UsagePercent, 0.0)
	}
}

func TestSessionContextUsage_CompressCountersAndClear(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model:      "gpt-4.1-mini",
		ContextMax: 1000,
	})
	sessionKey := "group:777"
	m.appendHistory(sessionKey, "u", "a")
	time.Sleep(1 * time.Millisecond)
	m.incrementHistoryCompressCount(sessionKey)
	m.incrementContextCompressCount(sessionKey)
	m.incrementContextCompressCount(sessionKey)

	usage := m.SessionContextUsage(sessionKey)
	if usage.HistoryCompressCount != 1 {
		t.Fatalf("history compress count mismatch: got %d, want %d", usage.HistoryCompressCount, 1)
	}
	if usage.ContextCompressCount != 2 {
		t.Fatalf("context compress count mismatch: got %d, want %d", usage.ContextCompressCount, 2)
	}
	if usage.TotalCompressCount != 3 {
		t.Fatalf("total compress count mismatch: got %d, want %d", usage.TotalCompressCount, 3)
	}
	if usage.SessionStartedAt.IsZero() {
		t.Fatalf("session start time should be set")
	}

	m.ClearHistory(sessionKey)
	usage = m.SessionContextUsage(sessionKey)
	if !usage.SessionStartedAt.IsZero() {
		t.Fatalf("session start time should be cleared")
	}
	if usage.TotalCompressCount != 0 {
		t.Fatalf("total compress count should be reset: got %d", usage.TotalCompressCount)
	}
}
