package history

import "github.com/dongwlin/nekomimi/internal/llm/model"

type Store interface {
	Snapshot(sessionKey string) []model.Message
	Append(sessionKey string, messages ...model.Message)
	Replace(sessionKey string, messages []model.Message)
	Clear(sessionKey string)
	Len(sessionKey string) int
}

