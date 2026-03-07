package llm

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
	"github.com/dongwlin/nekomimi/internal/llm/token"
	"github.com/rs/zerolog/log"
)

const (
	metaEventTime         = "event_time"
	metaCausalSeq         = "causal_seq"
	metaReplyToCutoffSeq  = "reply_to_cutoff_seq"
	defaultAssistantLabel = "name=assistant"
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

	cutoffSeq, ok := m.appendUserEventFormatted(session, userContent, time.Now())
	if !ok {
		return
	}
	_ = m.appendAssistantEventFormatted(session, assistantReply, "", time.Now(), cutoffSeq)
}

// AppendTurn appends a completed user-assistant turn into session history.
// userInput/speaker are normalized using the same formatter as regular replies.
func (m *Manager) AppendTurn(sessionKey, userInput, speaker, assistantReply string) {
	userContent := formatUserContent(userInput, speaker)
	m.appendHistory(sessionKey, userContent, assistantReply)
}

// AppendUserEvent appends one user atomic event and returns its causal sequence.
func (m *Manager) AppendUserEvent(sessionKey, userInput, speaker string) (int64, bool) {
	return m.AppendUserEventAt(sessionKey, userInput, speaker, time.Now())
}

// AppendUserEventAt appends one user atomic event with explicit event time.
func (m *Manager) AppendUserEventAt(sessionKey, userInput, speaker string, eventTime time.Time) (int64, bool) {
	at := normalizeEventTime(eventTime)
	content := formatUserContentAt(userInput, speaker, at)
	return m.appendUserEventFormatted(sessionKey, content, at)
}

// AppendAssistantEvent appends one assistant atomic event with a causal cutoff anchor.
func (m *Manager) AppendAssistantEvent(sessionKey, assistantReply string, replyToCutoffSeq int64) bool {
	return m.AppendAssistantEventAt(sessionKey, assistantReply, replyToCutoffSeq, time.Now())
}

// AppendAssistantEventAt appends one assistant atomic event with explicit event time.
func (m *Manager) AppendAssistantEventAt(sessionKey, assistantReply string, replyToCutoffSeq int64, eventTime time.Time) bool {
	return m.appendAssistantEventFormatted(sessionKey, assistantReply, "", eventTime, replyToCutoffSeq)
}

// AppendAssistantEventWithSpeakerAt appends one assistant event with an explicit speaker label.
func (m *Manager) AppendAssistantEventWithSpeakerAt(sessionKey, assistantReply, speaker string, replyToCutoffSeq int64, eventTime time.Time) bool {
	return m.appendAssistantEventFormatted(sessionKey, assistantReply, speaker, eventTime, replyToCutoffSeq)
}

func (m *Manager) appendUserEventFormatted(sessionKey, content string, eventTime time.Time) (int64, bool) {
	session := strings.TrimSpace(sessionKey)
	content = strings.TrimSpace(content)
	if session == "" || content == "" {
		return 0, false
	}

	m.sessions.ensureStarted(session)
	m.mu.RLock()
	store := m.chatStore
	m.mu.RUnlock()
	if store == nil {
		return 0, false
	}

	at := normalizeEventTime(eventTime)
	seq := m.sessions.nextCausalSeq(session)
	metadata := map[string]string{
		metaEventTime: at.Format(time.RFC3339Nano),
		metaCausalSeq: strconv.FormatInt(seq, 10),
	}

	if err := store.Append(context.Background(), session, chatlog.Entry{
		Role:      chatlog.RoleUser,
		Content:   content,
		CreatedAt: at,
		Metadata:  metadata,
	}); err != nil {
		log.Warn().Err(err).Str("session", session).Msg("append user chat event failed")
		return 0, false
	}
	return seq, true
}

func (m *Manager) appendAssistantEventFormatted(sessionKey, assistantReply, speaker string, eventTime time.Time, replyToCutoffSeq int64) bool {
	session := strings.TrimSpace(sessionKey)
	reply := strings.TrimSpace(assistantReply)
	if session == "" || reply == "" {
		return false
	}

	m.sessions.ensureStarted(session)
	m.mu.RLock()
	store := m.chatStore
	assistantSpeaker := strings.TrimSpace(speaker)
	if assistantSpeaker == "" {
		assistantSpeaker = strings.TrimSpace(m.current.assistantSpeaker)
	}
	m.mu.RUnlock()
	if store == nil {
		return false
	}
	if assistantSpeaker == "" {
		assistantSpeaker = defaultAssistantLabel
	}

	at := normalizeEventTime(eventTime)
	assistantContent := formatUserContentAt(reply, assistantSpeaker, at)
	if strings.TrimSpace(assistantContent) == "" {
		return false
	}

	seq := m.sessions.nextCausalSeq(session)
	metadata := map[string]string{
		metaEventTime: at.Format(time.RFC3339Nano),
		metaCausalSeq: strconv.FormatInt(seq, 10),
	}
	if replyToCutoffSeq > 0 {
		metadata[metaReplyToCutoffSeq] = strconv.FormatInt(replyToCutoffSeq, 10)
	}

	if err := store.Append(context.Background(), session, chatlog.Entry{
		Role:      chatlog.RoleAssistant,
		Content:   assistantContent,
		CreatedAt: at,
		Metadata:  metadata,
	}); err != nil {
		log.Warn().Err(err).Str("session", session).Msg("append assistant chat event failed")
		return false
	}
	return true
}

func normalizeEventTime(at time.Time) time.Time {
	if at.IsZero() {
		at = time.Now()
	}
	return at
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
	m.sessions.clearStats(session)
}

// ListChatEvents returns raw chatlog events for observability/debugging.
func (m *Manager) ListChatEvents(sessionKey string, opts chatlog.ListOptions) (chatlog.ListResult, error) {
	session := strings.TrimSpace(sessionKey)
	if session == "" {
		return chatlog.ListResult{}, chatlog.ErrEmptySessionKey
	}
	m.mu.RLock()
	store := m.chatStore
	m.mu.RUnlock()
	if store == nil {
		return chatlog.ListResult{}, nil
	}
	return store.List(context.Background(), session, opts)
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
		systemPrompt:     m.current.systemPrompt,
		contextMax:       m.current.contextMax,
		recentChatLimit:  m.current.recentChatLimit,
		recentDiaryLimit: m.current.recentDiaryLimit,
	}
	if state.recentChatLimit <= 0 {
		state.recentChatLimit = contextassemble.DefaultRecentChatLimit
	}
	if state.recentDiaryLimit <= 0 {
		state.recentDiaryLimit = contextassemble.DefaultRecentDiaryLimit
	}
	state.startedAt, state.contextTrimCount = m.sessions.snapshot(sessionKey)
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
	return renderAssembledBlocks(blocks)
}
