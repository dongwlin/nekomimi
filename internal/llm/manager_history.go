package llm

import (
	"context"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
	"github.com/dongwlin/nekomimi/internal/llm/token"
	"github.com/rs/zerolog/log"
)

type SessionContextUsage struct {
	UsedTokens          int
	MaxTokens           int
	UsagePercent        float64
	SessionStartedAt    time.Time
	RecentChatCount     int
	RecentChatLimit     int
	RecentDiaryCount    int
	RecentDiaryLimit    int
	AssembledChars      int
	TruncatedBlockCount int
	ContextTrimCount    int
}

type sessionContextUsageState struct {
	chatStore        chatlog.Store
	diaryStore       diary.Store
	assembler        *contextassemble.Assembler
	systemPrompt     string
	contextMax       int
	recentChatLimit  int
	recentDiaryLimit int
	startedAt        time.Time
	contextTrimCount int
}

func (m *Manager) appendHistory(sessionKey, userContent, assistantReply string) {
	session := strings.TrimSpace(sessionKey)
	if session == "" {
		return
	}
	userContent = strings.TrimSpace(userContent)
	assistantReply = strings.TrimSpace(assistantReply)
	if userContent == "" || assistantReply == "" {
		return
	}

	m.ensureSessionStarted(session)
	m.mu.RLock()
	store := m.chatStore
	m.mu.RUnlock()
	if store == nil {
		return
	}

	if err := store.Append(context.Background(), session,
		chatlog.Entry{Role: chatlog.RoleUser, Content: userContent},
		chatlog.Entry{Role: chatlog.RoleAssistant, Content: assistantReply},
	); err != nil {
		log.Warn().Err(err).Str("session", session).Msg("append chat history failed")
	}
}

// AppendTurn appends a completed user-assistant turn into session history.
// userInput/speaker are normalized using the same formatter as regular replies.
func (m *Manager) AppendTurn(sessionKey, userInput, speaker, assistantReply string) {
	userContent := formatUserContent(userInput, speaker)
	m.appendHistory(sessionKey, userContent, assistantReply)
}

func (m *Manager) ClearHistory(sessionKey string) {
	session := strings.TrimSpace(sessionKey)
	if session == "" {
		return
	}
	m.mu.RLock()
	store := m.chatStore
	m.mu.RUnlock()
	if store != nil {
		_ = store.Clear(context.Background(), session)
	}
	m.clearSessionStats(session)
}

func (m *Manager) SessionContextUsage(sessionKey string) SessionContextUsage {
	session := strings.TrimSpace(sessionKey)
	if session == "" {
		return SessionContextUsage{}
	}

	state := m.snapshotSessionContextUsage(session)
	usage := SessionContextUsage{
		MaxTokens:        state.contextMax,
		SessionStartedAt: state.startedAt,
		RecentChatLimit:  state.recentChatLimit,
		RecentDiaryLimit: state.recentDiaryLimit,
		ContextTrimCount: state.contextTrimCount,
	}

	if state.chatStore != nil {
		result, err := state.chatStore.List(context.Background(), session, chatlog.ListOptions{Limit: state.recentChatLimit})
		if err == nil {
			usage.RecentChatCount = len(result.Entries)
		}
	}
	if state.diaryStore != nil {
		result, err := state.diaryStore.List(context.Background(), session, diary.ListOptions{Limit: state.recentDiaryLimit})
		if err == nil {
			usage.RecentDiaryCount = len(result.Entries)
		}
	}

	if state.assembler != nil {
		assembled, err := state.assembler.Assemble(context.Background(), contextassemble.Request{
			SessionKey: session,
		})
		if err == nil {
			usage.AssembledChars = assembled.TotalChars
			usage.TruncatedBlockCount = countTruncatedBlocks(assembled.Blocks)
			assembledContent := renderUsageAssembledBlocks(assembled.Blocks)
			if strings.TrimSpace(assembledContent) == "" {
				usage.UsedTokens = token.EstimateContextTokens(state.systemPrompt, nil)
			} else {
				usage.UsedTokens = token.EstimateContextTokens(state.systemPrompt, []Message{
					{Role: "user", Content: assembledContent},
				})
			}
		}
	}
	if usage.UsedTokens == 0 {
		usage.UsedTokens = token.EstimateContextTokens(state.systemPrompt, nil)
	}
	if usage.MaxTokens > 0 {
		usage.UsagePercent = float64(usage.UsedTokens) * 100 / float64(usage.MaxTokens)
	}
	return usage
}

func (m *Manager) snapshotSessionContextUsage(sessionKey string) sessionContextUsageState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state := sessionContextUsageState{
		chatStore:        m.chatStore,
		diaryStore:       m.diaryStore,
		assembler:        m.contextAssembler,
		systemPrompt:     m.systemPrompt,
		contextMax:       m.contextMax,
		recentChatLimit:  m.recentChatLimit,
		recentDiaryLimit: m.recentDiaryLimit,
	}
	if state.recentChatLimit <= 0 {
		state.recentChatLimit = contextassemble.DefaultRecentChatLimit
	}
	if state.recentDiaryLimit <= 0 {
		state.recentDiaryLimit = contextassemble.DefaultRecentDiaryLimit
	}
	if stats := m.sessionStats[sessionKey]; stats != nil {
		state.startedAt = stats.startedAt
		state.contextTrimCount = stats.contextTrimCount
	}
	return state
}

func countTruncatedBlocks(blocks []contextassemble.Block) int {
	count := 0
	for _, block := range blocks {
		if block.Truncated {
			count++
		}
	}
	return count
}

func renderUsageAssembledBlocks(blocks []contextassemble.Block) string {
	if len(blocks) == 0 {
		return ""
	}
	filtered := make([]contextassemble.Block, 0, len(blocks))
	for _, block := range blocks {
		if block.Name == contextassemble.BlockCurrentInput && strings.TrimSpace(block.Content) == "" {
			continue
		}
		filtered = append(filtered, block)
	}
	return renderAssembledBlocks(filtered)
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

func (m *Manager) incrementContextTrimCount(sessionKey string) {
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
	stats.contextTrimCount++
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
