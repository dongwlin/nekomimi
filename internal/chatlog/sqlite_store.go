package chatlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
)

const DefaultSQLitePath = "data/chatlog.db"

type SQLiteStore struct {
	db     *bun.DB
	sqlDB  *sql.DB
	nextID atomic.Uint64
}

type sqliteEntry struct {
	bun.BaseModel `bun:"table:chatlog_entries,alias:chatlog_entries"`

	Seq          int64     `bun:"seq,pk,autoincrement"`
	ID           string    `bun:"id,notnull,type:text"`
	SessionKey   string    `bun:"session_key,notnull,type:text"`
	Role         string    `bun:"role,notnull,type:text"`
	Speaker      string    `bun:"speaker,notnull,type:text"`
	Content      string    `bun:"content,notnull,type:text"`
	CreatedAt    time.Time `bun:"created_at,notnull,type:datetime"`
	MetadataJSON string    `bun:"metadata_json,notnull,type:text"`
}

var _ Store = (*SQLiteStore)(nil)

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		path = DefaultSQLitePath
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create chatlog sqlite directory failed: %w", err)
		}
	}

	sqlDB, err := sql.Open(sqliteshim.ShimName, sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open chatlog sqlite failed: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	store := &SQLiteStore{
		sqlDB: sqlDB,
		db:    bun.NewDB(sqlDB, sqlitedialect.New()),
	}
	if err := store.initialize(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.sqlDB == nil {
		return nil
	}
	sqlDB := s.sqlDB
	s.sqlDB = nil
	s.db = nil
	return sqlDB.Close()
}

func (s *SQLiteStore) initialize(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := s.sqlDB.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("enable chatlog sqlite wal failed: %w", err)
	}
	if _, err := s.db.NewCreateTable().Model((*sqliteEntry)(nil)).IfNotExists().Exec(ctx); err != nil {
		return fmt.Errorf("create chatlog sqlite table failed: %w", err)
	}
	if _, err := s.db.NewRaw(
		"CREATE INDEX IF NOT EXISTS idx_chatlog_entries_session_seq ON chatlog_entries (session_key, seq)",
	).Exec(ctx); err != nil {
		return fmt.Errorf("create chatlog sqlite session_seq index failed: %w", err)
	}
	if _, err := s.db.NewRaw(
		"CREATE INDEX IF NOT EXISTS idx_chatlog_entries_session_created_seq ON chatlog_entries (session_key, created_at, seq)",
	).Exec(ctx); err != nil {
		return fmt.Errorf("create chatlog sqlite session_created_seq index failed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Append(ctx context.Context, sessionKey string, entries ...Entry) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return ErrEmptySessionKey
	}
	if len(entries) == 0 {
		return nil
	}

	now := time.Now().UTC()
	rows := make([]sqliteEntry, 0, len(entries))
	for _, entry := range entries {
		normalized := cloneEntry(entry)
		normalized.SessionKey = key
		if strings.TrimSpace(normalized.ID) == "" {
			normalized.ID = s.nextEntryID()
		}
		if normalized.CreatedAt.IsZero() {
			normalized.CreatedAt = now
		}
		metadataJSON, err := marshalMetadata(normalized.Metadata)
		if err != nil {
			return err
		}
		rows = append(rows, sqliteEntry{
			ID:           normalized.ID,
			SessionKey:   key,
			Role:         string(normalized.Role),
			Speaker:      strings.TrimSpace(normalized.Speaker),
			Content:      normalized.Content,
			CreatedAt:    normalized.CreatedAt,
			MetadataJSON: metadataJSON,
		})
	}

	if _, err := s.db.NewInsert().Model(&rows).Exec(ctx); err != nil {
		return fmt.Errorf("append chatlog entries failed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) List(ctx context.Context, sessionKey string, opts ListOptions) (ListResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ListResult{}, err
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return ListResult{}, ErrEmptySessionKey
	}

	offset, err := parseCursor(opts.Cursor)
	if err != nil {
		return ListResult{}, err
	}

	total, err := s.db.NewSelect().
		Model((*sqliteEntry)(nil)).
		Where("session_key = ?", key).
		Count(ctx)
	if err != nil {
		return ListResult{}, fmt.Errorf("count chatlog entries failed: %w", err)
	}
	if total == 0 || offset >= total {
		return ListResult{}, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = total - offset
	}

	rows := make([]sqliteEntry, 0, limit)
	if err := s.db.NewSelect().
		Model(&rows).
		Where("session_key = ?", key).
		OrderExpr("seq DESC").
		Limit(limit).
		Offset(offset).
		Scan(ctx); err != nil {
		return ListResult{}, fmt.Errorf("list chatlog entries failed: %w", err)
	}

	result := make([]Entry, 0, len(rows))
	for _, row := range rows {
		metadata, err := unmarshalMetadata(row.MetadataJSON)
		if err != nil {
			return ListResult{}, err
		}
		result = append(result, cloneEntry(Entry{
			ID:         row.ID,
			SessionKey: row.SessionKey,
			Role:       Role(row.Role),
			Speaker:    row.Speaker,
			Content:    row.Content,
			CreatedAt:  row.CreatedAt,
			Metadata:   metadata,
		}))
	}

	end := offset + len(result)
	var nextCursor string
	if end < total {
		nextCursor = strconv.Itoa(end)
	}

	return ListResult{
		Entries:    result,
		NextCursor: nextCursor,
	}, nil
}

func (s *SQLiteStore) Clear(ctx context.Context, sessionKey string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return ErrEmptySessionKey
	}

	if _, err := s.db.NewDelete().
		Model((*sqliteEntry)(nil)).
		Where("session_key = ?", key).
		Exec(ctx); err != nil {
		return fmt.Errorf("clear chatlog entries failed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) nextEntryID() string {
	n := s.nextID.Add(1)
	return "chat_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + strconv.FormatUint(n, 10)
}

func marshalMetadata(metadata map[string]string) (string, error) {
	if len(metadata) == 0 {
		return "", nil
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal chatlog metadata failed: %w", err)
	}
	return string(payload), nil
}

func unmarshalMetadata(encoded string) (map[string]string, error) {
	trimmed := strings.TrimSpace(encoded)
	if trimmed == "" {
		return nil, nil
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(trimmed), &metadata); err != nil {
		return nil, fmt.Errorf("unmarshal chatlog metadata failed: %w", err)
	}
	if len(metadata) == 0 {
		return nil, nil
	}
	return metadata, nil
}

func sqliteDSN(path string) string {
	normalized := filepath.ToSlash(path)
	return "file:" + normalized + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}
