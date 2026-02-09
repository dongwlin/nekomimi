package history

import (
	"strings"
	"sync"

	"github.com/dongwlin/nekomimi/internal/llm/model"
)

type MemoryStore struct {
	mu        sync.RWMutex
	history   map[string][]model.Message
	maxRounds int
}

func NewMemoryStore(maxRounds int) *MemoryStore {
	return &MemoryStore{
		history:   make(map[string][]model.Message),
		maxRounds: maxRounds,
	}
}

func (s *MemoryStore) Snapshot(sessionKey string) []model.Message {
	if strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	history, ok := s.history[sessionKey]
	if !ok || len(history) == 0 {
		return nil
	}
	copied := make([]model.Message, len(history))
	copy(copied, history)
	return copied
}

func (s *MemoryStore) Append(sessionKey string, messages ...model.Message) {
	if strings.TrimSpace(sessionKey) == "" || len(messages) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.history[sessionKey]
	history = append(history, messages...)
	maxMessages := s.maxRounds * 2
	if s.maxRounds > 0 && len(history) > maxMessages {
		history = history[len(history)-maxMessages:]
	}
	s.history[sessionKey] = history
}

func (s *MemoryStore) Replace(sessionKey string, messages []model.Message) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(messages) == 0 {
		delete(s.history, sessionKey)
		return
	}
	copied := make([]model.Message, len(messages))
	copy(copied, messages)
	s.history[sessionKey] = copied
}

func (s *MemoryStore) Clear(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.history, sessionKey)
}

func (s *MemoryStore) Len(sessionKey string) int {
	if strings.TrimSpace(sessionKey) == "" {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.history[sessionKey])
}

