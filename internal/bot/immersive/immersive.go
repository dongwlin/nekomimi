package immersive

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/ctxasm"
	"github.com/dongwlin/nekomimi/internal/llm"
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
	meta := b.AnalyzeAmbientMessage(ctx, text, speaker, isPrivate, time.Now())
	b.EnqueueAmbient(ctx, sessionKey, meta, 0)
}

// EnqueueAmbient adds one already-parsed ambient message to the session buffer.
// When persistedSeq is positive the message is treated as already written into
// chat history and will not be appended again during flush.
func (b *ImmersiveBuffer) EnqueueAmbient(ctx *zero.Ctx, sessionKey string, meta AmbientMessageMeta, persistedSeq int64) {
	if b == nil || b.llm == nil {
		return
	}
	trimmed := strings.TrimSpace(meta.Text)
	if strings.TrimSpace(sessionKey) == "" || trimmed == "" {
		return
	}
	state := b.session(sessionKey)
	now := meta.At
	if now.IsZero() {
		now = time.Now()
	}
	msg := newQueuedMessage(EventUserMessage, trimmed, meta.Speaker, now, meta.HistoryMetadata())
	msg.isMentionBot = meta.MentionBot
	msg.isQuestion = meta.Question
	msg.isAddressedToBot = meta.AddressedToBot
	msg.nicknamePosition = meta.NicknamePosition
	if persistedSeq > 0 {
		msg.persisted = true
		msg.causalSeq = persistedSeq
	}

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
	state.observeIncomingMessageLocked(msg, meta.IsPrivate, now)
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
		Bool("is_private", meta.IsPrivate).
		Bool("mention", meta.MentionBot).
		Bool("addressed", meta.AddressedToBot).
		Bool("question", meta.Question).
		Int("nick_pos", int(meta.NicknamePosition)).
		Int64("persisted_seq", persistedSeq).
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

type flushBatch struct {
	processing           []queuedMessage
	processingBatchStart time.Time
	waitRounds           int
	sendFn               SendFunc
}

type preparedFlushContext struct {
	evaluatedAt  time.Time
	behavior     behaviorSnapshot
	gate         speakGateDecision
	gateMeta     queueMeta
	immersiveCtx *ctxasm.ImmersiveContext
	replyPrompt  string
}

type resolvedControlAction struct {
	action     controlAction
	waitMS     int
	reason     string
	reasonCode string
	err        error
}

// flush processes queued messages through a two-phase flow:
// 1) control intent decision (skip/wait/reply), then
// 2) reply generation when action=reply.
func (b *ImmersiveBuffer) flush(sessionKey string) {
	state := b.session(sessionKey)
	batch, ok := b.acquireProcessingBatch(sessionKey, state)
	if !ok {
		return
	}

	skipFinalize := false
	defer func() {
		b.finalizeFlush(sessionKey, state, skipFinalize)
	}()

	log.Info().
		Str("session", sessionKey).
		Int("batch_messages", len(batch.processing)).
		Int("batch_chars", sumQueueChars(batch.processing)).
		Int("wait_rounds", batch.waitRounds).
		Msg("immersive flush started")

	cutoffSeq := b.persistProcessingBatch(sessionKey, batch.processing)
	flushCtx := b.prepareFlushContext(sessionKey, state, batch.processing)
	if !flushCtx.gate.Allow {
		b.applySkipDecision(sessionKey, state, flushCtx.gate, flushCtx.gate.Reason, "speak gate rejected this batch")
		return
	}

	recordReply := func(reply, reason string, delivered bool) {
		b.recordReplyOutcome(sessionKey, state, flushCtx.gate, cutoffSeq, reply, reason, delivered)
	}

	decision := b.resolveControlAction(sessionKey, batch.waitRounds, flushCtx.gateMeta, flushCtx.gate, flushCtx.immersiveCtx)
	state.mu.Lock()
	state.recordControlDecisionLocked(decision.action, decision.waitMS, decision.reason, decision.reasonCode, flushCtx.evaluatedAt)
	state.mu.Unlock()

	decisionLog := log.Info().
		Str("session", sessionKey).
		Str("action", string(decision.action)).
		Int("wait_ms", decision.waitMS).
		Int("wait_rounds", batch.waitRounds).
		Str("conversation_mode", string(flushCtx.behavior.Mode)).
		Int("energy_value", flushCtx.behavior.EnergyValue).
		Int("energy_target", flushCtx.behavior.EnergyTarget).
		Str("signal_band", string(flushCtx.gate.SignalBand)).
		Str("reason", decision.reason).
		Str("reason_code", decision.reasonCode)
	if decision.err != nil {
		decisionLog = decisionLog.Err(decision.err)
	}
	decisionLog.Msg("immersive control decision evaluated")

	if decision.action == controlActionWait {
		skipFinalize = true
		b.applyWaitDecision(sessionKey, state, batch.processing, batch.processingBatchStart, batch.waitRounds, decision, flushCtx.gate)
		return
	}

	if decision.action == controlActionReply {
		_ = b.handleReply(batch.sendFn, sessionKey, "", flushCtx.replyPrompt, flushCtx.immersiveCtx, recordReply)
		return
	}

	b.applySkipDecision(sessionKey, state, flushCtx.gate, decision.reasonCode, decision.reason)
}

func (b *ImmersiveBuffer) acquireProcessingBatch(sessionKey string, state *immersiveSession) (flushBatch, bool) {
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
		return flushBatch{}, false
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
		return flushBatch{}, false
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
		return flushBatch{}, false
	}

	batch := flushBatch{
		processing:           state.nextBatch,
		processingBatchStart: state.batchStartTime,
		waitRounds:           state.waitRounds,
		sendFn:               state.sendFn,
	}
	state.nextBatch = nil
	state.nextBatchChars = 0
	state.batchStartTime = time.Time{}
	state.processingBatch = batch.processing
	state.inFlight = true
	state.mu.Unlock()
	return batch, true
}

func (b *ImmersiveBuffer) finalizeFlush(sessionKey string, state *immersiveSession, skipFinalize bool) {
	if skipFinalize {
		return
	}
	state.mu.Lock()
	state.inFlight = false
	state.processingBatch = nil
	state.waitRounds = 0
	deferDec, pending := b.reschedulePendingBatchLocked(sessionKey, state, time.Now())
	state.mu.Unlock()
	if pending {
		log.Info().
			Str("session", sessionKey).
			Int64("next_delay_ms", deferDec.Delay.Milliseconds()).
			Str("next_reason", deferDec.Reason).
			Str("next_priority", deferDec.Priority).
			Msg("pending messages detected, flushing again")
	}
}

func (b *ImmersiveBuffer) reschedulePendingBatchLocked(sessionKey string, state *immersiveSession, now time.Time) (FlushDecision, bool) {
	if state == nil {
		return FlushDecision{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	pending := len(state.nextBatch) > 0
	if pending && state.batchStartTime.IsZero() {
		state.batchStartTime = batchStartTimeFromQueue(state.nextBatch)
	} else if !pending {
		state.batchStartTime = time.Time{}
	}
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	if !pending {
		return FlushDecision{}, false
	}
	decision := b.computeFlushDecision(sessionKey, state, now)
	state.recordSchedulerDecisionLocked(decision, now)
	state.timer = time.AfterFunc(decision.Delay, func() {
		b.flush(sessionKey)
	})
	return decision, true
}

func (b *ImmersiveBuffer) persistProcessingBatch(sessionKey string, processing []queuedMessage) int64 {
	var cutoffSeq int64
	for i := range processing {
		if processing[i].persisted {
			if processing[i].causalSeq > cutoffSeq {
				cutoffSeq = processing[i].causalSeq
			}
			continue
		}
		seq, ok := b.llm.AppendUserEventWithMetadataAt(sessionKey, processing[i].text, processing[i].speaker, processing[i].ts, processing[i].metadata)
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
	return cutoffSeq
}

func (b *ImmersiveBuffer) prepareFlushContext(sessionKey string, state *immersiveSession, processing []queuedMessage) preparedFlushContext {
	runtimeSnapshot := state.snapshotRuntimeBuffer()
	identity := b.currentIdentity()
	b.llm.SetAssistantSpeaker(assistantSpeakerLabel(identity))

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

	flushCtx := preparedFlushContext{
		evaluatedAt: now,
		behavior:    behavior,
		gate:        gate,
		gateMeta:    gateMeta,
	}
	if !gate.Allow {
		return flushCtx
	}

	timelineSlice := trimTimelineTail(runtimeSnapshot, b.runtimeBufferLimit())
	debugPreview := buildCombinedInput(timelineSlice, identity)
	if strings.TrimSpace(debugPreview) == "" {
		debugPreview = buildCombinedInput(processing, identity)
	}
	flushCtx.immersiveCtx = buildImmersiveContext(processing, runtimeSnapshot, identity, behavior, gate)
	flushCtx.replyPrompt = buildImmersiveReplyPrompt(flushCtx.immersiveCtx)
	log.Info().
		Str("session", sessionKey).
		Int("debug_preview_chars", len([]rune(debugPreview))).
		Str("debug_preview", previewForLog(debugPreview, immersiveLogPreviewChars)).
		Str("prompt_source", "pipeline_blocks").
		Msg("immersive debug preview prepared")
	return flushCtx
}

func (b *ImmersiveBuffer) resolveControlAction(sessionKey string, waitRounds int, gateMeta queueMeta, gate speakGateDecision, immersiveCtx *ctxasm.ImmersiveContext) resolvedControlAction {
	result := resolvedControlAction{
		action:     controlActionReply,
		reason:     "private strong call bypassed control intent",
		reasonCode: "private_strong_call_fast_path",
	}
	if !shouldBypassControlIntent(sessionKey, gateMeta, gate) {
		intent, err := b.llm.DecideImmersiveIntent(context.Background(), "", sessionKey, "", immersiveCtx)
		result.err = err
		decision := decisionFromIntent(intent)
		result.action = decision.action
		result.waitMS = decision.waitMS
		result.reason = decision.reason
		result.reasonCode = "model"
	}

	if result.err != nil {
		if isControlIntentProtocolError(result.err) {
			if waitRounds == 0 {
				result.action = controlActionWait
				result.waitMS = defaultProtocolErrorWaitMS
				result.reasonCode = "protocol_error_wait"
				result.reason = "control protocol invalid, wait once for retry"
			} else {
				result.action = controlActionSkip
				result.waitMS = 0
				result.reasonCode = "protocol_error_skip"
				result.reason = "control protocol invalid repeatedly, skip this round"
			}
		} else {
			result.action = controlActionSkip
			result.waitMS = 0
			result.reasonCode = "intent_error_skip"
			result.reason = "intent decision failed, skip this round"
		}
	}
	if result.action == controlActionWait && waitRounds >= maxImmersiveWaitRounds {
		result.action = controlActionSkip
		result.waitMS = 0
		result.reasonCode = "wait_round_limit_skip"
		result.reason = "wait round limit reached, skip this round"
	}
	if result.action == "" {
		result.action = controlActionSkip
		result.waitMS = 0
		result.reasonCode = "empty_action_skip"
		result.reason = "empty control action, skip this round"
	}
	if (result.action == controlActionWait || result.action == controlActionSkip) && strings.TrimSpace(result.reason) == "" {
		result.reasonCode = "missing_reason_fallback"
		result.reason = "missing control reason, skip this round"
		result.action = controlActionSkip
		result.waitMS = 0
	}
	return result
}

func (b *ImmersiveBuffer) applyWaitDecision(sessionKey string, state *immersiveSession, processing []queuedMessage, processingBatchStart time.Time, waitRounds int, decision resolvedControlAction, gate speakGateDecision) {
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
	merged := mergeWaitDecision(localDec, decision.waitMS)
	state.recordSchedulerDecisionLocked(merged, time.Now())
	state.recordFinalActionLocked("wait", decision.reasonCode, decision.reason, "", time.Now())
	if state.coldOpenEligible {
		state.recordProactiveLocked("cold_open", "skipped", decision.reasonCode, true, time.Now())
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
		ReasonCode: decision.reasonCode,
		SignalBand: string(gate.SignalBand),
	})
	log.Info().
		Str("session", sessionKey).
		Int("llm_wait_ms", decision.waitMS).
		Int64("local_delay_ms", localDec.Delay.Milliseconds()).
		Int64("merged_delay_ms", merged.Delay.Milliseconds()).
		Str("merged_reason", merged.Reason).
		Str("merged_priority", merged.Priority).
		Int("wait_rounds", waitRounds+1).
		Str("reason", decision.reason).
		Str("reason_code", decision.reasonCode).
		Msg("immersive flush deferred by wait action (merged)")
}

func (b *ImmersiveBuffer) applySkipDecision(sessionKey string, state *immersiveSession, gate speakGateDecision, reasonCode, reason string) {
	state.mu.Lock()
	state.recordFinalActionLocked("skip", reasonCode, reason, "", time.Now())
	if state.coldOpenEligible {
		state.recordProactiveLocked("cold_open", "skipped", reasonCode, true, time.Now())
	}
	state.clearStrongCallPendingLocked(time.Now())
	state.mu.Unlock()
	b.recordImmersiveMetrics(metrics.ImmersiveRecord{
		Action:     "skip",
		ReasonCode: reasonCode,
		SignalBand: string(gate.SignalBand),
	})
}

func (b *ImmersiveBuffer) recordReplyOutcome(sessionKey string, state *immersiveSession, gate speakGateDecision, cutoffSeq int64, reply, reason string, delivered bool) {
	trimmed := b.applyReplyBookkeeping(sessionKey, state, cutoffSeq, reply, delivered)
	if trimmed == "" {
		return
	}
	state.mu.Lock()
	state.recordFinalActionLocked("reply", reason, reason, trimmed, time.Now())
	if state.coldOpenEligible {
		state.recordProactiveLocked("cold_open", "triggered", reason, false, time.Now())
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

func (b *ImmersiveBuffer) applyReplyBookkeeping(sessionKey string, state *immersiveSession, cutoffSeq int64, reply string, delivered bool) string {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return ""
	}
	b.recordAssistantUtterance(sessionKey, trimmed)
	_ = b.llm.AppendAssistantEvent(sessionKey, trimmed, cutoffSeq)
	if delivered {
		b.noteAssistantDelivered(sessionKey, trimmed)
		return trimmed
	}
	if state == nil {
		state = b.lookupSession(sessionKey)
	}
	if state != nil {
		state.mu.Lock()
		state.clearStrongCallPendingLocked(time.Now())
		state.mu.Unlock()
	}
	return trimmed
}

// handleReply generates and sends an LLM reply for the given session,
// splitting multi-segment responses and recording the result.
func (b *ImmersiveBuffer) handleReply(
	sendFn SendFunc,
	sessionKey string,
	input string,
	extraPrompt string,
	immersiveCtx *ctxasm.ImmersiveContext,
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
		extraPrompt,
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

	segments := SplitReplySegmentsForDelivery(reply, replySegmentLimit(immersiveCtx))

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

// RecordAssistantDelivered synchronizes one externally delivered assistant
// message into the immersive runtime state.
func (b *ImmersiveBuffer) RecordAssistantDelivered(sessionKey, text, speaker string) {
	if b == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(text) == "" {
		return
	}
	label := strings.TrimSpace(speaker)
	if label == "" {
		label = b.botPrimaryName()
	}
	b.RecordEvent(sessionKey, NewAssistantTextEvent(text, label, time.Now()))
	b.noteAssistantDelivered(sessionKey, text)
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
