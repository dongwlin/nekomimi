package llm

import (
	"strings"
	"sync"
	"time"
)

type sessionState struct {
	mu        sync.RWMutex
	immersive map[string]bool
	stats     map[string]*sessionUsageStats
}

func newSessionState() *sessionState {
	return &sessionState{
		stats: make(map[string]*sessionUsageStats),
	}
}

func (s *sessionState) SetImmersive(sessionKey string, enabled bool) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.immersive == nil {
		s.immersive = make(map[string]bool)
	}
	if !enabled {
		delete(s.immersive, sessionKey)
		return
	}
	s.immersive[sessionKey] = true
}

func (s *sessionState) IsImmersive(sessionKey string) bool {
	if strings.TrimSpace(sessionKey) == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.immersive == nil {
		return false
	}
	return s.immersive[sessionKey]
}

func (s *sessionState) ensureStarted(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stats == nil {
		s.stats = make(map[string]*sessionUsageStats)
	}
	stats, ok := s.stats[sessionKey]
	if !ok {
		s.stats[sessionKey] = &sessionUsageStats{startedAt: time.Now()}
		return
	}
	if stats.startedAt.IsZero() {
		stats.startedAt = time.Now()
	}
}

func (s *sessionState) nextCausalSeq(sessionKey string) int64 {
	session := strings.TrimSpace(sessionKey)
	if session == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stats == nil {
		s.stats = make(map[string]*sessionUsageStats)
	}
	stats, ok := s.stats[session]
	if !ok {
		stats = &sessionUsageStats{startedAt: time.Now()}
		s.stats[session] = stats
	}
	if stats.startedAt.IsZero() {
		stats.startedAt = time.Now()
	}
	stats.causalSeq++
	return stats.causalSeq
}

func (s *sessionState) incrementContextTrimCount(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stats == nil {
		s.stats = make(map[string]*sessionUsageStats)
	}
	stats, ok := s.stats[sessionKey]
	if !ok {
		stats = &sessionUsageStats{startedAt: time.Now()}
		s.stats[sessionKey] = stats
	}
	if stats.startedAt.IsZero() {
		stats.startedAt = time.Now()
	}
	stats.contextTrimCount++
}

func (s *sessionState) clearStats(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stats == nil {
		return
	}
	delete(s.stats, sessionKey)
}

func (s *sessionState) snapshot(sessionKey string) (startedAt time.Time, contextTrimCount int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.stats == nil {
		return time.Time{}, 0
	}
	stats := s.stats[sessionKey]
	if stats == nil {
		return time.Time{}, 0
	}
	return stats.startedAt, stats.contextTrimCount
}
