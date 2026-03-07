package immersive

import (
	"strings"
	"time"
)

const (
	debugReplyPreviewChars  = 160
	debugReasonPreviewChars = 120
)

// DebugSnapshot captures the latest immersive decision state for one session.
// It intentionally keeps only compact previews and stable reason codes.
type DebugSnapshot struct {
	SessionKey string
	UpdatedAt  time.Time

	ConversationMode string
	FocusSpeaker     string

	EnergyValue                 int
	EnergyTarget                int
	EnergyBaseline              int
	EnergyBand                  string
	EnergyLastDeltaReason       string
	EnergyLastFastRecoverReason string

	LastSignalScore    int
	LastSignalBand     string
	LastSignalFeatures string

	LastSchedulerReason   string
	LastSchedulerPriority string
	LastSchedulerDelayMS  int64

	PendingQuestion bool
	FollowupDueAt   time.Time
	FollowupBudget  int
	FollowupReady   bool

	ColdOpenEligibleAt    time.Time
	ColdOpenWindowOpen    bool
	IgnoredProactiveCount int
	LastProactiveKind     string
	LastProactiveStatus   string
	LastProactiveReason   string

	LastControlAction     string
	LastControlReason     string
	LastControlReasonCode string
	LastControlWaitMS     int

	LastFinalAction         string
	LastFinalReason         string
	LastFinalReasonCode     string
	LastReplyPreview        string
	LastStrongCallLatencyMS int64
	StrongCallPending       bool
}

func sanitizeDebugPreview(text string, maxChars int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || maxChars <= 0 {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "\r\n", "\n")
	trimmed = strings.ReplaceAll(trimmed, "\r", "\n")
	trimmed = strings.ReplaceAll(trimmed, "\n", " \\n ")
	return limitRunes(strings.TrimSpace(trimmed), maxChars)
}

func sanitizeDebugReason(text string) string {
	return sanitizeDebugPreview(text, debugReasonPreviewChars)
}

func (s *immersiveSession) refreshDebugLocked(now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.debug.UpdatedAt = now
	s.debug.ConversationMode = string(s.mode)
	s.debug.FocusSpeaker = strings.TrimSpace(s.focusSpeaker)
	s.debug.EnergyValue = roundEnergy(s.energy)
	s.debug.EnergyTarget = roundEnergy(s.energyTarget)
	s.debug.EnergyBaseline = roundEnergy(s.energyBaseline)
	s.debug.EnergyBand = energyBand(s.energy)
	s.debug.EnergyLastDeltaReason = strings.TrimSpace(s.energyLastDeltaReason)
	s.debug.EnergyLastFastRecoverReason = strings.TrimSpace(s.energyLastFastRecover)
	s.debug.PendingQuestion = s.pendingQuestion
	s.debug.FollowupDueAt = s.followupDueAt
	s.debug.FollowupBudget = s.followupBudget
	s.debug.FollowupReady = s.pendingQuestion && !s.followupDueAt.IsZero() && !now.Before(s.followupDueAt)
	s.debug.ColdOpenEligibleAt = s.nextColdOpenEligibleAt
	s.debug.ColdOpenWindowOpen = s.coldOpenEligible
	s.debug.StrongCallPending = s.strongCallPending
}

func (s *immersiveSession) snapshotDebugLocked(sessionKey string, now time.Time) DebugSnapshot {
	if s == nil {
		return DebugSnapshot{SessionKey: strings.TrimSpace(sessionKey)}
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.ensureBehaviorDefaultsLocked(now)
	s.settleEnergyLocked(now, "lazy_debug_recovery")
	s.decayBehaviorLocked(now)
	s.refreshDebugLocked(now)
	s.debug.SessionKey = strings.TrimSpace(sessionKey)
	return s.debug
}

func (s *immersiveSession) recordSchedulerDecisionLocked(decision FlushDecision, now time.Time) {
	if s == nil {
		return
	}
	s.refreshDebugLocked(now)
	s.debug.LastSchedulerReason = strings.TrimSpace(decision.Reason)
	s.debug.LastSchedulerPriority = strings.TrimSpace(decision.Priority)
	s.debug.LastSchedulerDelayMS = decision.Delay.Milliseconds()
}

func (s *immersiveSession) recordSpeakGateLocked(decision speakGateDecision, now time.Time) {
	if s == nil {
		return
	}
	s.refreshDebugLocked(now)
	s.debug.LastSignalScore = decision.SignalScore
	s.debug.LastSignalBand = string(decision.SignalBand)
	s.debug.LastSignalFeatures = formatSignalFeatures(decision.SignalFeatures)
}

func (s *immersiveSession) recordControlDecisionLocked(action controlAction, waitMS int, reason, reasonCode string, now time.Time) {
	if s == nil {
		return
	}
	s.refreshDebugLocked(now)
	s.debug.LastControlAction = string(action)
	s.debug.LastControlWaitMS = waitMS
	s.debug.LastControlReason = sanitizeDebugReason(reason)
	s.debug.LastControlReasonCode = strings.TrimSpace(reasonCode)
}

func (s *immersiveSession) recordFinalActionLocked(action, reasonCode, reason, reply string, now time.Time) {
	if s == nil {
		return
	}
	s.refreshDebugLocked(now)
	s.debug.LastFinalAction = strings.TrimSpace(action)
	s.debug.LastFinalReasonCode = strings.TrimSpace(reasonCode)
	s.debug.LastFinalReason = sanitizeDebugReason(reason)
	if strings.TrimSpace(reply) != "" {
		s.debug.LastReplyPreview = sanitizeDebugPreview(reply, debugReplyPreviewChars)
	}
}

func (s *immersiveSession) recordProactiveLocked(kind, status, reason string, ignored bool, now time.Time) {
	if s == nil {
		return
	}
	s.refreshDebugLocked(now)
	s.debug.LastProactiveKind = strings.TrimSpace(kind)
	s.debug.LastProactiveStatus = strings.TrimSpace(status)
	s.debug.LastProactiveReason = sanitizeDebugReason(reason)
	if ignored {
		s.debug.IgnoredProactiveCount++
	}
}

func (s *immersiveSession) recordFastRecoveryLocked(reason string, now time.Time) {
	if s == nil {
		return
	}
	s.energyLastFastRecover = strings.TrimSpace(reason)
	s.lastFastRecoverAt = now
	s.refreshDebugLocked(now)
}

func (s *immersiveSession) noteStrongCallPendingLocked(at time.Time) {
	if s == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	if s.strongCallPending {
		return
	}
	s.strongCallPending = true
	s.lastStrongCallAt = at
	s.refreshDebugLocked(at)
}

func (s *immersiveSession) resolveStrongCallLatencyLocked(now time.Time) int64 {
	if s == nil {
		return -1
	}
	if now.IsZero() {
		now = time.Now()
	}
	if !s.strongCallPending || s.lastStrongCallAt.IsZero() {
		s.strongCallPending = false
		s.lastStrongCallAt = time.Time{}
		s.debug.LastStrongCallLatencyMS = -1
		s.refreshDebugLocked(now)
		return -1
	}
	latency := now.Sub(s.lastStrongCallAt).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	s.strongCallPending = false
	s.lastStrongCallAt = time.Time{}
	s.debug.LastStrongCallLatencyMS = latency
	s.refreshDebugLocked(now)
	return latency
}

func (s *immersiveSession) clearStrongCallPendingLocked(now time.Time) {
	if s == nil {
		return
	}
	s.strongCallPending = false
	s.lastStrongCallAt = time.Time{}
	s.debug.LastStrongCallLatencyMS = -1
	s.refreshDebugLocked(now)
}

func (b *ImmersiveBuffer) lookupSession(sessionKey string) *immersiveSession {
	if b == nil || strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[strings.TrimSpace(sessionKey)]
}

// DebugSnapshot returns the latest compact decision snapshot for one session.
func (b *ImmersiveBuffer) DebugSnapshot(sessionKey string) DebugSnapshot {
	trimmed := strings.TrimSpace(sessionKey)
	if trimmed == "" {
		return DebugSnapshot{}
	}
	state := b.lookupSession(trimmed)
	if state == nil {
		return DebugSnapshot{SessionKey: trimmed, LastStrongCallLatencyMS: -1}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.snapshotDebugLocked(trimmed, time.Now())
}
