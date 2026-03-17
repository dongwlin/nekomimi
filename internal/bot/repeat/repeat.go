package repeat

import (
	"strings"
	"sync"
	"time"

	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/bot/session"
	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const (
	defaultMinBatchWaitMS = 600
	defaultMaxBatchWaitMS = 3000
	defaultMaxBatchSize   = 15

	defaultHistoryScanLimit = 64
)

type Engine struct {
	mu        sync.Mutex
	cfg       config.RepeatConfig
	history   HistoryStore
	recorder  AssistantDeliveryRecorder
	collector *metrics.Collector
	sessions  map[string]*sessionState
	overrides map[string]sessionMode
}

type HistoryStore interface {
	ListChatEvents(sessionKey string, opts chatlog.ListOptions) (chatlog.ListResult, error)
	AppendAssistantEventWithSpeakerAt(sessionKey, assistantReply, speaker string, replyToCutoffSeq int64, eventTime time.Time) bool
}

type AssistantDeliveryRecorder interface {
	RecordAssistantDelivered(sessionKey, text, speaker string)
}

type sessionMode uint8

const (
	sessionModeDefault sessionMode = iota
	sessionModeOn
	sessionModeOff
)

type sessionState struct {
	mu sync.Mutex

	lastRoundText      string
	lastRoundStartSeq  int64
	lastRoundBotJoined bool
}

type SendFunc func(payload interface{}) message.ID

type repeatDecision struct {
	Text           string
	NormalizedText string
	RunCount       int
	DistinctUsers  int
	RoundStartSeq  int64
	LatestSeq      int64
	Reason         string
}

func NewEngine(cfg config.RepeatConfig, history HistoryStore, recorder AssistantDeliveryRecorder) *Engine {
	return &Engine{
		history:   history,
		recorder:  recorder,
		cfg:       normalizeConfig(cfg),
		sessions:  make(map[string]*sessionState),
		overrides: make(map[string]sessionMode),
	}
}

func (e *Engine) SetMetricsCollector(collector *metrics.Collector) {
	if e == nil {
		return
	}
	e.collector = collector
}

func (e *Engine) ReloadConfig(cfg config.RepeatConfig) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = normalizeConfig(cfg)
}

func (e *Engine) SetEnabled(sessionKey string, enabled bool) {
	if e == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	mode := sessionModeOff
	if enabled {
		mode = sessionModeOn
	}
	if enabled == e.cfg.Enabled {
		delete(e.overrides, sessionKey)
		return
	}
	e.overrides[sessionKey] = mode
}

func (e *Engine) IsEnabled(sessionKey string) bool {
	if e == nil || strings.TrimSpace(sessionKey) == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	switch e.overrides[sessionKey] {
	case sessionModeOn:
		return true
	case sessionModeOff:
		return false
	default:
		return e.cfg.Enabled
	}
}

func (e *Engine) Clear(sessionKey string) {
	if e == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	state := e.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.lastRoundText = ""
	state.lastRoundStartSeq = 0
	state.lastRoundBotJoined = false
}

func (e *Engine) TryRepeat(ctx *zero.Ctx, sessionKey string, meta immersivepkg.AmbientMessageMeta, assistantSpeaker string) bool {
	return e.tryRepeatWithSend(sessionKey, meta, assistantSpeaker, e.captureSendFunc(ctx))
}

func (e *Engine) tryRepeatWithSend(sessionKey string, meta immersivepkg.AmbientMessageMeta, assistantSpeaker string, sendFn SendFunc) bool {
	if e == nil || !e.IsEnabled(sessionKey) {
		return false
	}

	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || strings.TrimSpace(meta.Text) == "" {
		return false
	}
	if meta.IsPrivate {
		log.Info().
			Str("session", sessionKey).
			Bool("is_private", true).
			Msg("repeat miss: private_session")
		return false
	}

	state := e.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()

	decision, err := e.evaluateLatestRound(sessionKey)
	if err != nil {
		log.Warn().
			Err(err).
			Str("session", sessionKey).
			Msg("repeat evaluation failed")
		return false
	}

	evaluationLog := log.Info().
		Str("session", sessionKey).
		Bool("is_private", meta.IsPrivate).
		Str("normalized_text", decision.NormalizedText).
		Int("run_count", decision.RunCount).
		Int("distinct_users", decision.DistinctUsers).
		Int64("round_start_seq", decision.RoundStartSeq).
		Int64("latest_seq", decision.LatestSeq)
	evaluationLog.Msg("repeat evaluation started")

	if decision.Reason != "" {
		log.Info().
			Str("session", sessionKey).
			Str("normalized_text", decision.NormalizedText).
			Int("run_count", decision.RunCount).
			Int("distinct_users", decision.DistinctUsers).
			Int64("round_start_seq", decision.RoundStartSeq).
			Int64("latest_seq", decision.LatestSeq).
			Str("reason", decision.Reason).
			Msg("repeat miss")
		return false
	}

	if state.lastRoundBotJoined && state.lastRoundText == decision.NormalizedText && state.lastRoundStartSeq == decision.RoundStartSeq {
		log.Info().
			Str("session", sessionKey).
			Str("normalized_text", decision.NormalizedText).
			Int64("round_start_seq", decision.RoundStartSeq).
			Int64("latest_seq", decision.LatestSeq).
			Str("reason", "already_joined_round").
			Msg("repeat miss")
		return false
	}

	if sendFn == nil {
		log.Warn().
			Str("session", sessionKey).
			Str("normalized_text", decision.NormalizedText).
			Msg("repeat send function missing, skipping outbound send")
		return false
	}

	sendFn(decision.Text)

	now := meta.At
	if now.IsZero() {
		now = time.Now()
	}
	if e.history != nil {
		_ = e.history.AppendAssistantEventWithSpeakerAt(sessionKey, decision.Text, assistantSpeaker, decision.LatestSeq, now)
	}
	if e.recorder != nil {
		e.recorder.RecordAssistantDelivered(sessionKey, decision.Text, assistantSpeaker)
	}

	state.lastRoundText = decision.NormalizedText
	state.lastRoundStartSeq = decision.RoundStartSeq
	state.lastRoundBotJoined = true

	log.Info().
		Str("session", sessionKey).
		Str("repeat_text", decision.Text).
		Str("normalized_text", decision.NormalizedText).
		Int("repeat_count", decision.RunCount).
		Int("repeat_participants", decision.DistinctUsers).
		Int64("round_start_seq", decision.RoundStartSeq).
		Int64("latest_seq", decision.LatestSeq).
		Msg("repeat hit")
	return true
}

func (e *Engine) evaluateLatestRound(sessionKey string) (repeatDecision, error) {
	if e == nil || e.history == nil {
		return repeatDecision{Reason: "history_unavailable"}, nil
	}

	result, err := e.history.ListChatEvents(sessionKey, chatlog.ListOptions{Limit: defaultHistoryScanLimit})
	if err != nil {
		return repeatDecision{}, err
	}
	return buildRepeatDecision(result.Entries), nil
}

func buildRepeatDecision(entries []chatlog.Entry) repeatDecision {
	decision := repeatDecision{}
	participants := make(map[string]struct{})

	for _, entry := range entries {
		if entry.Role != chatlog.RoleUser {
			continue
		}
		rawText := strings.TrimSpace(entry.Metadata[llm.MetadataRawText])
		if rawText == "" {
			continue
		}
		normalized := normalizeRepeatText(rawText)
		if normalized == "" {
			continue
		}
		seq := parseCausalSeq(entry.Metadata)
		if decision.NormalizedText == "" {
			decision.Text = rawText
			decision.NormalizedText = normalized
			decision.RoundStartSeq = seq
			decision.LatestSeq = seq
		}
		if normalized != decision.NormalizedText {
			break
		}
		decision.RunCount++
		if seq > 0 {
			decision.RoundStartSeq = seq
		}
		if speaker := strings.TrimSpace(entry.Metadata[llm.MetadataSpeakerLabel]); speaker != "" {
			participants[speaker] = struct{}{}
		}
	}

	if decision.NormalizedText == "" {
		decision.Reason = "empty_tail"
		return decision
	}

	decision.DistinctUsers = len(participants)
	switch {
	case decision.RunCount < 2:
		decision.Reason = "insufficient_run_length"
	case decision.DistinctUsers < 2:
		decision.Reason = "insufficient_distinct_users"
	}
	return decision
}

func parseCausalSeq(metadata map[string]string) int64 {
	if len(metadata) == 0 {
		return 0
	}
	value := strings.TrimSpace(metadata[llm.MetadataCausalSeq])
	if value == "" {
		return 0
	}
	var seq int64
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0
		}
		seq = seq*10 + int64(ch-'0')
	}
	return seq
}

func (e *Engine) currentConfig() config.RepeatConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

func (e *Engine) session(sessionKey string) *sessionState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sessions == nil {
		e.sessions = make(map[string]*sessionState)
	}
	if state, ok := e.sessions[sessionKey]; ok {
		return state
	}
	state := &sessionState{}
	e.sessions[sessionKey] = state
	return state
}

func (e *Engine) captureSendFunc(ctx *zero.Ctx) SendFunc {
	if e == nil || ctx == nil {
		return nil
	}
	return func(payload interface{}) message.ID {
		return e.sendTracked(ctx, payload)
	}
}

func (e *Engine) sendTracked(ctx *zero.Ctx, payload interface{}) message.ID {
	var messageID message.ID
	if ctx == nil {
		return messageID
	}

	messageID = ctx.Send(payload)
	collector := e.collector
	if collector == nil {
		return messageID
	}

	if err := collector.RecordOutbound(metrics.OutboundTypeKeys(payload), messageID.ID() != 0, session.Key(ctx)); err != nil {
		log.Warn().Err(err).Msg("record repeat outbound metrics failed")
	}
	return messageID
}

func normalizeConfig(cfg config.RepeatConfig) config.RepeatConfig {
	if cfg.FlushPolicy.MinBatchWaitMS <= 0 {
		cfg.FlushPolicy.MinBatchWaitMS = defaultMinBatchWaitMS
	}
	if cfg.FlushPolicy.MaxBatchWaitMS <= 0 {
		cfg.FlushPolicy.MaxBatchWaitMS = defaultMaxBatchWaitMS
	}
	if cfg.FlushPolicy.MaxBatchSize <= 0 {
		cfg.FlushPolicy.MaxBatchSize = defaultMaxBatchSize
	}
	return cfg
}

func normalizeRepeatText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}
