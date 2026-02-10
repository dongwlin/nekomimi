package bot

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
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
	lastReply  time.Time
	botTurns   int
}

type queuedMessage struct {
	text             string
	speaker          string
	ts               time.Time
	chars            int
	isMentionBot     bool
	isQuestion       bool
	isAddressedToBot bool
}

type recentSample struct {
	ts    time.Time
	chars int
}

const (
	defaultCooldownMinMS       = 800
	defaultCooldownMaxMS       = 3500
	defaultCooldownBaseMS      = 1200
	defaultPrivateBaseMS       = 200
	defaultWindowMS            = 5000
	defaultJitterMS            = 200
	defaultMaxBatchMessages    = 10
	defaultMaxBatchChars       = 1200
	defaultImmediateDelayMS    = 120
	defaultPostShortWaitMS     = 1200
	defaultPostLongWaitMS      = 5000
	defaultPostMaxRounds       = 3
	defaultSpeakThreshold      = 2
	defaultSpeakSuppressMS     = 2500
	defaultSpeakMaxTurns       = 1
	defaultSpeakJudgeTimeoutMS = 1200

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
	mention, addressed, question := b.detectMessageSignals(ctx, trimmed)
	msg := queuedMessage{
		text:             trimmed,
		speaker:          strings.TrimSpace(speaker),
		ts:               now,
		chars:            charCount,
		isMentionBot:     mention,
		isQuestion:       question,
		isAddressedToBot: addressed,
	}

	state.mu.Lock()
	state.lastCtx = ctx
	state.queue = append(state.queue, msg)
	state.queueChars += charCount
	state.recent = append(state.recent, recentSample{ts: now, chars: charCount})
	state.recent = trimRecent(state.recent, now, b.cfg.WindowMS)

	recentCount, recentChars := summarizeRecent(state.recent)
	cooldown := b.calcCooldown(isPrivate, addressed, question, recentCount, recentChars, charCount)
	queueSnapshot := make([]queuedMessage, len(state.queue))
	copy(queueSnapshot, state.queue)

	judgeEnabled := mention && !isPrivate && b.cfg.MentionJudge.Enabled
	if (addressed || question) && !judgeEnabled {
		cooldown = minDuration(cooldown, time.Duration(b.cfg.ImmediateDelayMS)*time.Millisecond)
	}
	log.Info().
		Str("session", sessionKey).
		Bool("is_private", isPrivate).
		Bool("mention", mention).
		Bool("addressed", addressed).
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
	gate := b.shouldSpeak(state, queue)
	log.Info().
		Str("session", sessionKey).
		Bool("should_speak", gate.shouldSpeak).
		Int("speak_score", gate.score).
		Int("speak_threshold", gate.threshold).
		Bool("rule_decision", gate.baseDecision).
		Str("assistant_status", gate.assistantStatus).
		Int("mentions_to_bot", gate.mentionsToBot).
		Int("addressed_to_bot", gate.addressedToBot).
		Int("questions_count", gate.questionsCount).
		Int("directed_questions", gate.directedQuestions).
		Int("messages_count", gate.messagesCount).
		Int("participants_count", gate.participantsCount).
		Str("suppress_reason", gate.reason).
		Msg("immersive speak gate evaluated")
	if !gate.shouldSpeak {
		cooldownDelay := time.Duration(b.cfg.PostCooldownJudge.ShortWaitMS) * time.Millisecond
		if cooldownDelay <= 0 {
			cooldownDelay = time.Duration(b.cfg.ImmediateDelayMS) * time.Millisecond
		}
		state.mu.Lock()
		state.inFlight = false
		state.postRounds = 0
		pending := len(state.queue) > 0
		if state.timer != nil {
			state.timer.Stop()
		}
		if pending {
			state.timer = time.AfterFunc(cooldownDelay, func() {
				b.flush(sessionKey)
			})
		}
		state.mu.Unlock()
		log.Info().
			Str("session", sessionKey).
			Int64("next_cooldown_ms", cooldownDelay.Milliseconds()).
			Bool("pending_messages", pending).
			Bool("should_speak", false).
			Int("speak_score", gate.score).
			Int("speak_threshold", gate.threshold).
			Bool("rule_decision", gate.baseDecision).
			Str("assistant_status", gate.assistantStatus).
			Str("suppress_reason", gate.reason).
			Msg("immersive flush skipped by speak gate")
		return
	}
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
				Bool("should_speak", gate.shouldSpeak).
				Int("speak_score", gate.score).
				Int("speak_threshold", gate.threshold).
				Bool("rule_decision", gate.baseDecision).
				Str("assistant_status", gate.assistantStatus).
				Str("suppress_reason", gate.reason).
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
			Bool("should_speak", gate.shouldSpeak).
			Int("speak_score", gate.score).
			Int("speak_threshold", gate.threshold).
			Bool("rule_decision", gate.baseDecision).
			Str("assistant_status", gate.assistantStatus).
			Str("suppress_reason", gate.reason).
			Int64("next_cooldown_ms", cooldownDelay.Milliseconds()).
			Msg("immersive flush deferred by post-cooldown judge")
		return
	}
	replySent := false
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
			replySent = true
			log.Info().
				Str("session", sessionKey).
				Int("reply_chars", len([]rune(reply))).
				Msg("immersive reply sent")
		} else {
			replySent = true
		}
	}

	state.mu.Lock()
	state.inFlight = false
	state.postRounds = 0
	if replySent {
		now := time.Now()
		window := time.Duration(b.cfg.SpeakGate.SuppressAfterBotReplyMS) * time.Millisecond
		if window > 0 && !state.lastReply.IsZero() && now.Sub(state.lastReply) <= window {
			state.botTurns++
		} else {
			state.botTurns = 1
		}
		state.lastReply = now
	}
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

func (b *ImmersiveBuffer) detectMessageSignals(ctx *zero.Ctx, text string) (bool, bool, bool) {
	mention := b.isExplicitMention(ctx)
	addressed := mention || b.containsNickname(text) || strings.Contains(text, "@")
	question := looksLikeQuestion(text)
	return mention, addressed, question
}

func (b *ImmersiveBuffer) isExplicitMention(ctx *zero.Ctx) bool {
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
	return false
}

func (b *ImmersiveBuffer) containsNickname(text string) bool {
	lower := strings.ToLower(text)
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
	if cfg.SpeakGate.Threshold <= 0 {
		cfg.SpeakGate.Threshold = defaultSpeakThreshold
	}
	if cfg.SpeakGate.SuppressAfterBotReplyMS <= 0 {
		cfg.SpeakGate.SuppressAfterBotReplyMS = defaultSpeakSuppressMS
	}
	if cfg.SpeakGate.MaxConsecutiveBotTurns <= 0 {
		cfg.SpeakGate.MaxConsecutiveBotTurns = defaultSpeakMaxTurns
	}
	if cfg.SpeakGate.Judge.TimeoutMS <= 0 {
		cfg.SpeakGate.Judge.TimeoutMS = defaultSpeakJudgeTimeoutMS
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
	meta := summarizeQueueMeta(queue, time.Now())
	var builder strings.Builder
	builder.WriteString("batch_meta:\n")
	builder.WriteString("  now_date: ")
	builder.WriteString(meta.NowDate)
	builder.WriteString("\n")
	builder.WriteString("  now_time: ")
	builder.WriteString(meta.NowTime)
	builder.WriteString("\n")
	builder.WriteString("  messages_count: ")
	builder.WriteString(strconv.Itoa(meta.MessagesCount))
	builder.WriteString("\n")
	builder.WriteString("  participants: [")
	builder.WriteString(strings.Join(meta.Participants, ","))
	builder.WriteString("]\n")
	builder.WriteString("  mentions_to_bot: ")
	builder.WriteString(strconv.Itoa(meta.MentionsToBot))
	builder.WriteString("\n")
	builder.WriteString("  addressed_to_bot: ")
	builder.WriteString(strconv.Itoa(meta.AddressedToBot))
	builder.WriteString("\n")
	builder.WriteString("  questions_count: ")
	builder.WriteString(strconv.Itoa(meta.QuestionsCount))
	builder.WriteString("\n")
	builder.WriteString("  last_speaker: ")
	builder.WriteString(meta.LastSpeaker)
	builder.WriteString("\n")
	builder.WriteString("  time_span_ms: ")
	builder.WriteString(strconv.FormatInt(meta.TimeSpanMS, 10))
	builder.WriteString("\n")
	builder.WriteString("transcript:\n")
	for _, msg := range queue {
		formatted := formatQueuedMessage(msg)
		if formatted == "" {
			continue
		}
		builder.WriteString("  - ")
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
	content = sanitizeInline(content)
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

func sanitizeInline(text string) string {
	replacer := strings.NewReplacer("\r\n", "\\n", "\n", "\\n", "\r", "\\n")
	return replacer.Replace(text)
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

type queueMeta struct {
	NowDate        string
	NowTime        string
	MessagesCount  int
	Participants   []string
	MentionsToBot  int
	AddressedToBot int
	QuestionsCount int
	LastSpeaker    string
	TimeSpanMS     int64
}

type speakGateResult struct {
	shouldSpeak       bool
	score             int
	threshold         int
	reason            string
	baseDecision      bool
	assistantStatus   string
	mentionsToBot     int
	addressedToBot    int
	questionsCount    int
	directedQuestions int
	messagesCount     int
	participantsCount int
}

func summarizeQueueMeta(queue []queuedMessage, now time.Time) queueMeta {
	meta := queueMeta{
		NowDate:       now.Format("2006-01-02"),
		NowTime:       now.Format("15:04:05"),
		MessagesCount: len(queue),
		LastSpeaker:   "unknown",
		Participants:  []string{"none"},
	}
	if len(queue) == 0 {
		return meta
	}
	participants := make(map[string]struct{}, len(queue))
	first := queue[0].ts
	last := queue[len(queue)-1].ts
	for _, msg := range queue {
		label := strings.TrimSpace(msg.speaker)
		if label == "" {
			label = "unknown"
		}
		participants[label] = struct{}{}
		if msg.isMentionBot {
			meta.MentionsToBot++
		}
		if msg.isAddressedToBot {
			meta.AddressedToBot++
		}
		if msg.isQuestion {
			meta.QuestionsCount++
		}
		if !msg.ts.IsZero() {
			if first.IsZero() || msg.ts.Before(first) {
				first = msg.ts
			}
			if last.IsZero() || msg.ts.After(last) {
				last = msg.ts
			}
		}
	}
	meta.Participants = make([]string, 0, len(participants))
	for label := range participants {
		meta.Participants = append(meta.Participants, sanitizeInline(label))
	}
	sort.Strings(meta.Participants)
	lastSpeaker := strings.TrimSpace(queue[len(queue)-1].speaker)
	if lastSpeaker != "" {
		meta.LastSpeaker = sanitizeInline(lastSpeaker)
	}
	if !first.IsZero() && !last.IsZero() && !last.Before(first) {
		meta.TimeSpanMS = last.Sub(first).Milliseconds()
	}
	return meta
}

func (b *ImmersiveBuffer) shouldSpeak(state *immersiveSession, queue []queuedMessage) speakGateResult {
	if !b.cfg.SpeakGate.Enabled {
		return speakGateResult{shouldSpeak: true, reason: "speak_gate_disabled", assistantStatus: "disabled"}
	}
	meta := summarizeQueueMeta(queue, time.Now())
	score := 0
	reasons := make([]string, 0, 8)
	directedQuestions := 0
	for _, msg := range queue {
		if msg.isQuestion && msg.isAddressedToBot {
			directedQuestions++
		}
	}
	if meta.MentionsToBot > 0 {
		score += 4
		reasons = append(reasons, "mention_to_bot(+4)")
	}
	if meta.AddressedToBot > 0 {
		score += 1
		reasons = append(reasons, "addressed_to_bot(+1)")
	}
	if directedQuestions > 0 {
		score += 3
		reasons = append(reasons, "direct_question(+3)")
	} else if meta.QuestionsCount > 0 {
		score += 2
		reasons = append(reasons, "question_present(+2)")
	}
	if meta.MessagesCount >= 3 && len(meta.Participants) >= 2 && meta.MentionsToBot == 0 && directedQuestions == 0 {
		score -= 2
		reasons = append(reasons, "active_human_chat(-2)")
	}
	if b.cfg.SpeakGate.MaxConsecutiveBotTurns > 0 && state != nil {
		state.mu.Lock()
		botTurns := state.botTurns
		lastReply := state.lastReply
		state.mu.Unlock()
		window := time.Duration(b.cfg.SpeakGate.SuppressAfterBotReplyMS) * time.Millisecond
		if botTurns >= b.cfg.SpeakGate.MaxConsecutiveBotTurns &&
			window > 0 &&
			!lastReply.IsZero() &&
			time.Since(lastReply) <= window {
			score -= 3
			reasons = append(reasons, "recent_bot_reply(-3)")
		}
	}
	if meta.MessagesCount <= 2 && len(meta.Participants) <= 2 {
		score += 1
		reasons = append(reasons, "low_traffic_window(+1)")
	}
	threshold := b.cfg.SpeakGate.Threshold
	should := score >= threshold || meta.MentionsToBot > 0 || directedQuestions > 0
	baseDecision := should
	assistantStatus := "not_enabled"
	if b.llm != nil && b.cfg.SpeakGate.Judge.Enabled {
		assistantStatus = "enabled"
		assistantDecision, judged, err := b.llm.JudgeSpeakGate(
			context.Background(),
			buildCombinedInput(queue),
			score,
			threshold,
			strings.Join(reasons, ","),
		)
		if err != nil {
			if b.cfg.SpeakGate.Judge.FailOpen {
				reasons = append(reasons, "assistant_error_fallback_rules(0)")
				assistantStatus = "error_fallback_rules"
			} else {
				should = false
				reasons = append(reasons, "assistant_error_block(override_no)")
				assistantStatus = "error_block"
			}
		} else if judged {
			should = assistantDecision
			if assistantDecision {
				reasons = append(reasons, "assistant_yes(override)")
				assistantStatus = "yes"
			} else {
				reasons = append(reasons, "assistant_no(override)")
				assistantStatus = "no"
			}
		}
	}
	if !should {
		reasons = append(reasons, fmt.Sprintf("score_below_threshold(%d<%d)", score, threshold))
	}
	return speakGateResult{
		shouldSpeak:       should,
		score:             score,
		threshold:         threshold,
		reason:            strings.Join(reasons, ","),
		baseDecision:      baseDecision,
		assistantStatus:   assistantStatus,
		mentionsToBot:     meta.MentionsToBot,
		addressedToBot:    meta.AddressedToBot,
		questionsCount:    meta.QuestionsCount,
		directedQuestions: directedQuestions,
		messagesCount:     meta.MessagesCount,
		participantsCount: len(meta.Participants),
	}
}
