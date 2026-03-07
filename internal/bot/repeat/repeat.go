package repeat

import (
	"strings"
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/bot/session"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const (
	defaultMinBatchWaitMS = 600
	defaultMaxBatchWaitMS = 3000
	defaultMaxBatchSize   = 15
)

type Engine struct {
	mu        sync.Mutex
	cfg       config.RepeatConfig
	history   HistoryWriter
	collector *metrics.Collector
	sessions  map[string]*sessionState
	overrides map[string]sessionMode
}

type sessionMode uint8

const (
	sessionModeDefault sessionMode = iota
	sessionModeOn
	sessionModeOff
)

type sessionState struct {
	mu             sync.Mutex
	nextBatch      []queuedMessage
	nextBatchChars int
	batchStartTime time.Time
	timer          *time.Timer
	inFlight       bool
	sendFn         SendFunc
	assistantLabel string
}

type queuedMessage struct {
	text    string
	speaker string
	ts      time.Time
	chars   int
}

type flushDecision struct {
	Delay    time.Duration
	Reason   string
	Priority string
}

type SendFunc func(payload interface{}) message.ID

type HistoryWriter interface {
	AppendUserEventAt(sessionKey, userInput, speaker string, eventTime time.Time) (int64, bool)
	AppendAssistantEventWithSpeakerAt(sessionKey, assistantReply, speaker string, replyToCutoffSeq int64, eventTime time.Time) bool
}

func NewEngine(cfg config.RepeatConfig, history HistoryWriter) *Engine {
	return &Engine{
		history:   history,
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

	state.nextBatch = nil
	state.nextBatchChars = 0
	state.batchStartTime = time.Time{}
	state.inFlight = false
	state.sendFn = nil
	state.assistantLabel = ""
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
}

func (e *Engine) Enqueue(ctx *zero.Ctx, sessionKey, text, speaker, assistantSpeaker string, isPrivate bool) bool {
	if e == nil || !e.IsEnabled(sessionKey) {
		return false
	}

	trimmed := strings.TrimSpace(text)
	if strings.TrimSpace(sessionKey) == "" || trimmed == "" {
		return false
	}

	state := e.session(sessionKey)
	now := time.Now()
	msg := queuedMessage{
		text:    trimmed,
		speaker: strings.TrimSpace(speaker),
		ts:      now,
		chars:   len([]rune(trimmed)),
	}

	state.mu.Lock()
	if sendFn := e.captureSendFunc(ctx); sendFn != nil {
		state.sendFn = sendFn
	}
	if trimmedAssistant := strings.TrimSpace(assistantSpeaker); trimmedAssistant != "" {
		state.assistantLabel = trimmedAssistant
	}
	if len(state.nextBatch) == 0 {
		state.batchStartTime = now
	}
	state.nextBatch = append(state.nextBatch, msg)
	state.nextBatchChars += msg.chars

	decision := e.computeFlushDecision(state, now)
	queueLen := len(state.nextBatch)
	queueChars := state.nextBatchChars
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.timer = time.AfterFunc(decision.Delay, func() {
		e.flush(sessionKey)
	})
	state.mu.Unlock()

	log.Info().
		Str("session", sessionKey).
		Bool("is_private", isPrivate).
		Int("queue_len", queueLen).
		Int("queue_chars", queueChars).
		Int64("cooldown_ms", decision.Delay.Milliseconds()).
		Str("scheduler_reason", decision.Reason).
		Str("scheduler_priority", decision.Priority).
		Msg("repeat enqueue scheduled")
	return true
}

func (e *Engine) flush(sessionKey string) {
	state := e.session(sessionKey)
	state.mu.Lock()
	if state.inFlight {
		if len(state.nextBatch) > 0 {
			decision := e.computeFlushDecision(state, time.Now())
			if state.timer != nil {
				state.timer.Stop()
				state.timer = nil
			}
			state.timer = time.AfterFunc(decision.Delay, func() {
				e.flush(sessionKey)
			})
		}
		state.mu.Unlock()
		return
	}
	if len(state.nextBatch) == 0 {
		state.batchStartTime = time.Time{}
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		state.mu.Unlock()
		return
	}
	if !e.IsEnabled(sessionKey) {
		state.nextBatch = nil
		state.nextBatchChars = 0
		state.batchStartTime = time.Time{}
		state.inFlight = false
		state.assistantLabel = ""
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		state.mu.Unlock()
		return
	}

	processing := state.nextBatch
	state.nextBatch = nil
	state.nextBatchChars = 0
	state.batchStartTime = time.Time{}
	state.inFlight = true
	sendFn := state.sendFn
	assistantLabel := state.assistantLabel
	state.mu.Unlock()

	var cutoffSeq int64
	if e.history != nil {
		for _, msg := range processing {
			seq, ok := e.history.AppendUserEventAt(sessionKey, msg.text, msg.speaker, msg.ts)
			if ok && seq > cutoffSeq {
				cutoffSeq = seq
			}
		}
	}

	repeatText, repeatCount, repeatParticipants := detectConsecutiveRepeat(processing)
	if repeatText != "" {
		now := time.Now()
		delivered := sendFn != nil
		if !delivered {
			log.Warn().
				Str("session", sessionKey).
				Str("delivery", "repeat").
				Int("reply_chars", len([]rune(repeatText))).
				Msg("repeat send function missing, skipping outbound send")
		} else {
			sendFn(repeatText)
		}
		if e.history != nil {
			_ = e.history.AppendAssistantEventWithSpeakerAt(sessionKey, repeatText, assistantLabel, cutoffSeq, now)
		}
		log.Info().
			Str("session", sessionKey).
			Str("repeat_text", repeatText).
			Int("repeat_count", repeatCount).
			Int("repeat_participants", repeatParticipants).
			Bool("delivered", delivered).
			Msg("repeat triggered")
	}

	state.mu.Lock()
	state.inFlight = false
	pending := len(state.nextBatch) > 0
	if pending && state.batchStartTime.IsZero() {
		state.batchStartTime = batchStartTimeFromQueue(state.nextBatch)
	}
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	if pending {
		decision := e.computeFlushDecision(state, time.Now())
		state.timer = time.AfterFunc(decision.Delay, func() {
			e.flush(sessionKey)
		})
	} else {
		state.batchStartTime = time.Time{}
	}
	state.mu.Unlock()
}

func (e *Engine) computeFlushDecision(state *sessionState, now time.Time) flushDecision {
	cfg := e.currentConfig()
	policy := cfg.FlushPolicy

	if state == nil {
		return flushDecision{
			Delay:    time.Duration(policy.MinBatchWaitMS) * time.Millisecond,
			Reason:   "no_session",
			Priority: "normal",
		}
	}

	if policy.MaxBatchSize > 0 && len(state.nextBatch) >= policy.MaxBatchSize {
		return flushDecision{Delay: 0, Reason: "batch_full", Priority: "immediate"}
	}

	var maxDeadlineRemaining time.Duration = -1
	if !state.batchStartTime.IsZero() && policy.MaxBatchWaitMS > 0 {
		maxDeadline := state.batchStartTime.Add(time.Duration(policy.MaxBatchWaitMS) * time.Millisecond)
		maxDeadlineRemaining = maxDeadline.Sub(now)
		if maxDeadlineRemaining <= 0 {
			return flushDecision{Delay: 0, Reason: "max_batch_deadline", Priority: "immediate"}
		}
	}

	delay := time.Duration(policy.MinBatchWaitMS) * time.Millisecond
	if maxDeadlineRemaining >= 0 && (delay <= 0 || maxDeadlineRemaining < delay) {
		delay = maxDeadlineRemaining
	}
	return flushDecision{Delay: delay, Reason: "normal_debounce", Priority: "normal"}
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

func detectConsecutiveRepeat(queue []queuedMessage) (text string, repeatCount int, participants int) {
	if len(queue) < 2 {
		return "", 0, 0
	}

	bestText := ""
	bestRepeatCount := 0
	bestParticipants := 0

	for i := 0; i < len(queue); {
		base := normalizeRepeatText(queue[i].text)
		j := i + 1
		for j < len(queue) && base != "" && normalizeRepeatText(queue[j].text) == base {
			j++
		}
		runCount := j - i
		if base != "" && runCount >= 2 {
			speakers := make(map[string]struct{}, runCount)
			for _, msg := range queue[i:j] {
				speaker := strings.TrimSpace(msg.speaker)
				if speaker == "" {
					continue
				}
				speakers[speaker] = struct{}{}
			}
			if len(speakers) >= 2 {
				bestText = strings.TrimSpace(queue[j-1].text)
				bestRepeatCount = runCount
				bestParticipants = len(speakers)
			}
		}
		if j == i+1 {
			i++
			continue
		}
		i = j
	}

	return bestText, bestRepeatCount, bestParticipants
}

func normalizeRepeatText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

func batchStartTimeFromQueue(queue []queuedMessage) time.Time {
	var start time.Time
	for _, msg := range queue {
		if msg.ts.IsZero() {
			continue
		}
		if start.IsZero() || msg.ts.Before(start) {
			start = msg.ts
		}
	}
	return start
}
