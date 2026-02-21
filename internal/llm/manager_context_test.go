package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
	"github.com/dongwlin/nekomimi/internal/llm/token"
)

func TestSessionContextUsage_WithLimit(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model:        "gpt-4.1-mini",
		SystemPrompt: "test prompt",
		ContextMax:   1000,
		ContextAssembly: config.ContextAssemblyConfig{
			RecentChatLimit:  50,
			RecentDiaryLimit: 50,
		},
	})
	sessionKey := "group:123"
	m.appendHistory(sessionKey, "hello", "world")
	if _, err := m.diaryStore.Write(context.Background(), sessionKey, diary.Entry{
		Author:  "tester",
		Content: "diary entry",
	}); err != nil {
		t.Fatalf("write diary failed: %v", err)
	}

	usage := m.SessionContextUsage(sessionKey)
	assembled, err := m.contextAssembler.Assemble(context.Background(), contextassemble.Request{
		SessionKey: sessionKey,
	})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	expectedUsed := token.EstimateContextTokens(m.systemPrompt, []Message{
		{
			Role:    "user",
			Content: renderUsageAssembledBlocks(assembled.Blocks),
		},
	})
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
	if usage.RecentChatCount != 2 {
		t.Fatalf("recent chat count mismatch: got %d, want %d", usage.RecentChatCount, 2)
	}
	if usage.RecentChatLimit != 50 {
		t.Fatalf("recent chat limit mismatch: got %d, want %d", usage.RecentChatLimit, 50)
	}
	if usage.RecentDiaryCount != 1 {
		t.Fatalf("recent diary count mismatch: got %d, want %d", usage.RecentDiaryCount, 1)
	}
	if usage.RecentDiaryLimit != 50 {
		t.Fatalf("recent diary limit mismatch: got %d, want %d", usage.RecentDiaryLimit, 50)
	}
	if usage.AssembledChars != assembled.TotalChars {
		t.Fatalf("assembled chars mismatch: got %d, want %d", usage.AssembledChars, assembled.TotalChars)
	}
	if usage.TruncatedBlockCount != 0 {
		t.Fatalf("truncated block count mismatch: got %d, want 0", usage.TruncatedBlockCount)
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
	if usage.RecentChatCount != 2 {
		t.Fatalf("recent chat count mismatch: got %d, want %d", usage.RecentChatCount, 2)
	}
}

func TestSessionContextUsage_ContextTrimCountAndClear(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model:      "gpt-4.1-mini",
		ContextMax: 1000,
	})
	sessionKey := "group:777"
	m.appendHistory(sessionKey, "u", "a")
	time.Sleep(1 * time.Millisecond)
	m.incrementContextTrimCount(sessionKey)
	m.incrementContextTrimCount(sessionKey)

	usage := m.SessionContextUsage(sessionKey)
	if usage.ContextTrimCount != 2 {
		t.Fatalf("context trim count mismatch: got %d, want %d", usage.ContextTrimCount, 2)
	}
	if usage.SessionStartedAt.IsZero() {
		t.Fatalf("session start time should be set")
	}

	m.ClearHistory(sessionKey)
	usage = m.SessionContextUsage(sessionKey)
	if !usage.SessionStartedAt.IsZero() {
		t.Fatalf("session start time should be cleared")
	}
	if usage.ContextTrimCount != 0 {
		t.Fatalf("context trim count should be reset: got %d", usage.ContextTrimCount)
	}
}

func TestSessionContextUsage_TruncatedBlocks(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model:      "gpt-4.1-mini",
		ContextMax: 20,
	})
	sessionKey := "group:trim"
	m.appendHistory(sessionKey, strings.Repeat("a", 40), strings.Repeat("b", 40))

	usage := m.SessionContextUsage(sessionKey)
	if usage.TruncatedBlockCount == 0 {
		t.Fatalf("expected truncated blocks, got 0")
	}
	if usage.AssembledChars > 20 {
		t.Fatalf("assembled chars should respect context_max: got %d", usage.AssembledChars)
	}
}
