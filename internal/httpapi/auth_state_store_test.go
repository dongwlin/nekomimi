package httpapi

import (
	"path/filepath"
	"testing"
)

func TestAuthStateStore_InitializeAndPersist(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "auth.db")

	firstStore, initialPassphrase, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create first store failed: %v", err)
	}
	if initialPassphrase == "" {
		t.Fatal("expected initial passphrase to be generated")
	}

	firstVersion, err := firstStore.currentTokenVersion()
	if err != nil {
		t.Fatalf("load token version failed: %v", err)
	}
	if firstVersion != 1 {
		t.Fatalf("unexpected initial token version: %d", firstVersion)
	}

	if _, err := firstStore.authenticate(initialPassphrase); err != nil {
		t.Fatalf("authenticate initial passphrase failed: %v", err)
	}
	if _, err := firstStore.rotatePassphrase(initialPassphrase, "new-password-123"); err != nil {
		t.Fatalf("rotate passphrase failed: %v", err)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatalf("close first store failed: %v", err)
	}

	secondStore, secondInitialPassphrase, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create second store failed: %v", err)
	}
	defer func() {
		_ = secondStore.Close()
	}()
	if secondInitialPassphrase != "" {
		t.Fatalf("expected no new passphrase on existing DB, got %q", secondInitialPassphrase)
	}
	if _, err := secondStore.authenticate(initialPassphrase); err == nil {
		t.Fatal("old passphrase should be invalid after rotation")
	}
	if _, err := secondStore.authenticate("new-password-123"); err != nil {
		t.Fatalf("new passphrase should authenticate, got %v", err)
	}

	secondVersion, err := secondStore.currentTokenVersion()
	if err != nil {
		t.Fatalf("load token version from second store failed: %v", err)
	}
	if secondVersion != 2 {
		t.Fatalf("unexpected token version after reload: %d", secondVersion)
	}
}
