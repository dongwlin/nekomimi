package chatlog

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrEmptySessionKey indicates sessionKey is blank.
	ErrEmptySessionKey = errors.New("session key is required")
	// ErrInvalidCursor indicates cursor is not a non-negative integer.
	ErrInvalidCursor = errors.New("invalid cursor")
)

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string][]Entry
	nextID  uint64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[string][]Entry),
	}
}

func (s *MemoryStore) Append(ctx context.Context, sessionKey string, entries ...Entry) error {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	history := s.entries[key]
	for _, entry := range entries {
		normalized := cloneEntry(entry)
		normalized.SessionKey = key
		if strings.TrimSpace(normalized.ID) == "" {
			normalized.ID = s.nextEntryIDLocked()
		}
		if normalized.CreatedAt.IsZero() {
			normalized.CreatedAt = now
		}
		history = append(history, normalized)
	}
	s.entries[key] = history
	return nil
}

func (s *MemoryStore) List(ctx context.Context, sessionKey string, opts ListOptions) (ListResult, error) {
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

	s.mu.RLock()
	defer s.mu.RUnlock()

	history := s.entries[key]
	if len(history) == 0 || offset >= len(history) {
		return ListResult{}, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = len(history) - offset
	}

	end := offset + limit
	if end > len(history) {
		end = len(history)
	}

	entries := make([]Entry, 0, end-offset)
	for i := offset; i < end; i++ {
		entries = append(entries, cloneEntry(history[len(history)-1-i]))
	}

	var nextCursor string
	if end < len(history) {
		nextCursor = strconv.Itoa(end)
	}

	return ListResult{
		Entries:    entries,
		NextCursor: nextCursor,
	}, nil
}

func (s *MemoryStore) Clear(ctx context.Context, sessionKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return ErrEmptySessionKey
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
}

func (s *MemoryStore) nextEntryIDLocked() string {
	s.nextID++
	return "chat_" + strconv.FormatUint(s.nextID, 10)
}

func parseCursor(cursor string) (int, error) {
	trimmed := strings.TrimSpace(cursor)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, ErrInvalidCursor
	}
	return offset, nil
}

func cloneEntry(entry Entry) Entry {
	cloned := entry
	if len(entry.Metadata) > 0 {
		cloned.Metadata = make(map[string]string, len(entry.Metadata))
		for k, v := range entry.Metadata {
			cloned.Metadata[k] = v
		}
	}
	return cloned
}
