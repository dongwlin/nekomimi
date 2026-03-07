package immersive

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

// Default configuration values for the immersive buffer.
const (
	defaultRuntimeBufferMaxMessages  = 200
	defaultPokeReactionWindowMS      = 180000
	defaultPokeReactionMildThresh    = 3
	defaultPokeReactionAnnoyedThresh = 6
	maxImmersiveWaitRounds           = 2
	defaultProtocolErrorWaitMS       = 600
	immersiveEmptyReplyFallback      = "..."
	immersiveLogPreviewChars         = 600
)

// NewImmersiveBuffer creates a new ImmersiveBuffer with the given configuration,
// LLM manager, and bot nicknames.
func NewImmersiveBuffer(cfg config.ImmersiveConfig, llmManager llm.Service, nicknames []string) *ImmersiveBuffer {
	normalized := normalizeImmersiveConfig(cfg)
	configNames := normalizedBotNames(nicknames)
	return &ImmersiveBuffer{
		cfg:       normalized,
		llm:       llmManager,
		nicknames: configNames,
		identity: botIdentity{
			ConfigNicknames: configNames,
		},
		sessions: make(map[string]*immersiveSession),
	}
}

// NewEngine creates an immersive engine with configured policies.
func NewEngine(cfg config.ImmersiveConfig, llmManager llm.Service, nicknames []string) *ImmersiveBuffer {
	return NewImmersiveBuffer(cfg, llmManager, nicknames)
}

// ReloadConfig refreshes immersive policies without clearing runtime buffers.
func (b *ImmersiveBuffer) ReloadConfig(cfg config.ImmersiveConfig, nicknames []string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cfg = normalizeImmersiveConfig(cfg)
	b.nicknames = normalizedBotNames(nicknames)
	b.identity.ConfigNicknames = normalizedBotNames(nicknames)
}

// Enqueue adds a new message to the session buffer and schedules an immediate
// flush. It detects message signals (mentions, addressed to bot, questions)
// for downstream policies.
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
	mention, addressed, question, nickPos := b.detectMessageSignals(ctx, trimmed)
	msg := newQueuedMessage(EventUserMessage, trimmed, speaker, now, nil)
	msg.isMentionBot = mention
	msg.isQuestion = question
	msg.isAddressedToBot = addressed
	msg.nicknamePosition = nickPos

	state.mu.Lock()
	fastRecoveryReason := ""
	if sendFn := b.captureSendFunc(ctx); sendFn != nil {
		state.sendFn = sendFn
	}
	if len(state.nextBatch) == 0 {
		state.batchStartTime = now
	}
	state.nextBatch = append(state.nextBatch, msg)
	state.nextBatchChars += msg.chars
	state.runtimeBuffer = appendTimelineMessage(state.runtimeBuffer, msg, b.runtimeBufferLimit())
	state.ensureBehaviorDefaultsLocked(now)
	b.detectColdOpenEligibilityLocked(state, now)
	state.observeIncomingMessageLocked(msg, isPrivate, now)
	if state.lastFastRecoverAt.Equal(now) {
		fastRecoveryReason = state.energyLastFastRecover
	}
	if msg.isMentionBot || msg.nicknamePosition >= NickStart {
		state.noteStrongCallPendingLocked(now)
	}
	behavior := state.snapshotBehaviorLocked(now)

	decision := b.computeFlushDecision(sessionKey, state, now)
	state.recordSchedulerDecisionLocked(decision, now)
	queueLen := len(state.nextBatch)
	queueChars := state.nextBatchChars

	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.timer = time.AfterFunc(decision.Delay, func() {
		b.flush(sessionKey)
	})
	state.mu.Unlock()

	if strings.TrimSpace(fastRecoveryReason) != "" {
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			FastRecoveryReason: fastRecoveryReason,
		})
	}

	log.Info().
		Str("session", sessionKey).
		Bool("is_private", isPrivate).
		Bool("mention", mention).
		Bool("addressed", addressed).
		Bool("question", question).
		Int("nick_pos", int(nickPos)).
		Int("queue_len", queueLen).
		Int("queue_chars", queueChars).
		Str("mode", string(behavior.Mode)).
		Str("focus_speaker", behavior.FocusSpeaker).
		Int("energy_value", behavior.EnergyValue).
		Int("energy_target", behavior.EnergyTarget).
		Str("energy_band", behavior.EnergyBand).
		Int64("cooldown_ms", decision.Delay.Milliseconds()).
		Str("scheduler_reason", decision.Reason).
		Str("scheduler_priority", decision.Priority).
		Msg("immersive enqueue scheduled")
}

// Clear removes all queued messages and resets the session state for the given session key.
func (b *ImmersiveBuffer) Clear(sessionKey string) {
	if b == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	state := b.session(sessionKey)
	state.mu.Lock()
	state.nextBatch = nil
	state.nextBatchChars = 0
	state.batchStartTime = time.Time{}
	state.processingBatch = nil
	state.runtimeBuffer = nil
	state.sendFn = nil
	state.waitRounds = 0
	state.resetBehaviorStateLocked(time.Now())
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	if state.followupTimer != nil {
		state.followupTimer.Stop()
		state.followupTimer = nil
	}
	state.mu.Unlock()
	log.Info().Str("session", sessionKey).Msg("immersive session buffer cleared")
}

// flush processes queued messages through a two-phase flow:
// 1) control intent decision (skip/wait/reply), then
// 2) reply generation when action=reply.
func (b *ImmersiveBuffer) flush(sessionKey string) {
	state := b.session(sessionKey)
	state.mu.Lock()
	if state.inFlight {
		now := time.Now()
		if len(state.nextBatch) > 0 {
			if state.batchStartTime.IsZero() {
				state.batchStartTime = batchStartTimeFromQueue(state.nextBatch)
			}
			flushDec := b.computeFlushDecision(sessionKey, state, now)
			state.recordSchedulerDecisionLocked(flushDec, now)
			if state.timer != nil {
				state.timer.Stop()
				state.timer = nil
			}
			state.timer = time.AfterFunc(flushDec.Delay, func() {
				b.flush(sessionKey)
			})
		}
		state.recordFinalActionLocked("early_drop", "inflight", "flush skipped while another batch is in flight", "", now)
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "early_drop",
			ReasonCode: "inflight",
		})
		return
	}
	if len(state.nextBatch) == 0 {
		now := time.Now()
		state.batchStartTime = time.Time{}
		state.recordFinalActionLocked("early_drop", "empty_queue", "flush skipped because queue is empty", "", now)
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "early_drop",
			ReasonCode: "empty_queue",
		})
		return
	}
	if !b.llm.IsEnabled() || !b.llm.IsImmersive(sessionKey) {
		now := time.Now()
		reasonCode := "llm_disabled"
		reasonText := "immersive flush dropped because llm is disabled"
		if b.llm.IsEnabled() && !b.llm.IsImmersive(sessionKey) {
			reasonCode = "immersive_disabled"
			reasonText = "immersive flush dropped because immersive mode is disabled"
		}
		state.nextBatch = nil
		state.nextBatchChars = 0
		state.batchStartTime = time.Time{}
		state.processingBatch = nil
		state.recordFinalActionLocked("early_drop", reasonCode, reasonText, "", now)
		state.clearStrongCallPendingLocked(now)
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "early_drop",
			ReasonCode: reasonCode,
		})
		return
	}
	processing := state.nextBatch
	processingBatchStart := state.batchStartTime
	state.nextBatch = nil
	state.nextBatchChars = 0
	state.batchStartTime = time.Time{}
	state.processingBatch = processing
	state.inFlight = true
	waitRounds := state.waitRounds
	sendFn := state.sendFn
	state.mu.Unlock()

	skipFinalize := false
	defer func() {
		if skipFinalize {
			return
		}
		state.mu.Lock()
		state.inFlight = false
		state.processingBatch = nil
		state.waitRounds = 0
		pending := len(state.nextBatch) > 0
		if pending && state.batchStartTime.IsZero() {
			state.batchStartTime = batchStartTimeFromQueue(state.nextBatch)
		}
		var deferDec FlushDecision
		if pending {
			deferDec = b.computeFlushDecision(sessionKey, state, time.Now())
			state.recordSchedulerDecisionLocked(deferDec, time.Now())
		} else {
			state.batchStartTime = time.Time{}
		}
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		if pending {
			state.timer = time.AfterFunc(deferDec.Delay, func() {
				b.flush(sessionKey)
			})
		}
		state.mu.Unlock()
		if pending {
			log.Info().
				Str("session", sessionKey).
				Int64("next_delay_ms", deferDec.Delay.Milliseconds()).
				Str("next_reason", deferDec.Reason).
				Str("next_priority", deferDec.Priority).
				Msg("pending messages detected, flushing again")
		}
	}()

	log.Info().
		Str("session", sessionKey).
		Int("batch_messages", len(processing)).
		Int("batch_chars", sumQueueChars(processing)).
		Int("wait_rounds", waitRounds).
		Msg("immersive flush started")

	var cutoffSeq int64
	for i := range processing {
		if processing[i].persisted {
			if processing[i].causalSeq > cutoffSeq {
				cutoffSeq = processing[i].causalSeq
			}
			continue
		}
		seq, ok := b.llm.AppendUserEventAt(sessionKey, processing[i].text, processing[i].speaker, processing[i].ts)
		if !ok {
			log.Warn().
				Str("session", sessionKey).
				Str("speaker", processing[i].speaker).
				Msg("immersive user event append skipped")
			continue
		}
		processing[i].persisted = true
		processing[i].causalSeq = seq
		if seq > cutoffSeq {
			cutoffSeq = seq
		}
	}

	runtimeSnapshot := state.snapshotRuntimeBuffer()

	identity := b.currentIdentity()
	b.llm.SetAssistantSpeaker(assistantSpeakerLabel(identity))
	repeatText, repeatCount, repeatParticipants := detectConsecutiveRepeat(processing)
	if repeatText != "" {
		now := time.Now()
		delivered := sendFn != nil
		if !delivered {
			log.Warn().
				Str("session", sessionKey).
				Str("delivery", "repeat").
				Int("reply_chars", len([]rune(repeatText))).
				Msg("immersive send function missing, skipping outbound send")
		} else {
			sendFn(repeatText)
		}
		b.RecordEvent(sessionKey, NewRepeatTriggerEvent(repeatText, b.botPrimaryName(), repeatCount, repeatParticipants, time.Now()))
		b.recordAssistantUtterance(sessionKey, repeatText)
		_ = b.llm.AppendAssistantEvent(sessionKey, repeatText, cutoffSeq)
		state.mu.Lock()
		state.recordFinalActionLocked("reply", "repeat_trigger", "reply triggered by repeat detection", repeatText, now)
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "reply",
			ReasonCode: "repeat_trigger",
		})
		if delivered {
			b.noteAssistantDelivered(sessionKey, repeatText)
		}
		log.Info().
			Str("session", sessionKey).
			Str("repeat_text", repeatText).
			Int("repeat_count", repeatCount).
			Int("repeat_participants", repeatParticipants).
			Bool("delivered", delivered).
			Msg("immersive repeat triggered")
		return
	}

	now := time.Now()
	gateMeta := summarizeQueueMeta(processing, now, identity)
	state.mu.Lock()
	behavior, gate := state.evaluateSpeakGateLocked(sessionKey, gateMeta, now)
	state.mu.Unlock()
	b.recordImmersiveMetrics(metrics.ImmersiveRecord{
		SignalBand: string(gate.SignalBand),
	})
	log.Info().
		Str("session", sessionKey).
		Bool("allow", gate.Allow).
		Bool("strong_call", gate.StrongCall).
		Str("signal_band", string(gate.SignalBand)).
		Str("mode", string(behavior.Mode)).
		Str("focus_speaker", behavior.FocusSpeaker).
		Int("energy_value", behavior.EnergyValue).
		Int("energy_target", behavior.EnergyTarget).
		Str("energy_band", behavior.EnergyBand).
		Int("signal_score", gate.SignalScore).
		Str("signal_features", formatSignalFeatures(gate.SignalFeatures)).
		Str("reason", gate.Reason).
		Msg("immersive speak gate evaluated")
	if !gate.Allow {
		state.mu.Lock()
		state.recordFinalActionLocked("skip", gate.Reason, "speak gate rejected this batch", "", now)
		if state.coldOpenEligible {
			state.recordProactiveLocked("cold_open", "skipped", gate.Reason, true, now)
		}
		state.clearStrongCallPendingLocked(now)
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "skip",
			ReasonCode: gate.Reason,
			SignalBand: string(gate.SignalBand),
		})
		return
	}

	timelineSlice := trimTimelineTail(runtimeSnapshot, b.runtimeBufferLimit())
	debugPreview := buildCombinedInput(timelineSlice, identity)
	if strings.TrimSpace(debugPreview) == "" {
		debugPreview = buildCombinedInput(processing, identity)
	}
	immersiveCtx := buildImmersiveContext(processing, runtimeSnapshot, identity, behavior, gate)
	log.Info().
		Str("session", sessionKey).
		Int("debug_preview_chars", len([]rune(debugPreview))).
		Str("debug_preview", previewForLog(debugPreview, immersiveLogPreviewChars)).
		Str("prompt_source", "pipeline_blocks").
		Msg("immersive debug preview prepared")

	recordReply := func(reply, reason string, delivered bool) {
		trimmed := strings.TrimSpace(reply)
		if trimmed == "" {
			return
		}
		b.recordAssistantUtterance(sessionKey, trimmed)
		_ = b.llm.AppendAssistantEvent(sessionKey, trimmed, cutoffSeq)
		if delivered {
			b.noteAssistantDelivered(sessionKey, trimmed)
		}
		state.mu.Lock()
		state.recordFinalActionLocked("reply", reason, reason, trimmed, time.Now())
		if state.coldOpenEligible {
			state.recordProactiveLocked("cold_open", "triggered", reason, false, time.Now())
		}
		if !delivered {
			state.clearStrongCallPendingLocked(time.Now())
		}
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "reply",
			ReasonCode: reason,
			SignalBand: string(gate.SignalBand),
		})
		log.Info().
			Str("session", sessionKey).
			Str("reason", reason).
			Int("reply_chars", len([]rune(trimmed))).
			Int64("reply_to_cutoff_seq", cutoffSeq).
			Bool("delivered", delivered).
			Msg("immersive reply recorded into runtime buffer and llm history")
	}

	intent, intentErr := b.llm.DecideImmersiveIntent(context.Background(), "", sessionKey, "", immersiveCtx)
	decision := decisionFromIntent(intent)
	action := decision.action
	waitMS := decision.waitMS
	decisionReason := decision.reason
	decisionReasonCode := "model"

	if intentErr != nil {
		if isControlIntentProtocolError(intentErr) {
			if waitRounds == 0 {
				action = controlActionWait
				waitMS = defaultProtocolErrorWaitMS
				decisionReasonCode = "protocol_error_wait"
				decisionReason = "control protocol invalid, wait once for retry"
			} else {
				action = controlActionSkip
				waitMS = 0
				decisionReasonCode = "protocol_error_skip"
				decisionReason = "control protocol invalid repeatedly, skip this round"
			}
		} else {
			action = controlActionSkip
			waitMS = 0
			decisionReasonCode = "intent_error_skip"
			decisionReason = "intent decision failed, skip this round"
		}
	}
	if action == controlActionWait && waitRounds >= maxImmersiveWaitRounds {
		action = controlActionSkip
		waitMS = 0
		decisionReasonCode = "wait_round_limit_skip"
		decisionReason = "wait round limit reached, skip this round"
	}
	if action == "" {
		action = controlActionSkip
		waitMS = 0
		decisionReasonCode = "empty_action_skip"
		decisionReason = "empty control action, skip this round"
	}
	if (action == controlActionWait || action == controlActionSkip) && strings.TrimSpace(decisionReason) == "" {
		decisionReasonCode = "missing_reason_fallback"
		decisionReason = "missing control reason, skip this round"
		action = controlActionSkip
		waitMS = 0
	}
	state.mu.Lock()
	state.recordControlDecisionLocked(action, waitMS, decisionReason, decisionReasonCode, now)
	state.mu.Unlock()

	decisionLog := log.Info().
		Str("session", sessionKey).
		Str("action", string(action)).
		Int("wait_ms", waitMS).
		Int("wait_rounds", waitRounds).
		Str("conversation_mode", string(behavior.Mode)).
		Int("energy_value", behavior.EnergyValue).
		Int("energy_target", behavior.EnergyTarget).
		Str("signal_band", string(gate.SignalBand)).
		Str("reason", decisionReason).
		Str("reason_code", decisionReasonCode)
	if intentErr != nil {
		decisionLog = decisionLog.Err(intentErr)
	}
	decisionLog.Msg("immersive control decision evaluated")

	if action == controlActionWait {
		skipFinalize = true
		state.mu.Lock()
		state.nextBatch = prependMessages(processing, state.nextBatch)
		state.nextBatchChars += sumQueueChars(processing)
		if processingBatchStart.IsZero() {
			processingBatchStart = batchStartTimeFromQueue(processing)
		}
		state.batchStartTime = batchStartTimeFromQueue(state.nextBatch)
		if state.batchStartTime.IsZero() {
			state.batchStartTime = processingBatchStart
		}
		state.processingBatch = nil
		state.inFlight = false
		state.waitRounds++

		localDec := b.computeFlushDecision(sessionKey, state, time.Now())
		merged := mergeWaitDecision(localDec, waitMS)
		state.recordSchedulerDecisionLocked(merged, time.Now())
		state.recordFinalActionLocked("wait", decisionReasonCode, decisionReason, "", time.Now())
		if state.coldOpenEligible {
			state.recordProactiveLocked("cold_open", "skipped", decisionReasonCode, true, time.Now())
		}

		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}
		state.timer = time.AfterFunc(merged.Delay, func() {
			b.flush(sessionKey)
		})
		state.mu.Unlock()
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "wait",
			ReasonCode: decisionReasonCode,
			SignalBand: string(gate.SignalBand),
		})
		log.Info().
			Str("session", sessionKey).
			Int("llm_wait_ms", waitMS).
			Int64("local_delay_ms", localDec.Delay.Milliseconds()).
			Int64("merged_delay_ms", merged.Delay.Milliseconds()).
			Str("merged_reason", merged.Reason).
			Str("merged_priority", merged.Priority).
			Int("wait_rounds", waitRounds+1).
			Str("reason", decisionReason).
			Str("reason_code", decisionReasonCode).
			Msg("immersive flush deferred by wait action (merged)")
		return
	}

	if action == controlActionReply {
		_ = b.handleReply(sendFn, sessionKey, "", immersiveCtx, recordReply)
		return
	}

	state.mu.Lock()
	state.recordFinalActionLocked("skip", decisionReasonCode, decisionReason, "", time.Now())
	if state.coldOpenEligible {
		state.recordProactiveLocked("cold_open", "skipped", decisionReasonCode, true, time.Now())
	}
	state.clearStrongCallPendingLocked(time.Now())
	state.mu.Unlock()
	b.recordImmersiveMetrics(metrics.ImmersiveRecord{
		Action:     "skip",
		ReasonCode: decisionReasonCode,
		SignalBand: string(gate.SignalBand),
	})
}

// handleReply generates and sends an LLM reply for the given session,
// splitting multi-segment responses and recording the result.
func (b *ImmersiveBuffer) handleReply(
	sendFn SendFunc,
	sessionKey string,
	input string,
	immersiveCtx *contextassemble.ImmersiveContext,
	recordReply func(reply, reason string, delivered bool),
) error {
	onReplyEvent := func(event llm.StreamEvent) error {
		switch event.Type {
		case llm.StreamEventToolCall, llm.StreamEventToolResult, llm.StreamEventFinal, llm.StreamEventError:
			log.Debug().
				Str("session", sessionKey).
				Int64("stream_seq", event.Seq).
				Int("stream_step", event.Step).
				Str("stream_event_type", string(event.Type)).
				Msg("immersive reply stream event")
		}
		return nil
	}

	replyCtx, cancelReply := context.WithCancel(context.Background())
	reply, replyErr := b.llm.ReplyStreamWithExtraPromptAllowTools(
		replyCtx,
		input,
		sessionKey,
		"",
		"",
		onReplyEvent,
		immersiveCtx,
	)
	cancelReply()
	replyLog := log.Info().
		Str("session", sessionKey).
		Int("output_chars", len([]rune(reply))).
		Str("output_first_line", firstLineForLog(reply)).
		Str("output_preview", previewForLog(reply, immersiveLogPreviewChars))
	if replyErr != nil {
		replyLog = replyLog.Err(replyErr)
	}
	replyLog.Msg("immersive reply llm raw output captured")
	if replyErr != nil {
		if state := b.lookupSession(sessionKey); state != nil {
			state.mu.Lock()
			state.recordFinalActionLocked("early_drop", "reply_error", llm.UserVisibleError(replyErr), "", time.Now())
			state.clearStrongCallPendingLocked(time.Now())
			state.mu.Unlock()
		}
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			Action:     "early_drop",
			ReasonCode: "reply_error",
		})
		log.Warn().
			Str("session", sessionKey).
			Err(replyErr).
			Msg("immersive reply generation failed")
		return replyErr
	}

	reply = strings.TrimSpace(reply)
	if reply == "" {
		reply = immersiveEmptyReplyFallback
	}

	segments := SplitReplySegments(reply)
	if len(segments) == 0 {
		segments = []string{reply}
	}

	sentMessages := 0
	sentChars := 0
	replyParts := make([]string, 0, len(segments))
	canSend := sendFn != nil
	if !canSend {
		log.Warn().
			Str("session", sessionKey).
			Str("delivery", "reply").
			Int("segment_count", len(segments)).
			Msg("immersive send function missing, skipping outbound send")
	}
	for _, segment := range segments {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		replyParts = append(replyParts, trimmed)
		if !canSend {
			continue
		}
		if sentMessages > 0 {
			time.Sleep(NextReplySegmentDelay(trimmed))
		}
		sendFn(trimmed)
		sentMessages++
		sentChars += len([]rune(trimmed))
	}
	finalReply := strings.TrimSpace(strings.Join(replyParts, "\n"))
	if finalReply == "" {
		finalReply = immersiveEmptyReplyFallback
	}

	if !canSend {
		log.Info().
			Str("session", sessionKey).
			Int("reply_chars", len([]rune(finalReply))).
			Msg("immersive reply prepared without outbound delivery")
	} else {
		log.Info().
			Str("session", sessionKey).
			Int("reply_chars", len([]rune(finalReply))).
			Int("reply_messages", sentMessages).
			Int("sent_chars", sentChars).
			Msg("immersive reply sent")
	}
	recordReply(finalReply, "reply_action", canSend)
	return nil
}

// session retrieves or creates the session state for the given session key.
func (b *ImmersiveBuffer) session(sessionKey string) *immersiveSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = make(map[string]*immersiveSession)
	}
	if state, ok := b.sessions[sessionKey]; ok {
		return state
	}
	state := newImmersiveSession(time.Now())
	b.sessions[sessionKey] = state
	return state
}

func (b *ImmersiveBuffer) captureSendFunc(ctx *zero.Ctx) SendFunc {
	if b == nil || ctx == nil {
		return nil
	}
	return func(payload interface{}) {
		b.sendTracked(ctx, payload)
	}
}

func (b *ImmersiveBuffer) recordAssistantUtterance(sessionKey, text string) {
	if strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(text) == "" {
		return
	}
	b.RecordEvent(sessionKey, NewAssistantTextEvent(text, b.botPrimaryName(), time.Now()))
}

func (b *ImmersiveBuffer) noteAssistantDelivered(sessionKey, text string) int64 {
	if strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(text) == "" {
		return -1
	}
	state := b.session(sessionKey)
	now := time.Now()
	scheduledFollowup := false
	state.mu.Lock()
	state.noteAssistantDeliveredLocked(text, now)
	latency := state.resolveStrongCallLatencyLocked(now)

	state.nextColdOpenEligibleAt = now.Add(time.Duration(b.cfg.Scheduler.ColdOpenMinIntervalMS) * time.Millisecond)
	state.coldOpenEligible = false

	if state.pendingQuestion && state.followupBudget > 0 {
		followupDelay := time.Duration(b.cfg.Scheduler.FollowupWaitMS) * time.Millisecond
		state.followupDueAt = now.Add(followupDelay)
		if state.followupTimer != nil {
			state.followupTimer.Stop()
		}
		state.followupTimer = time.AfterFunc(followupDelay, func() {
			b.tryFollowup(sessionKey)
		})
		state.recordProactiveLocked("followup", "scheduled", "assistant_question", false, now)
		scheduledFollowup = true
	}
	state.refreshDebugLocked(now)
	state.mu.Unlock()

	if scheduledFollowup {
		log.Info().
			Str("session", sessionKey).
			Str("proactive_kind", "followup").
			Str("proactive_status", "scheduled").
			Msg("immersive proactive decision")
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			ProactiveKind:   "followup",
			ProactiveStatus: "scheduled",
		})
	}
	if latency >= 0 {
		b.recordImmersiveMetrics(metrics.ImmersiveRecord{
			StrongCallLatencyMS: latency,
		})
	}
	return latency
}

// RecordEvent appends a typed runtime event without queuing or flushing.
func (b *ImmersiveBuffer) RecordEvent(sessionKey string, event TimelineEvent) {
	if b == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	msg := queuedMessageFromTimelineEvent(event)
	if strings.TrimSpace(msg.text) == "" && len(msg.metadata) == 0 {
		return
	}
	state := b.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureBehaviorDefaultsLocked(msg.ts)
	state.runtimeBuffer = appendTimelineMessage(state.runtimeBuffer, msg, b.runtimeBufferLimit())
}

// RecordTimelineEvent appends an event to the runtime buffer without queuing or flushing.
func (b *ImmersiveBuffer) RecordTimelineEvent(sessionKey, text, speaker string) {
	b.RecordEvent(sessionKey, NewSystemNoteEvent(text, speaker, time.Now()))
}

func (b *ImmersiveBuffer) botPrimaryName() string {
	identity := b.currentIdentity()
	for _, name := range identity.ConfigNicknames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			return trimmed
		}
	}
	if trimmed := strings.TrimSpace(identity.AccountNickname); trimmed != "" {
		return trimmed
	}
	return "bot"
}

func assistantSpeakerLabel(identity botIdentity) string {
	name := "assistant"
	for _, candidate := range identity.ConfigNicknames {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			name = trimmed
			break
		}
	}
	if name == "assistant" {
		if nick := strings.TrimSpace(identity.AccountNickname); nick != "" {
			name = nick
		}
	}
	var id string
	for _, candidate := range identity.AccountIDs {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			id = trimmed
			break
		}
	}
	if id == "" {
		return "name=" + name
	}
	return "name=" + name + ";id=" + id
}

// RefreshIdentityFromCtx updates runtime bot identity data from event context.
// It captures account nickname and account IDs from both event and get_login_info.
func (b *ImmersiveBuffer) RefreshIdentityFromCtx(ctx *zero.Ctx) {
	if b == nil || ctx == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	updated := b.identity
	updated.ConfigNicknames = normalizedBotNames(b.nicknames)
	updated.AccountIDs = nil
	if ctx.Event != nil {
		if ctx.Event.SelfID != 0 {
			updated.AccountIDs = append(updated.AccountIDs, strconv.FormatInt(ctx.Event.SelfID, 10))
		}
		if strings.TrimSpace(ctx.Event.SelfTinyID) != "" {
			updated.AccountIDs = append(updated.AccountIDs, strings.TrimSpace(ctx.Event.SelfTinyID))
		}
	}
	info := ctx.GetLoginInfo()
	if nick := strings.TrimSpace(info.Get("nickname").String()); nick != "" {
		updated.AccountNickname = nick
	}
	if id := strings.TrimSpace(info.Get("user_id").String()); id != "" {
		updated.AccountIDs = append(updated.AccountIDs, id)
	}
	updated.AccountIDs = normalizedBotNames(updated.AccountIDs)
	b.identity = updated
}

func (b *ImmersiveBuffer) currentIdentity() botIdentity {
	b.mu.Lock()
	defer b.mu.Unlock()
	current := b.identity
	configNames := append([]string{}, b.nicknames...)
	configNames = append(configNames, current.ConfigNicknames...)
	current.ConfigNicknames = normalizedBotNames(configNames)
	current.AccountIDs = normalizedBotNames(current.AccountIDs)
	return current
}

func (b *ImmersiveBuffer) runtimeBufferLimit() int {
	return b.cfg.RuntimeBuffer.MaxMessages
}

func previewForLog(text string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = immersiveLogPreviewChars
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + "...(truncated)"
}

func firstLineForLog(text string) string {
	if text == "" {
		return ""
	}
	line := text
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}
