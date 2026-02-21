package llm

import (
	"context"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/summarizer"
	"github.com/dongwlin/nekomimi/internal/llm/token"
)

type SessionContextUsage struct {
	UsedTokens           int
	MaxTokens            int
	UsagePercent         float64
	MessageCount         int
	SessionStartedAt     time.Time
	HistoryCompressCount int
	ContextCompressCount int
	TotalCompressCount   int
}

func (m *Manager) historySnapshot(sessionKey string) []Message {
	if strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	return m.historyStore.Snapshot(sessionKey)
}

func (m *Manager) appendHistory(sessionKey, userContent, assistantReply string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	userContent = strings.TrimSpace(userContent)
	assistantReply = strings.TrimSpace(assistantReply)
	if userContent == "" || assistantReply == "" {
		return
	}
	m.ensureSessionStarted(sessionKey)
	m.historyStore.Append(
		sessionKey,
		Message{Role: "user", Content: userContent},
		Message{Role: "assistant", Content: assistantReply},
	)
}

// AppendTurn appends a completed user-assistant turn into session history.
// userInput/speaker are normalized using the same formatter as regular replies.
func (m *Manager) AppendTurn(sessionKey, userInput, speaker, assistantReply string) {
	userContent := formatUserContent(userInput, speaker)
	m.appendHistory(sessionKey, userContent, assistantReply)
}

func (m *Manager) compressHistoryIfNeeded(ctx context.Context, provider, model, sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" || m.historyMax <= 0 {
		return
	}
	history := m.historyStore.Snapshot(sessionKey)
	if len(history) < m.historyMax*2 {
		return
	}
	oldRounds := m.historyMax / 2
	if oldRounds < 1 {
		oldRounds = 1
	}
	oldMsgCount := oldRounds * 2
	if len(history) <= oldMsgCount {
		return
	}
	summarySrc := make([]Message, oldMsgCount)
	copy(summarySrc, history[:oldMsgCount])

	summary := m.summarizeWithProvider(ctx, provider, model, summarizer.ModeFull, summarySrc, 600)
	if strings.TrimSpace(summary) == "" {
		return
	}
	summaryMsg := Message{
		Role:    "system",
		Content: "对话摘要（历史压缩）: " + summary,
	}

	latest := m.historyStore.Snapshot(sessionKey)
	if len(latest) < m.historyMax*2 || len(latest) <= oldMsgCount {
		return
	}
	tail := make([]Message, len(latest)-oldMsgCount)
	copy(tail, latest[oldMsgCount:])
	compressed := make([]Message, 0, len(tail)+1)
	compressed = append(compressed, summaryMsg)
	compressed = append(compressed, tail...)
	m.historyStore.Replace(sessionKey, compressed)
	m.incrementHistoryCompressCount(sessionKey)
}

func (m *Manager) ClearHistory(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.historyStore.Clear(sessionKey)
	m.clearSessionStats(sessionKey)
}

func (m *Manager) SessionContextUsage(sessionKey string) SessionContextUsage {
	if strings.TrimSpace(sessionKey) == "" {
		return SessionContextUsage{}
	}
	history := m.historySnapshot(sessionKey)
	m.mu.RLock()
	systemPrompt := m.systemPrompt
	contextMax := m.contextMax
	stats := m.sessionStats[sessionKey]
	m.mu.RUnlock()

	used := token.EstimateContextTokens(systemPrompt, history)
	percent := 0.0
	startedAt := time.Time{}
	historyCompressCount := 0
	contextCompressCount := 0
	if stats != nil {
		startedAt = stats.startedAt
		historyCompressCount = stats.historyCompressCount
		contextCompressCount = stats.contextCompressCount
	}
	if contextMax > 0 {
		percent = float64(used) * 100 / float64(contextMax)
	}
	return SessionContextUsage{
		UsedTokens:           used,
		MaxTokens:            contextMax,
		UsagePercent:         percent,
		MessageCount:         len(history),
		SessionStartedAt:     startedAt,
		HistoryCompressCount: historyCompressCount,
		ContextCompressCount: contextCompressCount,
		TotalCompressCount:   historyCompressCount + contextCompressCount,
	}
}

func (m *Manager) ensureSessionStarted(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionStats == nil {
		m.sessionStats = make(map[string]*sessionUsageStats)
	}
	stats, ok := m.sessionStats[sessionKey]
	if !ok {
		m.sessionStats[sessionKey] = &sessionUsageStats{
			startedAt: time.Now(),
		}
		return
	}
	if stats.startedAt.IsZero() {
		stats.startedAt = time.Now()
	}
}

func (m *Manager) incrementHistoryCompressCount(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionStats == nil {
		m.sessionStats = make(map[string]*sessionUsageStats)
	}
	stats, ok := m.sessionStats[sessionKey]
	if !ok {
		stats = &sessionUsageStats{startedAt: time.Now()}
		m.sessionStats[sessionKey] = stats
	}
	if stats.startedAt.IsZero() {
		stats.startedAt = time.Now()
	}
	stats.historyCompressCount++
}

func (m *Manager) incrementContextCompressCount(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionStats == nil {
		m.sessionStats = make(map[string]*sessionUsageStats)
	}
	stats, ok := m.sessionStats[sessionKey]
	if !ok {
		stats = &sessionUsageStats{startedAt: time.Now()}
		m.sessionStats[sessionKey] = stats
	}
	if stats.startedAt.IsZero() {
		stats.startedAt = time.Now()
	}
	stats.contextCompressCount++
}

func (m *Manager) clearSessionStats(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionStats == nil {
		return
	}
	delete(m.sessionStats, sessionKey)
}
