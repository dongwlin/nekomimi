package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"golang.org/x/crypto/bcrypt"
)

const (
	authStateRowID           = 1
	passphraseMinLength      = 8
	generatedPassphraseChars = 32
	bcryptCost               = 12
)

var (
	errStoreInvalidPassphrase = errors.New("invalid passphrase")
	errStoreWeakPassphrase    = errors.New("weak passphrase")
)

type authStateRecord struct {
	bun.BaseModel `bun:"table:auth_state,alias:auth_state"`

	ID             int64     `bun:"id,pk"`
	PassphraseHash string    `bun:"passphrase_hash,notnull"`
	TokenVersion   int64     `bun:"token_version,notnull"`
	CreatedAt      time.Time `bun:"created_at,notnull"`
	UpdatedAt      time.Time `bun:"updated_at,notnull"`
}

type authStateStore struct {
	mu             sync.RWMutex
	db             *bun.DB
	sqlDB          *sql.DB
	passphraseHash string
	tokenVersion   int64
}

func newSQLiteAuthStateStore(dbPath string) (*authStateStore, string, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, "", fmt.Errorf("sqlite path is empty")
	}

	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, "", fmt.Errorf("create sqlite directory failed: %w", err)
		}
	}

	sqlDB, err := sql.Open(sqliteshim.ShimName, dbPath)
	if err != nil {
		return nil, "", fmt.Errorf("open sqlite database failed: %w", err)
	}

	db := bun.NewDB(sqlDB, sqlitedialect.New())
	store := &authStateStore{
		db:    db,
		sqlDB: sqlDB,
	}

	initialPassphrase, err := store.initialize(context.Background())
	if err != nil {
		_ = store.Close()
		return nil, "", err
	}

	return store, initialPassphrase, nil
}

func (s *authStateStore) Close() error {
	return s.sqlDB.Close()
}

func (s *authStateStore) initialize(ctx context.Context) (string, error) {
	if _, err := s.db.NewCreateTable().Model((*authStateRecord)(nil)).IfNotExists().Exec(ctx); err != nil {
		return "", fmt.Errorf("create auth_state table failed: %w", err)
	}

	record := new(authStateRecord)
	err := s.db.NewSelect().Model(record).Where("id = ?", authStateRowID).Limit(1).Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("load auth_state failed: %w", err)
	}

	if errors.Is(err, sql.ErrNoRows) {
		plainPassphrase, genErr := generateAlnumPassphrase(generatedPassphraseChars)
		if genErr != nil {
			return "", genErr
		}

		hashedPassphrase, hashErr := hashPassphrase(plainPassphrase)
		if hashErr != nil {
			return "", hashErr
		}

		now := time.Now().UTC()
		record = &authStateRecord{
			ID:             authStateRowID,
			PassphraseHash: hashedPassphrase,
			TokenVersion:   1,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if _, insertErr := s.db.NewInsert().Model(record).Exec(ctx); insertErr != nil {
			return "", fmt.Errorf("insert initial auth_state failed: %w", insertErr)
		}

		s.mu.Lock()
		s.passphraseHash = record.PassphraseHash
		s.tokenVersion = record.TokenVersion
		s.mu.Unlock()

		return plainPassphrase, nil
	}

	s.mu.Lock()
	s.passphraseHash = record.PassphraseHash
	s.tokenVersion = record.TokenVersion
	s.mu.Unlock()
	return "", nil
}

func (s *authStateStore) authenticate(passphrase string) (int64, error) {
	s.mu.RLock()
	hash := s.passphraseHash
	version := s.tokenVersion
	s.mu.RUnlock()

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(passphrase)) != nil {
		return 0, errStoreInvalidPassphrase
	}
	return version, nil
}

func (s *authStateStore) currentTokenVersion() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.tokenVersion < 1 {
		return 0, fmt.Errorf("invalid token version")
	}
	return s.tokenVersion, nil
}

func (s *authStateStore) rotatePassphrase(currentPassphrase string, newPassphrase string) (int64, error) {
	if len(newPassphrase) < passphraseMinLength {
		return 0, errStoreWeakPassphrase
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if bcrypt.CompareHashAndPassword([]byte(s.passphraseHash), []byte(currentPassphrase)) != nil {
		return 0, errStoreInvalidPassphrase
	}

	hashedPassphrase, err := hashPassphrase(newPassphrase)
	if err != nil {
		return 0, err
	}

	nextVersion := s.tokenVersion + 1
	now := time.Now().UTC()

	_, err = s.db.NewUpdate().
		Model((*authStateRecord)(nil)).
		Set("passphrase_hash = ?", hashedPassphrase).
		Set("token_version = ?", nextVersion).
		Set("updated_at = ?", now).
		Where("id = ?", authStateRowID).
		Exec(context.Background())
	if err != nil {
		return 0, fmt.Errorf("update auth_state failed: %w", err)
	}

	s.passphraseHash = hashedPassphrase
	s.tokenVersion = nextVersion
	return nextVersion, nil
}

func hashPassphrase(passphrase string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(passphrase), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash passphrase failed: %w", err)
	}
	return string(hashed), nil
}

func generateAlnumPassphrase(length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if length < 1 {
		return "", fmt.Errorf("invalid passphrase length: %d", length)
	}

	builder := strings.Builder{}
	builder.Grow(length)

	limit := big.NewInt(int64(len(alphabet)))
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate random passphrase failed: %w", err)
		}
		builder.WriteByte(alphabet[n.Int64()])
	}

	return builder.String(), nil
}
