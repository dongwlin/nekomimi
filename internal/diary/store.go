package diary

import (
	"context"
	"time"
)

// Entry is a persistent memory note written by the assistant.
type Entry struct {
	ID         string
	SessionKey string
	Content    string
	Author     string
	Tags       []string
	CreatedAt  time.Time
	Metadata   map[string]string
}

// ListOptions defines stable pagination inputs.
type ListOptions struct {
	Limit  int
	Cursor string
}

// ListResult defines stable pagination outputs.
type ListResult struct {
	Entries    []Entry
	NextCursor string
}

// Store is the contract frozen in package-0 for diary storage.
type Store interface {
	Write(ctx context.Context, sessionKey string, entry Entry) (Entry, error)
	List(ctx context.Context, sessionKey string, opts ListOptions) (ListResult, error)
	Clear(ctx context.Context, sessionKey string) error
}
