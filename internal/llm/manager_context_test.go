package llm

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/contextassemble"
	"github.com/dongwlin/nekomimi/internal/diary"
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
	}, ManagerDeps{})
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
	expectedUsed := token.EstimateContextTokens(m.current.systemPrompt, []Message{
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
	}, ManagerDeps{})
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
	}, ManagerDeps{})
	sessionKey := "group:777"
	m.appendHistory(sessionKey, "u", "a")
	if _, err := m.diaryStore.Write(context.Background(), sessionKey, diary.Entry{
		Author:  "assistant",
		Content: "note",
	}); err != nil {
		t.Fatalf("write diary failed: %v", err)
	}
	time.Sleep(1 * time.Millisecond)
	m.sessions.incrementContextTrimCount(sessionKey)
	m.sessions.incrementContextTrimCount(sessionKey)

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
	if usage.RecentDiaryCount != 0 {
		t.Fatalf("recent diary count should be reset: got %d", usage.RecentDiaryCount)
	}
}

func TestSessionContextUsage_TruncatedBlocks(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model:      "gpt-4.1-mini",
		ContextMax: 20,
	}, ManagerDeps{})
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

func TestAppendHistory_AssistantEntryHasIdentityLabel(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model: "gpt-4.1-mini",
	}, ManagerDeps{})
	m.SetAssistantSpeaker("name=nekomimi;id=10000")
	sessionKey := "group:assistant-speaker"
	m.appendHistory(sessionKey, "hello", "world")

	assembled, err := m.contextAssembler.Assemble(context.Background(), contextassemble.Request{
		SessionKey: sessionKey,
	})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	recentChat, ok := assembled.Block(contextassemble.BlockRecentChat)
	if !ok {
		t.Fatalf("missing %s block", contextassemble.BlockRecentChat)
	}
	if !strings.Contains(recentChat.Content, "[role=assistant] [name=nekomimi;id=10000;time=") {
		t.Fatalf("assistant identity label missing in recent_chat: %q", recentChat.Content)
	}
	if !strings.Contains(recentChat.Content, "]: world") {
		t.Fatalf("assistant content formatting mismatch: %q", recentChat.Content)
	}
}

func TestAppendEvents_MetadataAndReplyAnchor(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model: "gpt-4.1-mini",
	}, ManagerDeps{})
	sessionKey := "group:event-metadata"
	userAt := time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC)
	replyAt := userAt.Add(2 * time.Second)

	cutoffSeq, ok := m.AppendUserEventAt(sessionKey, "hello", "name=alice", userAt)
	if !ok {
		t.Fatal("append user event failed")
	}
	if cutoffSeq <= 0 {
		t.Fatalf("invalid cutoff seq: %d", cutoffSeq)
	}
	if !m.AppendAssistantEventAt(sessionKey, "world", cutoffSeq, replyAt) {
		t.Fatal("append assistant event failed")
	}

	result, err := m.ListChatEvents(sessionKey, chatlog.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list chat events failed: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("event count mismatch: got %d, want %d", len(result.Entries), 2)
	}

	assistant := result.Entries[0]
	user := result.Entries[1]
	if user.Role != chatlog.RoleUser {
		t.Fatalf("unexpected user role: %q", user.Role)
	}
	if assistant.Role != chatlog.RoleAssistant {
		t.Fatalf("unexpected assistant role: %q", assistant.Role)
	}

	userSeq, err := strconv.ParseInt(user.Metadata["causal_seq"], 10, 64)
	if err != nil || userSeq <= 0 {
		t.Fatalf("invalid user causal_seq: %q", user.Metadata["causal_seq"])
	}
	assistantSeq, err := strconv.ParseInt(assistant.Metadata["causal_seq"], 10, 64)
	if err != nil || assistantSeq <= userSeq {
		t.Fatalf("invalid assistant causal_seq: user=%d assistant=%q", userSeq, assistant.Metadata["causal_seq"])
	}
	if strings.TrimSpace(user.Metadata["event_time"]) == "" {
		t.Fatalf("missing user event_time metadata")
	}
	if strings.TrimSpace(assistant.Metadata["event_time"]) == "" {
		t.Fatalf("missing assistant event_time metadata")
	}
	if assistant.Metadata["reply_to_cutoff_seq"] != strconv.FormatInt(cutoffSeq, 10) {
		t.Fatalf("reply_to_cutoff_seq mismatch: got %q, want %q", assistant.Metadata["reply_to_cutoff_seq"], strconv.FormatInt(cutoffSeq, 10))
	}
}

func TestAppendUserEventAt_PreservesEventTimezoneInContent(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model: "gpt-4.1-mini",
	}, ManagerDeps{})
	sessionKey := "group:event-timezone"
	userZone := time.FixedZone("UTC-8", -8*60*60)
	userAt := time.Date(2026, 2, 22, 9, 30, 0, 0, userZone)

	if _, ok := m.AppendUserEventAt(sessionKey, "hello", "name=alice", userAt); !ok {
		t.Fatal("append user event failed")
	}

	result, err := m.ListChatEvents(sessionKey, chatlog.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list chat events failed: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("event count mismatch: got %d, want %d", len(result.Entries), 1)
	}

	user := result.Entries[0]
	if !strings.Contains(user.Content, "[name=alice;time=2026-02-22 09:30:00]: hello") {
		t.Fatalf("user content should preserve event timezone wall-clock time, got %q", user.Content)
	}
	parsedEventTime, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(user.Metadata["event_time"]))
	if err != nil {
		t.Fatalf("parse event_time metadata failed: %v", err)
	}
	_, offset := parsedEventTime.Zone()
	if offset != -8*60*60 {
		t.Fatalf("event_time offset mismatch: got %d, want %d", offset, -8*60*60)
	}
}

func TestAssemble_OrderStableWhenEventTimeSkews(t *testing.T) {
	m := NewManager(config.LLMConfig{
		Model: "gpt-4.1-mini",
	}, ManagerDeps{})
	sessionKey := "group:causal-stable"
	firstAt := time.Date(2026, 2, 22, 12, 1, 0, 0, time.UTC)
	secondAt := firstAt.Add(-5 * time.Minute)

	firstSeq, ok := m.AppendUserEventAt(sessionKey, "first-message", "name=alice", firstAt)
	if !ok || firstSeq == 0 {
		t.Fatal("append first user event failed")
	}
	if !m.AppendAssistantEventAt(sessionKey, "second-message", firstSeq, secondAt) {
		t.Fatal("append second assistant event failed")
	}

	assembled, err := m.contextAssembler.Assemble(context.Background(), contextassemble.Request{
		SessionKey: sessionKey,
	})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}
	recentChat, ok := assembled.Block(contextassemble.BlockRecentChat)
	if !ok {
		t.Fatalf("missing %s block", contextassemble.BlockRecentChat)
	}
	firstPos := strings.Index(recentChat.Content, "first-message")
	secondPos := strings.Index(recentChat.Content, "second-message")
	if firstPos < 0 || secondPos < 0 {
		t.Fatalf("recent_chat missing expected messages: %q", recentChat.Content)
	}
	if firstPos > secondPos {
		t.Fatalf("recent_chat order should follow causal append order, got: %q", recentChat.Content)
	}
}
