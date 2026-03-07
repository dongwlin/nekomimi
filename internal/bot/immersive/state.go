package immersive

import (
	"strings"
	"time"
)

const (
	defaultThreadWindow     = 75 * time.Second
	defaultCoolingWindow    = 90 * time.Second
	defaultIdleDecayWindow  = 4 * time.Minute
	defaultFollowupDelay    = 90 * time.Second
	defaultFollowupBudget   = 1
	defaultColdOpenCooldown = 10 * time.Minute
	defaultWeakAddressNudge = 2.0
)

// ConversationMode describes the bot's current social position in a session.
type ConversationMode string

const (
	ModeIdle        ConversationMode = "idle"
	ModeAddressed   ConversationMode = "addressed"
	ModeInThread    ConversationMode = "in_thread"
	ModeWaitingUser ConversationMode = "waiting_user"
	ModeCoolingDown ConversationMode = "cooling_down"
)

type behaviorSnapshot struct {
	Mode                   ConversationMode
	FocusSpeaker           string
	LastBotReplyAt         time.Time
	LastAddressedAt        time.Time
	LastTransitionReason   string
	EnergyValue            int
	EnergyBaseline         int
	EnergyTarget           int
	EnergyBand             string
	LastEnergyUpdateAt     time.Time
	EnergyLastDeltaReason  string
	SpeakGateOpen          bool
	PendingQuestion        bool
	FollowupDueAt          time.Time
	FollowupBudget         int
	NextColdOpenEligibleAt time.Time
	ColdOpenEligible       bool
}

func newImmersiveSession(now time.Time) *immersiveSession {
	session := &immersiveSession{}
	session.resetBehaviorStateLocked(now)
	return session
}

func (s *immersiveSession) snapshotBehavior(now time.Time) behaviorSnapshot {
	if s == nil {
		return behaviorSnapshot{
			Mode:           ModeIdle,
			EnergyValue:    int(defaultEnergyBaseline),
			EnergyBaseline: int(defaultEnergyBaseline),
			EnergyTarget:   int(defaultEnergyBaseline),
			EnergyBand:     energyBand(defaultEnergyBaseline),
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureBehaviorDefaultsLocked(now)
	s.settleEnergyLocked(now, "lazy_snapshot_recovery")
	s.decayBehaviorLocked(now)
	return s.snapshotBehaviorLocked(now)
}

func (s *immersiveSession) snapshotBehaviorLocked(now time.Time) behaviorSnapshot {
	if s == nil {
		return behaviorSnapshot{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.ensureBehaviorDefaultsLocked(now)
	return behaviorSnapshot{
		Mode:                   s.mode,
		FocusSpeaker:           strings.TrimSpace(s.focusSpeaker),
		LastBotReplyAt:         s.lastBotReplyAt,
		LastAddressedAt:        s.lastAddressedAt,
		LastTransitionReason:   strings.TrimSpace(s.lastTransitionReason),
		EnergyValue:            roundEnergy(s.energy),
		EnergyBaseline:         roundEnergy(s.energyBaseline),
		EnergyTarget:           roundEnergy(s.energyTarget),
		EnergyBand:             energyBand(s.energy),
		LastEnergyUpdateAt:     s.lastEnergyUpdateAt,
		EnergyLastDeltaReason:  strings.TrimSpace(s.energyLastDeltaReason),
		SpeakGateOpen:          s.speakGateOpen,
		PendingQuestion:        s.pendingQuestion,
		FollowupDueAt:          s.followupDueAt,
		FollowupBudget:         s.followupBudget,
		NextColdOpenEligibleAt: s.nextColdOpenEligibleAt,
		ColdOpenEligible:       s.coldOpenEligible,
	}
}

func (s *immersiveSession) snapshotRuntimeBuffer() []queuedMessage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := make([]queuedMessage, len(s.runtimeBuffer))
	copy(snapshot, s.runtimeBuffer)
	return snapshot
}

func (s *immersiveSession) resetBehaviorStateLocked(now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mode = ModeIdle
	s.focusSpeaker = ""
	s.lastBotReplyAt = time.Time{}
	s.lastAddressedAt = time.Time{}
	s.lastTransitionReason = "session_reset"
	s.energyBaseline = defaultEnergyBaseline
	s.energyTarget = energyTargetForMode(ModeIdle, s.energyBaseline)
	s.energy = s.energyTarget
	s.lastEnergyUpdateAt = now
	s.energyLastDeltaReason = "session_reset"
	s.energyLastFastRecover = ""
	s.lastFastRecoverAt = time.Time{}
	s.speakGateOpen = false
	s.pendingQuestion = false
	s.followupDueAt = time.Time{}
	s.followupBudget = 0
	if s.followupTimer != nil {
		s.followupTimer.Stop()
		s.followupTimer = nil
	}
	s.nextColdOpenEligibleAt = time.Time{}
	s.lastMessageAt = time.Time{}
	s.coldOpenEligible = false
	s.coldOpenActivityCount = 0
	s.lastStrongCallAt = time.Time{}
	s.strongCallPending = false
	s.debug = DebugSnapshot{}
	s.debug.LastStrongCallLatencyMS = -1
	s.refreshDebugLocked(now)
}

func (s *immersiveSession) ensureBehaviorDefaultsLocked(now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	if s.mode == "" {
		s.mode = ModeIdle
	}
	if s.energyBaseline <= 0 {
		s.energyBaseline = defaultEnergyBaseline
	}
	if s.energyTarget <= 0 {
		s.energyTarget = energyTargetForMode(s.mode, s.energyBaseline)
	}
	if s.lastEnergyUpdateAt.IsZero() && s.energy == 0 {
		s.energy = s.energyTarget
		s.lastEnergyUpdateAt = now
		if strings.TrimSpace(s.lastTransitionReason) == "" {
			s.lastTransitionReason = "session_init"
		}
		if strings.TrimSpace(s.energyLastDeltaReason) == "" {
			s.energyLastDeltaReason = "session_init"
		}
	}
	s.energy = clampEnergy(s.energy)
	if s.lastEnergyUpdateAt.IsZero() {
		s.lastEnergyUpdateAt = now
	}
}

func (s *immersiveSession) transitionToLocked(mode ConversationMode, reason string, now time.Time) {
	if s == nil {
		return
	}
	s.ensureBehaviorDefaultsLocked(now)
	if mode == "" {
		mode = ModeIdle
	}
	s.mode = mode
	s.energyTarget = energyTargetForMode(mode, s.energyBaseline)
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		s.lastTransitionReason = trimmed
	}
	switch mode {
	case ModeIdle:
		s.clearPendingQuestionLocked()
		s.focusSpeaker = ""
		s.speakGateOpen = false
	case ModeCoolingDown:
		s.clearPendingQuestionLocked()
		s.speakGateOpen = false
	case ModeWaitingUser:
		if s.followupBudget <= 0 {
			s.followupBudget = defaultFollowupBudget
		}
		s.speakGateOpen = false
	}
	s.refreshDebugLocked(now)
}

func (s *immersiveSession) clearPendingQuestionLocked() {
	if s == nil {
		return
	}
	s.pendingQuestion = false
	s.followupDueAt = time.Time{}
	s.followupBudget = 0
	if s.followupTimer != nil {
		s.followupTimer.Stop()
		s.followupTimer = nil
	}
	s.refreshDebugLocked(time.Now())
}

func (s *immersiveSession) decayBehaviorLocked(now time.Time) {
	if s == nil {
		return
	}
	s.ensureBehaviorDefaultsLocked(now)
	if s.pendingQuestion && !s.followupDueAt.IsZero() && !now.Before(s.followupDueAt) {
		s.clearPendingQuestionLocked()
		if s.mode == ModeWaitingUser {
			s.transitionToLocked(ModeIdle, "followup_window_elapsed", now)
		}
	}
	if s.mode == ModeCoolingDown && !s.lastBotReplyAt.IsZero() && now.Sub(s.lastBotReplyAt) >= defaultCoolingWindow && !s.pendingQuestion {
		s.transitionToLocked(ModeIdle, "cooldown_elapsed", now)
		return
	}
	anchor := recentConversationAnchor(s.lastBotReplyAt, s.lastAddressedAt)
	if anchor.IsZero() || s.pendingQuestion || s.mode == ModeIdle {
		return
	}
	if now.Sub(anchor) >= defaultIdleDecayWindow {
		s.transitionToLocked(ModeIdle, "conversation_idle_timeout", now)
	}
}

func (s *immersiveSession) observeIncomingMessageLocked(msg queuedMessage, isPrivate bool, now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.ensureBehaviorDefaultsLocked(now)
	s.settleEnergyLocked(now, "lazy_enqueue_recovery")
	s.decayBehaviorLocked(now)

	speaker := strings.TrimSpace(msg.speaker)
	if isPrivate {
		if speaker != "" {
			s.focusSpeaker = speaker
		}
		s.lastAddressedAt = now
		s.transitionToLocked(ModeAddressed, "private_message", now)
		s.raiseEnergyTowardsTargetLocked(defaultAddressBoost, "private_message_boost")
		s.maybeFastRecoveryLocked(defaultFastRecoveryChance, defaultFastRecoveryBoost, "private_message_fast_recovery", now)
		s.refreshDebugLocked(now)
		return
	}

	isStrongAddress := msg.isMentionBot || msg.nicknamePosition >= NickStart
	isWeakAddress := !isStrongAddress && msg.isAddressedToBot

	if isStrongAddress {
		sameFocus := sameSpeaker(speaker, s.focusSpeaker)
		if speaker != "" {
			s.focusSpeaker = speaker
		}
		s.lastAddressedAt = now
		if s.pendingQuestion && sameFocus {
			s.clearPendingQuestionLocked()
			s.recordProactiveLocked("followup", "canceled", "focus_reply_after_bot_question", false, now)
			s.transitionToLocked(ModeInThread, "focus_reply_after_bot_question", now)
		} else if s.mode == ModeInThread && sameFocus {
			s.transitionToLocked(ModeInThread, "explicit_thread_continuation", now)
		} else {
			s.transitionToLocked(ModeAddressed, "strong_address", now)
		}
		s.raiseEnergyTowardsTargetLocked(defaultAddressBoost, "strong_address_boost")
		s.maybeFastRecoveryLocked(defaultFastRecoveryChance, defaultFastRecoveryBoost, "strong_address_fast_recovery", now)
		s.refreshDebugLocked(now)
		return
	}

	if isWeakAddress {
		s.applyTrafficNudgeLocked(defaultWeakAddressNudge, "weak_address_nudge")
		s.refreshDebugLocked(now)
		return
	}

	if s.mode == ModeWaitingUser {
		if sameSpeaker(speaker, s.focusSpeaker) && speaker != "" {
			s.clearPendingQuestionLocked()
			s.recordProactiveLocked("followup", "canceled", "focus_reply_while_waiting_user", false, now)
			s.transitionToLocked(ModeInThread, "focus_reply_while_waiting_user", now)
			s.raiseEnergyTowardsTargetLocked(defaultThreadBoost, "focus_reply_energy_boost")
			s.refreshDebugLocked(now)
			return
		}
		s.applyTrafficNudgeLocked(defaultTrafficNudge, "ambient_waiting_user")
		s.refreshDebugLocked(now)
		return
	}

	if s.isLikelyThreadContinuationLocked(speaker, now) {
		if speaker != "" {
			s.focusSpeaker = speaker
		}
		s.transitionToLocked(ModeInThread, "thread_continuation", now)
		s.raiseEnergyTowardsTargetLocked(defaultThreadBoost, "thread_continuation_boost")
		s.refreshDebugLocked(now)
		return
	}

	s.applyTrafficNudgeLocked(defaultTrafficNudge, "ambient_traffic")
	s.refreshDebugLocked(now)
}

func (s *immersiveSession) isLikelyThreadContinuationLocked(speaker string, now time.Time) bool {
	if s == nil {
		return false
	}
	speaker = strings.TrimSpace(speaker)
	if speaker == "" || !sameSpeaker(speaker, s.focusSpeaker) {
		return false
	}
	switch s.mode {
	case ModeAddressed, ModeInThread, ModeCoolingDown:
	default:
		return false
	}
	anchor := recentConversationAnchor(s.lastBotReplyAt, s.lastAddressedAt)
	if anchor.IsZero() {
		return false
	}
	return now.Sub(anchor) <= defaultThreadWindow
}

func (s *immersiveSession) noteAssistantDeliveredLocked(reply string, now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.ensureBehaviorDefaultsLocked(now)
	s.settleEnergyLocked(now, "lazy_reply_recovery")
	s.lastBotReplyAt = now
	s.nextColdOpenEligibleAt = now.Add(defaultColdOpenCooldown)

	if replyLooksLikeQuestion(reply) {
		s.transitionToLocked(ModeWaitingUser, "assistant_question", now)
		s.pendingQuestion = true
		if s.followupBudget <= 0 {
			s.followupBudget = defaultFollowupBudget
		}
		s.followupDueAt = now.Add(defaultFollowupDelay)
	} else {
		s.transitionToLocked(ModeCoolingDown, "assistant_reply", now)
	}
	s.spendReplyEnergyLocked(reply, now, "assistant_reply_cost")
	s.speakGateOpen = false
	s.refreshDebugLocked(now)
}

func sameSpeaker(a, b string) bool {
	trimmedA := strings.TrimSpace(a)
	trimmedB := strings.TrimSpace(b)
	if trimmedA == "" || trimmedB == "" {
		return false
	}
	return strings.EqualFold(trimmedA, trimmedB)
}

func recentConversationAnchor(values ...time.Time) time.Time {
	var latest time.Time
	for _, candidate := range values {
		if candidate.IsZero() {
			continue
		}
		if latest.IsZero() || candidate.After(latest) {
			latest = candidate
		}
	}
	return latest
}

func replyLooksLikeQuestion(reply string) bool {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return false
	}
	return looksLikeQuestion(trimmed)
}
