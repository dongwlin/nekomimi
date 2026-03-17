package diary

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStore_WriteListPaginationAndLimit(t *testing.T) {
	store := newTestSQLiteStore(t)
	sessionKey := "group:200"

	writes := []string{"n1", "n2", "n3", "n4", "n5"}
	for _, content := range writes {
		if _, err := store.Write(context.Background(), sessionKey, Entry{Content: content, Author: "assistant"}); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	page1, err := store.List(context.Background(), sessionKey, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("list page1 failed: %v", err)
	}
	assertDiaryContents(t, page1.Entries, []string{"n5", "n4"})
	if page1.NextCursor != "2" {
		t.Fatalf("unexpected page1 next cursor: %q", page1.NextCursor)
	}

	page2, err := store.List(context.Background(), sessionKey, ListOptions{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("list page2 failed: %v", err)
	}
	assertDiaryContents(t, page2.Entries, []string{"n3", "n2"})
	if page2.NextCursor != "4" {
		t.Fatalf("unexpected page2 next cursor: %q", page2.NextCursor)
	}

	page3, err := store.List(context.Background(), sessionKey, ListOptions{Limit: 2, Cursor: page2.NextCursor})
	if err != nil {
		t.Fatalf("list page3 failed: %v", err)
	}
	assertDiaryContents(t, page3.Entries, []string{"n1"})
	if page3.NextCursor != "" {
		t.Fatalf("unexpected page3 next cursor: %q", page3.NextCursor)
	}
}

func TestSQLiteStore_WriteNormalizesAndCopiesEntry(t *testing.T) {
	store := newTestSQLiteStore(t)
	sessionKey := "private:9"

	tags := []string{"todo"}
	metadata := map[string]string{"source": "caller"}
	written, err := store.Write(context.Background(), sessionKey, Entry{
		SessionKey: "ignored",
		Content:    "remember this",
		Author:     "assistant",
		Tags:       tags,
		Metadata:   metadata,
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if written.ID == "" {
		t.Fatalf("entry id should be auto generated")
	}
	if written.CreatedAt.IsZero() {
		t.Fatalf("entry created_at should be populated")
	}
	if written.SessionKey != sessionKey {
		t.Fatalf("unexpected session key: got %q, want %q", written.SessionKey, sessionKey)
	}

	tags[0] = "changed"
	metadata["source"] = "mutated"
	written.Tags[0] = "changed-again"
	written.Metadata["source"] = "changed-again"

	listed, err := store.List(context.Background(), sessionKey, ListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listed.Entries) != 1 {
		t.Fatalf("unexpected entry count: got %d, want 1", len(listed.Entries))
	}
	got := listed.Entries[0]
	if got.Tags[0] != "todo" {
		t.Fatalf("tags should be copied on write, got %q", got.Tags[0])
	}
	if got.Metadata["source"] != "caller" {
		t.Fatalf("metadata should be copied on write, got %q", got.Metadata["source"])
	}

	got.Tags[0] = "changed-after-read"
	got.Metadata["source"] = "changed-after-read"
	listedAgain, err := store.List(context.Background(), sessionKey, ListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if listedAgain.Entries[0].Tags[0] != "todo" {
		t.Fatalf("tags should be copied on read, got %q", listedAgain.Entries[0].Tags[0])
	}
	if listedAgain.Entries[0].Metadata["source"] != "caller" {
		t.Fatalf("metadata should be copied on read, got %q", listedAgain.Entries[0].Metadata["source"])
	}
}

func TestSQLiteStore_SessionIsolationAndClear(t *testing.T) {
	store := newTestSQLiteStore(t)

	if _, err := store.Write(context.Background(), "s1", Entry{Content: "one"}); err != nil {
		t.Fatalf("write s1 failed: %v", err)
	}
	if _, err := store.Write(context.Background(), "s2", Entry{Content: "two"}); err != nil {
		t.Fatalf("write s2 failed: %v", err)
	}

	s1, err := store.List(context.Background(), "s1", ListOptions{})
	if err != nil {
		t.Fatalf("list s1 failed: %v", err)
	}
	assertDiaryContents(t, s1.Entries, []string{"one"})

	s2, err := store.List(context.Background(), "s2", ListOptions{})
	if err != nil {
		t.Fatalf("list s2 failed: %v", err)
	}
	assertDiaryContents(t, s2.Entries, []string{"two"})

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
	assertDiaryContents(t, s2AfterClear.Entries, []string{"two"})
}

func TestSQLiteStore_ListInvalidCursor(t *testing.T) {
	store := newTestSQLiteStore(t)
	if _, err := store.Write(context.Background(), "s", Entry{Content: "v"}); err != nil {
		t.Fatalf("write failed: %v", err)
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
	dbPath := filepath.Join(t.TempDir(), "nested", "diary.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store failed: %v", err)
	}
	if _, err := store.Write(context.Background(), "s", Entry{Content: "persist", Author: "assistant"}); err != nil {
		t.Fatalf("write failed: %v", err)
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
	assertDiaryContents(t, result.Entries, []string{"persist"})
}

func TestSQLiteStore_ListPreservesWriteOrderWhenCreatedAtSkews(t *testing.T) {
	store := newTestSQLiteStore(t)
	sessionKey := "group:skew"
	firstAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(-10 * time.Minute)

	if _, err := store.Write(context.Background(), sessionKey, Entry{Content: "first", CreatedAt: firstAt}); err != nil {
		t.Fatalf("write first failed: %v", err)
	}
	if _, err := store.Write(context.Background(), sessionKey, Entry{Content: "second", CreatedAt: secondAt}); err != nil {
		t.Fatalf("write second failed: %v", err)
	}

	result, err := store.List(context.Background(), sessionKey, ListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	assertDiaryContents(t, result.Entries, []string{"second", "first"})
}

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "diary.db"))
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
