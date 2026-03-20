package llm

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/ctxasm"
	"github.com/dongwlin/nekomimi/internal/diary"
	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/rs/zerolog/log"
)

const (
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
	assembler        *ctxasm.Assembler
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

	cutoffSeq, ok := m.appendUserEventFormatted(session, userContent, time.Now(), nil)
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
	return m.AppendUserEventWithMetadataAt(sessionKey, userInput, speaker, eventTime, nil)
}

// AppendUserEventWithMetadataAt appends one user atomic event with explicit event
// time and extra metadata for downstream business logic.
func (m *Manager) AppendUserEventWithMetadataAt(sessionKey, userInput, speaker string, eventTime time.Time, metadata map[string]string) (int64, bool) {
	at := normalizeEventTime(eventTime)
	content := formatUserContentAt(userInput, speaker, at)
	return m.appendUserEventFormatted(sessionKey, content, at, metadata)
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

func (m *Manager) appendUserEventFormatted(sessionKey, content string, eventTime time.Time, extraMetadata map[string]string) (int64, bool) {
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
	metadata := normalizeEventMetadata(extraMetadata)
	metadata[MetadataEventTime] = at.Format(time.RFC3339Nano)
	metadata[MetadataCausalSeq] = strconv.FormatInt(seq, 10)

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
		MetadataEventTime: at.Format(time.RFC3339Nano),
		MetadataCausalSeq: strconv.FormatInt(seq, 10),
	}
	if replyToCutoffSeq > 0 {
		metadata[MetadataReplyToCutoffSeq] = strconv.FormatInt(replyToCutoffSeq, 10)
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

func normalizeEventMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return make(map[string]string, 2)
	}
	normalized := make(map[string]string, len(metadata)+2)
	for key, value := range metadata {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		normalized[trimmedKey] = strings.TrimSpace(value)
	}
	return normalized
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
	chatStore := m.chatStore
	diaryStore := m.diaryStore
	m.mu.RUnlock()
	if chatStore != nil {
		if err := chatStore.Clear(context.Background(), session); err != nil {
			log.Warn().Err(err).Str("session", session).Msg("clear chat history failed")
		}
	}
	if diaryStore != nil {
		if err := diaryStore.Clear(context.Background(), session); err != nil {
			log.Warn().Err(err).Str("session", session).Msg("clear diary history failed")
		}
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
		assembled, err := state.assembler.Assemble(context.Background(), ctxasm.Request{
			SessionKey: session,
		})
		if err == nil {
			usage.AssembledChars = assembled.TotalChars
			usage.TruncatedBlockCount = countTruncatedBlocks(assembled.Blocks)
			assembledContent := renderAssembledBlocks(assembled.Blocks)
			if strings.TrimSpace(assembledContent) == "" {
				usage.UsedTokens = estimateContextTokens(state.systemPrompt, nil)
			} else {
				usage.UsedTokens = estimateContextTokens(state.systemPrompt, []model.Message{
					{Role: "user", Content: assembledContent},
				})
			}
		}
	}
	if usage.UsedTokens == 0 {
		usage.UsedTokens = estimateContextTokens(state.systemPrompt, nil)
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
		state.recentChatLimit = ctxasm.DefaultRecentChatLimit
	}
	if state.recentDiaryLimit <= 0 {
		state.recentDiaryLimit = ctxasm.DefaultRecentDiaryLimit
	}
	state.startedAt, state.contextTrimCount = m.sessions.snapshot(sessionKey)
	return state
}

func countTruncatedBlocks(blocks []ctxasm.Block) int {
	count := 0
	for _, block := range blocks {
		if block.Truncated {
			count++
		}
	}
	return count
}
