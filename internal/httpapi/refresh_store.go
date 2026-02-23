package httpapi

import (
	"sync"
	"time"
)

type refreshStore struct {
	mu    sync.Mutex
	items map[string]time.Time
}

func newRefreshStore() *refreshStore {
	return &refreshStore{
		items: make(map[string]time.Time),
	}
}

func (s *refreshStore) put(jti string, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now())
	s.items[jti] = expiresAt
}

func (s *refreshStore) consume(jti string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.cleanupExpiredLocked(now)
	expiresAt, ok := s.items[jti]
	if !ok {
		return false
	}
	if !expiresAt.After(now) {
		delete(s.items, jti)
		return false
	}
	delete(s.items, jti)
	return true
}

func (s *refreshStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]time.Time)
}

func (s *refreshStore) cleanupExpiredLocked(now time.Time) {
	for jti, expiresAt := range s.items {
		if !expiresAt.After(now) {
			delete(s.items, jti)
		}
	}
}
