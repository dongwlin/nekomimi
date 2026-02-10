package bot

import (
	"context"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type ImmersiveBuffer struct {
	cfg       config.ImmersiveConfig
	llm       *llm.Manager
	nicknames []string
	mu        sync.Mutex
	sessions  map[string]*immersiveSession
}

type immersiveSession struct {
	mu         sync.Mutex
	queue      []queuedMessage
	queueChars int
	recent     []recentSample
	timer      *time.Timer
	inFlight   bool
	lastCtx    *zero.Ctx
	postRounds int
}

type queuedMessage struct {
	text    string
	speaker string
	ts      time.Time
	chars   int
}

type recentSample struct {
	ts    time.Time
	chars int
}

const (
	defaultCooldownMinMS    = 800
	defaultCooldownMaxMS    = 3500
	defaultCooldownBaseMS   = 1200
	defaultPrivateBaseMS    = 200
	defaultWindowMS         = 5000
	defaultJitterMS         = 200
	defaultMaxBatchMessages = 10
	defaultMaxBatchChars    = 1200
	defaultImmediateDelayMS = 120
	defaultPostShortWaitMS  = 1200
	defaultPostLongWaitMS   = 5000
	defaultPostMaxRounds    = 3

	activeMsgPenaltyMS  = 150
	activeCharPenaltyMS = 4
	shortMsgLen         = 12
	shortMsgPenaltyMS   = 200
	mentionBonusMS      = 400
)

func NewImmersiveBuffer(cfg config.ImmersiveConfig, llmManager *llm.Manager, nicknames []string) *ImmersiveBuffer {
	normalized := normalizeImmersiveConfig(cfg)
	rand.Seed(time.Now().UnixNano())
	return &ImmersiveBuffer{
		cfg:       normalized,
		llm:       llmManager,
		nicknames: nicknames,
		sessions:  make(map[string]*immersiveSession),
	}
}

func (b *ImmersiveBuffer) Enqueue(ctx *zero.Ctx, sessionKey, text, speaker string, isPrivate bool) {
	if b == nil || b.llm == nil {
		return
	}
	trimmed := strings.TrimSpace(text)
	if strings.TrimSpace(sessionKey) == "" || trimmed == "" {
		return
	}
	state := b.session(sessionKey)
	now := time.Now()
	charCount := len([]rune(trimmed))
	msg := queuedMessage{
		text:    trimmed,
		speaker: strings.TrimSpace(speaker),
		ts:      now,
		chars:   charCount,
	}

	state.mu.Lock()
	state.lastCtx = ctx
	state.queue = append(state.queue, msg)
	state.queueChars += charCount
	state.recent = append(state.recent, recentSample{ts: now, chars: charCount})
	state.recent = trimRecent(state.recent, now, b.cfg.WindowMS)

	recentCount, recentChars := summarizeRecent(state.recent)
	mention := b.isMentioned(ctx, trimmed)
	question := looksLikeQuestion(trimmed)
	cooldown := b.calcCooldown(isPrivate, mention, question, recentCount, recentChars, charCount)
	queueSnapshot := make([]queuedMessage, len(state.queue))
	copy(queueSnapshot, state.queue)

	judgeEnabled := mention && !isPrivate && b.cfg.MentionJudge.Enabled
	if (mention || question) && !judgeEnabled {
		cooldown = minDuration(cooldown, time.Duration(b.cfg.ImmediateDelayMS)*time.Millisecond)
	}
	log.Info().
		Str("session", sessionKey).
		Bool("is_private", isPrivate).
		Bool("mention", mention).
		Bool("question", question).
		Int("queue_len", len(queueSnapshot)).
		Int("queue_chars", state.queueChars).
		Int("recent_count", recentCount).
		Int("recent_chars", recentChars).
		Int64("cooldown_ms", cooldown.Milliseconds()).
		Msg("immersive enqueue scheduled")
	state.mu.Unlock()

	if judgeEnabled {
		preview := buildRecentPreview(queueSnapshot, 4)
		immediate, err := b.llm.JudgeMentionImmediate(context.Background(), trimmed, speaker, preview)
		if err == nil && immediate {
			cooldown = minDuration(cooldown, time.Duration(b.cfg.ImmediateDelayMS)*time.Millisecond)
			log.Info().
				Str("session", sessionKey).
				Int64("cooldown_ms", cooldown.Milliseconds()).
				Msg("mention judge requested immediate reply")
		} else if err != nil {
			log.Warn().
				Err(err).
				Str("session", sessionKey).
				Msg("mention judge failed")
		}
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.queueChars >= b.cfg.MaxBatchChars || len(state.queue) >= b.cfg.MaxBatchMessages {
		cooldown = minDuration(cooldown, 200*time.Millisecond)
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.timer = time.AfterFunc(cooldown, func() {
		b.flush(sessionKey)
	})
}

func (b *ImmersiveBuffer) Clear(sessionKey string) {
	if b == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	state := b.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.queue = nil
	state.queueChars = 0
	state.recent = nil
	state.postRounds = 0
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	log.Info().Str("session", sessionKey).Msg("immersive session buffer cleared")
}

func (b *ImmersiveBuffer) flush(sessionKey string) {
	state := b.session(sessionKey)
	state.mu.Lock()
	if state.inFlight {
		if len(state.queue) > 0 {
			delay := time.Duration(b.cfg.ImmediateDelayMS) * time.Millisecond
			if state.timer != nil {
				state.timer.Stop()
			}
			state.timer = time.AfterFunc(delay, func() {
				b.flush(sessionKey)
			})
		}
		state.mu.Unlock()
		return
	}
	if len(state.queue) == 0 {
		state.mu.Unlock()
		return
	}
	if !b.llm.IsEnabled() || !b.llm.IsImmersive(sessionKey) {
		state.queue = nil
		state.queueChars = 0
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		state.mu.Unlock()
		return
	}
	queue := make([]queuedMessage, len(state.queue))
	copy(queue, state.queue)
	state.queue = nil
	state.queueChars = 0
	state.inFlight = true
	postRounds := state.postRounds
	ctx := state.lastCtx
	state.mu.Unlock()
	log.Info().
		Str("session", sessionKey).
		Int("batch_messages", len(queue)).
		Int("batch_chars", sumQueueChars(queue)).
		Int("post_rounds", postRounds).
		Msg("immersive flush started")

	input := buildCombinedInput(queue)
	decision := llm.DecisionReplyNow
	cooldownDelay := time.Duration(0)
	if input != "" && b.cfg.PostCooldownJudge.Enabled && postRounds < b.cfg.PostCooldownJudge.MaxRounds {
		lastSpeaker := queue[len(queue)-1].speaker
		recent := buildRecentPreview(queue, 6)
		judgeDecision, err := b.llm.JudgePostCooldown(context.Background(), input, lastSpeaker, recent)
		if err == nil {
			decision = judgeDecision
			log.Info().
				Str("session", sessionKey).
				Str("decision", string(decision)).
				Int("post_rounds", postRounds).
				Msg("post-cooldown judge decided")
		} else if !b.cfg.PostCooldownJudge.FailOpen {
			decision = llm.DecisionCooldownShort
			log.Warn().
				Err(err).
				Str("session", sessionKey).
				Bool("fail_open", false).
				Msg("post-cooldown judge failed, fallback to short cooldown")
		} else {
			log.Warn().
				Err(err).
				Str("session", sessionKey).
				Bool("fail_open", true).
				Msg("post-cooldown judge failed, continue reply")
		}
		if decision == llm.DecisionCooldownShort {
			cooldownDelay = time.Duration(b.cfg.PostCooldownJudge.ShortWaitMS) * time.Millisecond
		}
		if decision == llm.DecisionCooldownLong {
			cooldownDelay = time.Duration(b.cfg.PostCooldownJudge.LongWaitMS) * time.Millisecond
		}
	}
	if decision != llm.DecisionReplyNow {
		state.mu.Lock()
		state.queue = prependMessages(queue, state.queue)
		state.queueChars += sumQueueChars(queue)
		state.inFlight = false
		state.postRounds++
		if state.timer != nil {
			state.timer.Stop()
		}
		if cooldownDelay <= 0 {
			cooldownDelay = time.Duration(b.cfg.PostCooldownJudge.ShortWaitMS) * time.Millisecond
		}
		state.timer = time.AfterFunc(cooldownDelay, func() {
			b.flush(sessionKey)
		})
		state.mu.Unlock()
		log.Info().
			Str("session", sessionKey).
			Str("decision", string(decision)).
			Int64("next_cooldown_ms", cooldownDelay.Milliseconds()).
			Msg("immersive flush deferred by post-cooldown judge")
		return
	}
	if input != "" {
		reply, err := b.llm.Reply(context.Background(), input, sessionKey, "")
		if err != nil {
			log.Error().
				Err(err).
				Str("session", sessionKey).
				Msg("immersive reply failed")
			if ctx != nil {
				ctx.Send("LLM调用失败: " + err.Error())
			}
		} else if ctx != nil {
			ctx.Send(reply)
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(reply))).
				Msg("immersive reply sent")
		}
	}

	state.mu.Lock()
	state.inFlight = false
	state.postRounds = 0
	pending := len(state.queue) > 0
	state.mu.Unlock()
	if pending {
		log.Info().Str("session", sessionKey).Msg("pending messages detected, flushing again")
		b.flush(sessionKey)
	}
}

func (b *ImmersiveBuffer) session(sessionKey string) *immersiveSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = make(map[string]*immersiveSession)
	}
	if state, ok := b.sessions[sessionKey]; ok {
		return state
	}
	state := &immersiveSession{}
	b.sessions[sessionKey] = state
	return state
}

func (b *ImmersiveBuffer) isMentioned(ctx *zero.Ctx, text string) bool {
	if ctx != nil && ctx.Event != nil {
		if ctx.Event.IsToMe {
			return true
		}
		for _, seg := range ctx.Event.Message {
			if seg.Type != "at" {
				continue
			}
			qq := strings.TrimSpace(seg.Data["qq"])
			if qq == "" || qq == "all" {
				continue
			}
			if ctx.Event.SelfID != 0 && qq == strconv.FormatInt(ctx.Event.SelfID, 10) {
				return true
			}
			if ctx.Event.SelfTinyID != "" && qq == ctx.Event.SelfTinyID {
				return true
			}
		}
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "@") {
		return true
	}
	for _, name := range b.nicknames {
		trimmed := strings.ToLower(strings.TrimSpace(name))
		if trimmed == "" {
			continue
		}
		if strings.Contains(lower, trimmed) {
			return true
		}
	}
	return false
}

func (b *ImmersiveBuffer) calcCooldown(isPrivate, mention, question bool, recentCount, recentChars, msgChars int) time.Duration {
	base := b.cfg.CooldownBaseMS
	if isPrivate {
		base = b.cfg.PrivateBaseMS
	}
	cooldown := base
	cooldown += recentCount * activeMsgPenaltyMS
	cooldown += (recentChars / 10) * activeCharPenaltyMS
	if msgChars <= shortMsgLen {
		cooldown += shortMsgPenaltyMS
	}
	if mention || question {
		cooldown -= mentionBonusMS
	}
	if b.cfg.JitterMS > 0 {
		jitter := rand.Intn(b.cfg.JitterMS*2+1) - b.cfg.JitterMS
		cooldown += jitter
	}
	if cooldown < b.cfg.CooldownMinMS {
		cooldown = b.cfg.CooldownMinMS
	}
	if cooldown > b.cfg.CooldownMaxMS {
		cooldown = b.cfg.CooldownMaxMS
	}
	if mention || question {
		if cooldown < b.cfg.ImmediateDelayMS {
			cooldown = b.cfg.ImmediateDelayMS
		}
	}
	return time.Duration(cooldown) * time.Millisecond
}

func normalizeImmersiveConfig(cfg config.ImmersiveConfig) config.ImmersiveConfig {
	if cfg.CooldownMinMS <= 0 {
		cfg.CooldownMinMS = defaultCooldownMinMS
	}
	if cfg.CooldownMaxMS <= 0 {
		cfg.CooldownMaxMS = defaultCooldownMaxMS
	}
	if cfg.CooldownBaseMS <= 0 {
		cfg.CooldownBaseMS = defaultCooldownBaseMS
	}
	if cfg.PrivateBaseMS <= 0 {
		cfg.PrivateBaseMS = defaultPrivateBaseMS
	}
	if cfg.WindowMS <= 0 {
		cfg.WindowMS = defaultWindowMS
	}
	if cfg.JitterMS <= 0 {
		cfg.JitterMS = defaultJitterMS
	}
	if cfg.MaxBatchMessages <= 0 {
		cfg.MaxBatchMessages = defaultMaxBatchMessages
	}
	if cfg.MaxBatchChars <= 0 {
		cfg.MaxBatchChars = defaultMaxBatchChars
	}
	if cfg.ImmediateDelayMS <= 0 {
		cfg.ImmediateDelayMS = defaultImmediateDelayMS
	}
	if cfg.PostCooldownJudge.TimeoutMS <= 0 {
		cfg.PostCooldownJudge.TimeoutMS = 1200
	}
	if cfg.PostCooldownJudge.ShortWaitMS <= 0 {
		cfg.PostCooldownJudge.ShortWaitMS = defaultPostShortWaitMS
	}
	if cfg.PostCooldownJudge.LongWaitMS <= 0 {
		cfg.PostCooldownJudge.LongWaitMS = defaultPostLongWaitMS
	}
	if cfg.PostCooldownJudge.MaxRounds <= 0 {
		cfg.PostCooldownJudge.MaxRounds = defaultPostMaxRounds
	}
	return cfg
}

func trimRecent(recent []recentSample, now time.Time, windowMS int) []recentSample {
	if len(recent) == 0 {
		return recent
	}
	cutoff := now.Add(-time.Duration(windowMS) * time.Millisecond)
	start := 0
	for start < len(recent) && recent[start].ts.Before(cutoff) {
		start++
	}
	if start == 0 {
		return recent
	}
	trimmed := make([]recentSample, len(recent)-start)
	copy(trimmed, recent[start:])
	return trimmed
}

func summarizeRecent(recent []recentSample) (int, int) {
	count := len(recent)
	totalChars := 0
	for _, sample := range recent {
		totalChars += sample.chars
	}
	return count, totalChars
}

func buildRecentPreview(queue []queuedMessage, keep int) string {
	if len(queue) == 0 || keep <= 0 {
		return ""
	}
	start := len(queue) - keep
	if start < 0 {
		start = 0
	}
	return buildCombinedInput(queue[start:])
}

func buildCombinedInput(queue []queuedMessage) string {
	var builder strings.Builder
	for _, msg := range queue {
		formatted := formatQueuedMessage(msg)
		if formatted == "" {
			continue
		}
		builder.WriteString(formatted)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func formatQueuedMessage(msg queuedMessage) string {
	content := strings.TrimSpace(msg.text)
	if content == "" {
		return ""
	}
	label := strings.TrimSpace(msg.speaker)
	timeLabel := formatMessageTime(msg.ts)
	if label == "" {
		if timeLabel == "" {
			return content
		}
		return "[time=" + timeLabel + "]: " + content
	}
	if timeLabel == "" {
		return "[" + label + "]: " + content
	}
	return "[" + label + ";time=" + timeLabel + "]: " + content
}

func formatMessageTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format("2006-01-02 15:04:05")
}

func looksLikeQuestion(text string) bool {
	if strings.Contains(text, "?") || strings.Contains(text, "？") {
		return true
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "吗") || strings.Contains(lower, "能否")
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}

func sumQueueChars(queue []queuedMessage) int {
	total := 0
	for _, msg := range queue {
		total += msg.chars
	}
	return total
}

func prependMessages(head, tail []queuedMessage) []queuedMessage {
	if len(head) == 0 {
		return tail
	}
	if len(tail) == 0 {
		next := make([]queuedMessage, len(head))
		copy(next, head)
		return next
	}
	next := make([]queuedMessage, 0, len(head)+len(tail))
	next = append(next, head...)
	next = append(next, tail...)
	return next
}
