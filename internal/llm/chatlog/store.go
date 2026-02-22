package chatlog

import (
	"context"
	"time"
)

// Role is the canonical role set used by chatlog entries.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Entry is a normalized chat message persisted in chatlog storage.
type Entry struct {
	ID         string
	SessionKey string
	Role       Role
	Speaker    string
	Content    string
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

// Store is the contract frozen in package-0 for chat history storage.
type Store interface {
	Append(ctx context.Context, sessionKey string, entries ...Entry) error
	List(ctx context.Context, sessionKey string, opts ListOptions) (ListResult, error)
	Clear(ctx context.Context, sessionKey string) error
}
