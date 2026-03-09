package llm

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
)

func TestManager_WithSQLiteStores_PersistsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	chatStore, err := chatlog.NewSQLiteStore(filepath.Join(root, "chatlog.db"))
	if err != nil {
		t.Fatalf("new chat sqlite store failed: %v", err)
	}
	diaryStore, err := diary.NewSQLiteStore(filepath.Join(root, "diary.db"))
	if err != nil {
		_ = chatStore.Close()
		t.Fatalf("new diary sqlite store failed: %v", err)
	}
	manager := NewManager(config.LLMConfig{
		Model: "gpt-4.1-mini",
	}, ManagerDeps{
		ChatStore:  chatStore,
		DiaryStore: diaryStore,
	})

	manager.AppendTurn("group:sqlite", "hello", "name=alice", "world")
	if _, err := manager.diaryStore.Write(context.Background(), "group:sqlite", diary.Entry{
		Author:  "assistant",
		Content: "memory",
	}); err != nil {
		t.Fatalf("write diary failed: %v", err)
	}

	if err := chatStore.Close(); err != nil {
		t.Fatalf("close chat store failed: %v", err)
	}
	if err := diaryStore.Close(); err != nil {
		t.Fatalf("close diary store failed: %v", err)
	}

	reopenedChatStore, err := chatlog.NewSQLiteStore(filepath.Join(root, "chatlog.db"))
	if err != nil {
		t.Fatalf("reopen chat sqlite store failed: %v", err)
	}
	defer func() {
		if err := reopenedChatStore.Close(); err != nil {
			t.Fatalf("close reopened chat store failed: %v", err)
		}
	}()
	reopenedDiaryStore, err := diary.NewSQLiteStore(filepath.Join(root, "diary.db"))
	if err != nil {
		t.Fatalf("reopen diary sqlite store failed: %v", err)
	}
	defer func() {
		if err := reopenedDiaryStore.Close(); err != nil {
			t.Fatalf("close reopened diary store failed: %v", err)
		}
	}()

	reopenedManager := NewManager(config.LLMConfig{
		Model: "gpt-4.1-mini",
	}, ManagerDeps{
		ChatStore:  reopenedChatStore,
		DiaryStore: reopenedDiaryStore,
	})

	chatResult, err := reopenedManager.ListChatEvents("group:sqlite", chatlog.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list chat after reopen failed: %v", err)
	}
	if len(chatResult.Entries) != 2 {
		t.Fatalf("unexpected chat event count after reopen: got %d, want 2", len(chatResult.Entries))
	}

	diaryResult, err := reopenedManager.diaryStore.List(context.Background(), "group:sqlite", diary.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list diary after reopen failed: %v", err)
	}
	if len(diaryResult.Entries) != 1 {
		t.Fatalf("unexpected diary count after reopen: got %d, want 1", len(diaryResult.Entries))
	}
}

func TestManager_WithSQLiteStores_ReloadKeepsHistoryAndClearRemovesDiary(t *testing.T) {
	chatStore, diaryStore := newTestSQLiteStores(t)
	manager := NewManager(config.LLMConfig{
		Model: "gpt-4.1-mini",
	}, ManagerDeps{
		ChatStore:  chatStore,
		DiaryStore: diaryStore,
	})

	sessionKey := "group:reload"
	manager.AppendTurn(sessionKey, "hello", "name=alice", "world")
	if _, err := manager.diaryStore.Write(context.Background(), sessionKey, diary.Entry{
		Author:  "assistant",
		Content: "memory",
	}); err != nil {
		t.Fatalf("write diary failed: %v", err)
	}

	if err := manager.ReloadConfig(config.LLMConfig{
		Model: "gpt-4.1-mini",
		ContextAssembly: config.ContextAssemblyConfig{
			RecentChatLimit:  50,
			RecentDiaryLimit: 50,
		},
	}); err != nil {
		t.Fatalf("reload config failed: %v", err)
	}

	chatResult, err := manager.ListChatEvents(sessionKey, chatlog.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list chat events failed: %v", err)
	}
	if len(chatResult.Entries) != 2 {
		t.Fatalf("unexpected chat event count after reload: got %d, want 2", len(chatResult.Entries))
	}

	diaryResult, err := manager.diaryStore.List(context.Background(), sessionKey, diary.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list diary failed: %v", err)
	}
	if len(diaryResult.Entries) != 1 {
		t.Fatalf("unexpected diary count after reload: got %d, want 1", len(diaryResult.Entries))
	}

	manager.ClearHistory(sessionKey)

	chatAfterClear, err := manager.ListChatEvents(sessionKey, chatlog.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list chat after clear failed: %v", err)
	}
	if len(chatAfterClear.Entries) != 0 {
		t.Fatalf("chat history should be empty after clear, got %d entries", len(chatAfterClear.Entries))
	}

	diaryAfterClear, err := manager.diaryStore.List(context.Background(), sessionKey, diary.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list diary after clear failed: %v", err)
	}
	if len(diaryAfterClear.Entries) != 0 {
		t.Fatalf("diary history should be empty after clear, got %d entries", len(diaryAfterClear.Entries))
	}
}

func newTestSQLiteStores(t *testing.T) (*chatlog.SQLiteStore, *diary.SQLiteStore) {
	t.Helper()
	root := t.TempDir()

	chatStore, err := chatlog.NewSQLiteStore(filepath.Join(root, "chatlog.db"))
	if err != nil {
		t.Fatalf("new chat sqlite store failed: %v", err)
	}
	diaryStore, err := diary.NewSQLiteStore(filepath.Join(root, "diary.db"))
	if err != nil {
		_ = chatStore.Close()
		t.Fatalf("new diary sqlite store failed: %v", err)
	}

	t.Cleanup(func() {
		if err := chatStore.Close(); err != nil {
			t.Fatalf("close chat sqlite store failed: %v", err)
		}
		if err := diaryStore.Close(); err != nil {
			t.Fatalf("close diary sqlite store failed: %v", err)
		}
	})
	return chatStore, diaryStore
}
