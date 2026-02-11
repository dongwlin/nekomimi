package llm

import (
	"context"
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm/summarizer"
	"github.com/dongwlin/nekomimi/internal/llm/token"
)

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
	m.historyStore.Append(
		sessionKey,
		Message{Role: "user", Content: userContent},
		Message{Role: "assistant", Content: assistantReply},
	)
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
}

func (m *Manager) ClearHistory(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.historyStore.Clear(sessionKey)
}

func (m *Manager) SessionContextUsage(sessionKey string) (usedTokens int, maxTokens int, usagePercent float64, messageCount int) {
	if strings.TrimSpace(sessionKey) == "" {
		return 0, 0, 0, 0
	}
	history := m.historySnapshot(sessionKey)
	m.mu.RLock()
	systemPrompt := m.systemPrompt
	contextMax := m.contextMax
	m.mu.RUnlock()

	used := token.EstimateContextTokens(systemPrompt, history)
	percent := 0.0
	if contextMax > 0 {
		percent = float64(used) * 100 / float64(contextMax)
	}
	return used, contextMax, percent, len(history)
}
