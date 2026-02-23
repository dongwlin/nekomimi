package httpapi

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	paseto "aidanwoods.dev/go-paseto"
	"github.com/uptrace/bun/driver/sqliteshim"
)

const testPasetoKeyHexForStore = "9f10ec4ee8ca74d6b6a6460f6609409e63d76ca4bc5f8cc86f3bd9464f694f16"

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

func TestAuthStateStore_ResolvePasetoKeyHexGeneratedAndLoaded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "auth.db")

	firstStore, _, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create first store failed: %v", err)
	}

	generatedKey, action, err := firstStore.resolvePasetoKeyHex("")
	if err != nil {
		t.Fatalf("resolve generated key failed: %v", err)
	}
	if action != pasetoKeyActionGenerated {
		t.Fatalf("unexpected key action: %q", action)
	}
	if _, err := paseto.V4SymmetricKeyFromHex(generatedKey); err != nil {
		t.Fatalf("generated key is invalid: %v", err)
	}

	if err := firstStore.Close(); err != nil {
		t.Fatalf("close first store failed: %v", err)
	}

	secondStore, _, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create second store failed: %v", err)
	}
	defer func() {
		_ = secondStore.Close()
	}()

	loadedKey, action, err := secondStore.resolvePasetoKeyHex("")
	if err != nil {
		t.Fatalf("resolve loaded key failed: %v", err)
	}
	if action != pasetoKeyActionLoadedFromDB {
		t.Fatalf("unexpected key action: %q", action)
	}
	if loadedKey != generatedKey {
		t.Fatalf("key mismatch after reload: got %q, want %q", loadedKey, generatedKey)
	}
}

func TestAuthStateStore_ResolvePasetoKeyHexSyncFromConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "auth.db")

	firstStore, _, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create first store failed: %v", err)
	}

	keyFromConfig, action, err := firstStore.resolvePasetoKeyHex(testPasetoKeyHexForStore)
	if err != nil {
		t.Fatalf("resolve configured key failed: %v", err)
	}
	if action != pasetoKeyActionSyncedFromConfig {
		t.Fatalf("unexpected key action: %q", action)
	}
	if keyFromConfig != testPasetoKeyHexForStore {
		t.Fatalf("configured key mismatch: got %q", keyFromConfig)
	}

	keyFromConfig, action, err = firstStore.resolvePasetoKeyHex(testPasetoKeyHexForStore)
	if err != nil {
		t.Fatalf("resolve unchanged configured key failed: %v", err)
	}
	if action != pasetoKeyActionUnchangedConfig {
		t.Fatalf("unexpected key action: %q", action)
	}

	loadedKey, action, err := firstStore.resolvePasetoKeyHex("")
	if err != nil {
		t.Fatalf("resolve loaded key failed: %v", err)
	}
	if action != pasetoKeyActionLoadedFromDB {
		t.Fatalf("unexpected key action: %q", action)
	}
	if loadedKey != testPasetoKeyHexForStore {
		t.Fatalf("loaded key mismatch: got %q", loadedKey)
	}

	if err := firstStore.Close(); err != nil {
		t.Fatalf("close first store failed: %v", err)
	}

	secondStore, _, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create second store failed: %v", err)
	}
	defer func() {
		_ = secondStore.Close()
	}()

	loadedAfterReload, action, err := secondStore.resolvePasetoKeyHex("")
	if err != nil {
		t.Fatalf("resolve loaded key after reload failed: %v", err)
	}
	if action != pasetoKeyActionLoadedFromDB {
		t.Fatalf("unexpected key action: %q", action)
	}
	if loadedAfterReload != testPasetoKeyHexForStore {
		t.Fatalf("loaded key after reload mismatch: got %q", loadedAfterReload)
	}
}

func TestAuthStateStore_ResolvePasetoKeyHexResetInvalidDBKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "auth.db")

	firstStore, _, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create first store failed: %v", err)
	}

	if _, err := firstStore.db.NewUpdate().
		Model((*authStateRecord)(nil)).
		Set("paseto_key_hex = ?", "bad-key").
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", authStateRowID).
		Exec(context.Background()); err != nil {
		t.Fatalf("inject invalid db key failed: %v", err)
	}

	if err := firstStore.Close(); err != nil {
		t.Fatalf("close first store failed: %v", err)
	}

	secondStore, _, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create second store failed: %v", err)
	}
	defer func() {
		_ = secondStore.Close()
	}()

	resetKey, action, err := secondStore.resolvePasetoKeyHex("")
	if err != nil {
		t.Fatalf("resolve key after invalid db state failed: %v", err)
	}
	if action != pasetoKeyActionResetInvalid {
		t.Fatalf("unexpected key action: %q", action)
	}
	if _, err := paseto.V4SymmetricKeyFromHex(resetKey); err != nil {
		t.Fatalf("reset key is invalid: %v", err)
	}

	loadedKey, action, err := secondStore.resolvePasetoKeyHex("")
	if err != nil {
		t.Fatalf("resolve key after reset failed: %v", err)
	}
	if action != pasetoKeyActionLoadedFromDB {
		t.Fatalf("unexpected key action: %q", action)
	}
	if loadedKey != resetKey {
		t.Fatalf("loaded key mismatch after reset: got %q, want %q", loadedKey, resetKey)
	}
}

func TestAuthStateStore_InitializeMigratesLegacySchemaForPasetoKeyColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "auth.db")
	createLegacyAuthStateDB(t, dbPath)

	store, initialPassphrase, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("create store with legacy schema failed: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()
	if initialPassphrase != "" {
		t.Fatalf("expected no new passphrase for existing row, got %q", initialPassphrase)
	}

	if !hasPasetoKeyColumn(t, dbPath) {
		t.Fatal("paseto_key_hex column should be added for legacy schema")
	}

	keyHex, action, err := store.resolvePasetoKeyHex("")
	if err != nil {
		t.Fatalf("resolve paseto key for migrated schema failed: %v", err)
	}
	if action != pasetoKeyActionGenerated {
		t.Fatalf("unexpected key action: %q", action)
	}
	if _, err := paseto.V4SymmetricKeyFromHex(keyHex); err != nil {
		t.Fatalf("generated key for migrated schema is invalid: %v", err)
	}
}

func createLegacyAuthStateDB(t *testing.T, dbPath string) {
	t.Helper()

	sqlDB, err := sql.Open(sqliteshim.ShimName, sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	if _, err := sqlDB.Exec(`
CREATE TABLE auth_state (
	id INTEGER PRIMARY KEY,
	passphrase_hash TEXT NOT NULL,
	token_version INTEGER NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
)`); err != nil {
		t.Fatalf("create legacy auth_state table failed: %v", err)
	}

	hash, err := hashPassphrase("legacy-passphrase")
	if err != nil {
		t.Fatalf("hash passphrase failed: %v", err)
	}
	now := time.Now().UTC()
	if _, err := sqlDB.Exec(
		`INSERT INTO auth_state (id, passphrase_hash, token_version, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		authStateRowID, hash, int64(1), now, now,
	); err != nil {
		t.Fatalf("insert legacy auth_state row failed: %v", err)
	}
}

func hasPasetoKeyColumn(t *testing.T, dbPath string) bool {
	t.Helper()

	sqlDB, err := sql.Open(sqliteshim.ShimName, sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	rows, err := sqlDB.Query("PRAGMA table_info(auth_state)")
	if err != nil {
		t.Fatalf("query table_info failed: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid          int64
			name         string
			columnType   string
			notNull      int64
			defaultValue any
			pk           int64
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info failed: %v", err)
		}
		if name == "paseto_key_hex" {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info failed: %v", err)
	}
	return false
}
