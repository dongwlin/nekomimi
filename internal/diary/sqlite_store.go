package diary

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

const DefaultSQLitePath = "data/diary.db"

type SQLiteStore struct {
	db     *bun.DB
	sqlDB  *sql.DB
	nextID atomic.Uint64
}

type sqliteEntry struct {
	bun.BaseModel `bun:"table:diary_entries,alias:diary_entries"`

	Seq          int64     `bun:"seq,pk,autoincrement"`
	ID           string    `bun:"id,notnull,type:text"`
	SessionKey   string    `bun:"session_key,notnull,type:text"`
	Content      string    `bun:"content,notnull,type:text"`
	Author       string    `bun:"author,notnull,type:text"`
	CreatedAt    time.Time `bun:"created_at,notnull,type:datetime"`
	TagsJSON     string    `bun:"tags_json,notnull,type:text"`
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
			return nil, fmt.Errorf("create diary sqlite directory failed: %w", err)
		}
	}

	sqlDB, err := sql.Open(sqliteshim.ShimName, sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open diary sqlite failed: %w", err)
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
		return fmt.Errorf("enable diary sqlite wal failed: %w", err)
	}
	if _, err := s.db.NewCreateTable().Model((*sqliteEntry)(nil)).IfNotExists().Exec(ctx); err != nil {
		return fmt.Errorf("create diary sqlite table failed: %w", err)
	}
	if _, err := s.db.NewRaw(
		"CREATE INDEX IF NOT EXISTS idx_diary_entries_session_seq ON diary_entries (session_key, seq)",
	).Exec(ctx); err != nil {
		return fmt.Errorf("create diary sqlite session_seq index failed: %w", err)
	}
	if _, err := s.db.NewRaw(
		"CREATE INDEX IF NOT EXISTS idx_diary_entries_session_created_seq ON diary_entries (session_key, created_at, seq)",
	).Exec(ctx); err != nil {
		return fmt.Errorf("create diary sqlite session_created_seq index failed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Write(ctx context.Context, sessionKey string, entry Entry) (Entry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return Entry{}, ErrEmptySessionKey
	}

	normalized := cloneEntry(entry)
	normalized.SessionKey = key
	if strings.TrimSpace(normalized.ID) == "" {
		normalized.ID = s.nextEntryID()
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = time.Now().UTC()
	}

	tagsJSON, err := marshalTags(normalized.Tags)
	if err != nil {
		return Entry{}, err
	}
	metadataJSON, err := marshalMetadata(normalized.Metadata)
	if err != nil {
		return Entry{}, err
	}

	row := sqliteEntry{
		ID:           normalized.ID,
		SessionKey:   key,
		Content:      normalized.Content,
		Author:       strings.TrimSpace(normalized.Author),
		CreatedAt:    normalized.CreatedAt,
		TagsJSON:     tagsJSON,
		MetadataJSON: metadataJSON,
	}
	if _, err := s.db.NewInsert().Model(&row).Exec(ctx); err != nil {
		return Entry{}, fmt.Errorf("write diary entry failed: %w", err)
	}
	return cloneEntry(normalized), nil
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
		return ListResult{}, fmt.Errorf("count diary entries failed: %w", err)
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
		return ListResult{}, fmt.Errorf("list diary entries failed: %w", err)
	}

	result := make([]Entry, 0, len(rows))
	for _, row := range rows {
		tags, err := unmarshalTags(row.TagsJSON)
		if err != nil {
			return ListResult{}, err
		}
		metadata, err := unmarshalMetadata(row.MetadataJSON)
		if err != nil {
			return ListResult{}, err
		}
		result = append(result, cloneEntry(Entry{
			ID:         row.ID,
			SessionKey: row.SessionKey,
			Content:    row.Content,
			Author:     row.Author,
			CreatedAt:  row.CreatedAt,
			Tags:       tags,
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
		return fmt.Errorf("clear diary entries failed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) nextEntryID() string {
	n := s.nextID.Add(1)
	return "diary_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + strconv.FormatUint(n, 10)
}

func marshalTags(tags []string) (string, error) {
	if len(tags) == 0 {
		return "", nil
	}
	payload, err := json.Marshal(tags)
	if err != nil {
		return "", fmt.Errorf("marshal diary tags failed: %w", err)
	}
	return string(payload), nil
}

func unmarshalTags(encoded string) ([]string, error) {
	trimmed := strings.TrimSpace(encoded)
	if trimmed == "" {
		return nil, nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(trimmed), &tags); err != nil {
		return nil, fmt.Errorf("unmarshal diary tags failed: %w", err)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func marshalMetadata(metadata map[string]string) (string, error) {
	if len(metadata) == 0 {
		return "", nil
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal diary metadata failed: %w", err)
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
		return nil, fmt.Errorf("unmarshal diary metadata failed: %w", err)
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
