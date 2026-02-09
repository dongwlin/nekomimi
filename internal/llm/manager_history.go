package llm

import (
	"context"
	"strings"
)

func (m *Manager) historySnapshot(sessionKey string) []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	history, ok := m.history[sessionKey]
	if !ok || len(history) == 0 {
		return nil
	}
	copied := make([]Message, len(history))
	copy(copied, history)
	return copied
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.history == nil {
		m.history = make(map[string][]Message)
	}
	history := m.history[sessionKey]
	history = append(history, Message{Role: "user", Content: userContent})
	history = append(history, Message{Role: "assistant", Content: assistantReply})
	maxMessages := m.historyMax * 2
	if m.historyMax > 0 && len(history) > maxMessages {
		history = history[len(history)-maxMessages:]
	}
	m.history[sessionKey] = history
}

func (m *Manager) compressHistoryIfNeeded(ctx context.Context, provider, model, sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" || m.historyMax <= 0 {
		return
	}
	m.mu.RLock()
	history, ok := m.history[sessionKey]
	if !ok || len(history) < m.historyMax*2 {
		m.mu.RUnlock()
		return
	}
	oldRounds := m.historyMax / 2
	if oldRounds < 1 {
		oldRounds = 1
	}
	oldMsgCount := oldRounds * 2
	if len(history) <= oldMsgCount {
		m.mu.RUnlock()
		return
	}
	summarySrc := make([]Message, oldMsgCount)
	copy(summarySrc, history[:oldMsgCount])
	m.mu.RUnlock()

	summary := m.buildSummaryWithModel(ctx, provider, model, summarySrc)
	if strings.TrimSpace(summary) == "" {
		summary = buildSummary(summarySrc, 600)
	}
	if strings.TrimSpace(summary) == "" {
		return
	}
	summaryMsg := Message{
		Role:    "system",
		Content: "对话摘要（历史压缩）: " + summary,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	history, ok = m.history[sessionKey]
	if !ok || len(history) < m.historyMax*2 || len(history) <= oldMsgCount {
		return
	}
	tail := make([]Message, len(history)-oldMsgCount)
	copy(tail, history[oldMsgCount:])
	compressed := make([]Message, 0, len(tail)+1)
	compressed = append(compressed, summaryMsg)
	compressed = append(compressed, tail...)
	m.history[sessionKey] = compressed
}

func (m *Manager) ClearHistory(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.history) == 0 {
		return
	}
	delete(m.history, sessionKey)
}
