package chatlog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStore_AppendListPaginationAndLimit(t *testing.T) {
	store := newTestSQLiteStore(t)
	sessionKey := "group:100"

	err := store.Append(
		context.Background(),
		sessionKey,
		Entry{Role: RoleUser, Content: "u1"},
		Entry{Role: RoleAssistant, Content: "a1"},
		Entry{Role: RoleUser, Content: "u2"},
		Entry{Role: RoleAssistant, Content: "a2"},
		Entry{Role: RoleUser, Content: "u3"},
	)
	if err != nil {
		t.Fatalf("append failed: %v", err)
	}

	page1, err := store.List(context.Background(), sessionKey, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("list page1 failed: %v", err)
	}
	assertEntryContents(t, page1.Entries, []string{"u3", "a2"})
	if page1.NextCursor != "2" {
		t.Fatalf("unexpected page1 next cursor: %q", page1.NextCursor)
	}

	page2, err := store.List(context.Background(), sessionKey, ListOptions{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("list page2 failed: %v", err)
	}
	assertEntryContents(t, page2.Entries, []string{"u2", "a1"})
	if page2.NextCursor != "4" {
		t.Fatalf("unexpected page2 next cursor: %q", page2.NextCursor)
	}

	page3, err := store.List(context.Background(), sessionKey, ListOptions{Limit: 2, Cursor: page2.NextCursor})
	if err != nil {
		t.Fatalf("list page3 failed: %v", err)
	}
	assertEntryContents(t, page3.Entries, []string{"u1"})
	if page3.NextCursor != "" {
		t.Fatalf("unexpected page3 next cursor: %q", page3.NextCursor)
	}
}

func TestSQLiteStore_AppendNormalizesAndCopiesEntry(t *testing.T) {
	store := newTestSQLiteStore(t)
	sessionKey := "private:7"

	metadata := map[string]string{"origin": "caller"}
	input := Entry{
		SessionKey: "ignored",
		Role:       RoleUser,
		Content:    "hello",
		Speaker:    "alice",
		Metadata:   metadata,
	}

	if err := store.Append(context.Background(), sessionKey, input); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	metadata["origin"] = "mutated"

	result, err := store.List(context.Background(), sessionKey, ListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("unexpected entry count: got %d, want 1", len(result.Entries))
	}

	entry := result.Entries[0]
	if entry.SessionKey != sessionKey {
		t.Fatalf("unexpected session key: got %q, want %q", entry.SessionKey, sessionKey)
	}
	if entry.ID == "" {
		t.Fatalf("entry id should be auto generated")
	}
	if entry.CreatedAt.IsZero() {
		t.Fatalf("entry created_at should be populated")
	}
	if entry.Speaker != "alice" {
		t.Fatalf("speaker should round-trip, got %q", entry.Speaker)
	}
	if entry.Metadata["origin"] != "caller" {
		t.Fatalf("metadata should be copied on write, got %q", entry.Metadata["origin"])
	}

	entry.Metadata["origin"] = "changed-after-read"
	resultAgain, err := store.List(context.Background(), sessionKey, ListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if resultAgain.Entries[0].Metadata["origin"] != "caller" {
		t.Fatalf("metadata should be copied on read, got %q", resultAgain.Entries[0].Metadata["origin"])
	}
}

func TestSQLiteStore_SessionIsolationAndClear(t *testing.T) {
	store := newTestSQLiteStore(t)

	if err := store.Append(context.Background(), "s1", Entry{Role: RoleUser, Content: "one"}); err != nil {
		t.Fatalf("append s1 failed: %v", err)
	}
	if err := store.Append(context.Background(), "s2", Entry{Role: RoleUser, Content: "two"}); err != nil {
		t.Fatalf("append s2 failed: %v", err)
	}

	s1, err := store.List(context.Background(), "s1", ListOptions{})
	if err != nil {
		t.Fatalf("list s1 failed: %v", err)
	}
	assertEntryContents(t, s1.Entries, []string{"one"})

	s2, err := store.List(context.Background(), "s2", ListOptions{})
	if err != nil {
		t.Fatalf("list s2 failed: %v", err)
	}
	assertEntryContents(t, s2.Entries, []string{"two"})

	if err := store.Clear(context.Background(), "s1"); err != nil {
		t.Fatalf("clear s1 failed: %v", err)
	}

	s1AfterClear, err := store.List(context.Background(), "s1", ListOptions{})
	if err != nil {
		t.Fatalf("list s1 after clear failed: %v", err)
	}
	if len(s1AfterClear.Entries) != 0 {
		t.Fatalf("s1 should be empty after clear")
	}

	s2AfterClear, err := store.List(context.Background(), "s2", ListOptions{})
	if err != nil {
		t.Fatalf("list s2 after clear failed: %v", err)
	}
	assertEntryContents(t, s2AfterClear.Entries, []string{"two"})
}

func TestSQLiteStore_ListInvalidCursor(t *testing.T) {
	store := newTestSQLiteStore(t)
	if err := store.Append(context.Background(), "s", Entry{Role: RoleUser, Content: "v"}); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	_, err := store.List(context.Background(), "s", ListOptions{Cursor: "not-a-number"})
	if err == nil {
		t.Fatalf("list with invalid cursor should fail")
	}
	if err != ErrInvalidCursor {
		t.Fatalf("unexpected error: got %v, want %v", err, ErrInvalidCursor)
	}
}

func TestSQLiteStore_PersistsAcrossReopenAndCreatesDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "chatlog.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store failed: %v", err)
	}
	if err := store.Append(context.Background(), "s", Entry{Role: RoleUser, Content: "persist"}); err != nil {
		t.Fatalf("append failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected sqlite db file to exist: %v", err)
	}

	reopened, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store failed: %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("close reopened store failed: %v", err)
		}
	}()

	result, err := reopened.List(context.Background(), "s", ListOptions{})
	if err != nil {
		t.Fatalf("list after reopen failed: %v", err)
	}
	assertEntryContents(t, result.Entries, []string{"persist"})
}

func TestSQLiteStore_ListPreservesAppendOrderWhenCreatedAtSkews(t *testing.T) {
	store := newTestSQLiteStore(t)
	sessionKey := "group:skew"
	firstAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(-10 * time.Minute)

	if err := store.Append(
		context.Background(),
		sessionKey,
		Entry{Role: RoleUser, Content: "first", CreatedAt: firstAt},
		Entry{Role: RoleAssistant, Content: "second", CreatedAt: secondAt},
	); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	result, err := store.List(context.Background(), sessionKey, ListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	assertEntryContents(t, result.Entries, []string{"second", "first"})
}

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "chatlog.db"))
	if err != nil {
		t.Fatalf("new sqlite store failed: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store failed: %v", err)
		}
	})
	return store
}
