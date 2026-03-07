package immersive

import (
	"math"
	"math/rand"
	"strings"
	"time"
)

const (
	defaultEnergyBaseline          = 45.0
	defaultCoolingEnergyTarget     = 32.0
	defaultAddressedEnergyTarget   = 60.0
	defaultInThreadEnergyTarget    = 70.0
	defaultWaitingUserEnergyTarget = 40.0

	defaultEnergyRecoveryPerSecond = 0.20
	defaultTrafficNudge            = 1.0
	defaultAddressBoost            = 10.0
	defaultThreadBoost             = 6.0
	defaultFastRecoveryBoost       = 10.0
	defaultFastRecoveryChance      = 0.25

	defaultReplyEnergyCost         = 18.0
	defaultQuestionReplyEnergyCost = 12.0

	defaultSpeakOpenThreshold  = 56
	defaultSpeakCloseThreshold = 44

	defaultHighPriorityScoreBoost = 5.0
	defaultEngageScoreNudge       = 2.0
)

type speakGateDecision struct {
	Allow            bool
	StrongCall       bool
	SignalScore      int
	SignalBand       SignalBand
	SignalFeatures   []SignalFeature
	Reason           string
	ReplyTier        string
	MaxReplySegments int
	FollowupAllowed  bool
}

func energyTargetForMode(mode ConversationMode, baseline float64) float64 {
	if baseline <= 0 {
		baseline = defaultEnergyBaseline
	}
	switch mode {
	case ModeCoolingDown:
		return defaultCoolingEnergyTarget
	case ModeAddressed:
		return defaultAddressedEnergyTarget
	case ModeInThread:
		return defaultInThreadEnergyTarget
	case ModeWaitingUser:
		return defaultWaitingUserEnergyTarget
	default:
		return baseline
	}
}

func clampEnergy(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return value
	}
}

func roundEnergy(value float64) int {
	return int(math.Round(clampEnergy(value)))
}

func energyBand(value float64) string {
	value = clampEnergy(value)
	switch {
	case value < 40:
		return "low"
	case value < 65:
		return "medium"
	default:
		return "high"
	}
}

func (s *immersiveSession) settleEnergyLocked(now time.Time, reason string) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.ensureBehaviorDefaultsLocked(now)
	if s.lastEnergyUpdateAt.IsZero() {
		s.lastEnergyUpdateAt = now
		return
	}
	if now.Before(s.lastEnergyUpdateAt) {
		s.lastEnergyUpdateAt = now
		return
	}
	elapsed := now.Sub(s.lastEnergyUpdateAt)
	if elapsed <= 0 {
		return
	}
	before := s.energy
	diff := s.energyTarget - s.energy
	if diff != 0 {
		step := math.Min(math.Abs(diff), elapsed.Seconds()*defaultEnergyRecoveryPerSecond)
		if diff > 0 {
			s.energy += step
		} else {
			s.energy -= step
		}
	}
	s.energy = clampEnergy(s.energy)
	s.lastEnergyUpdateAt = now
	if math.Abs(s.energy-before) >= 0.05 {
		if trimmed := strings.TrimSpace(reason); trimmed != "" {
			s.energyLastDeltaReason = trimmed
		} else {
			s.energyLastDeltaReason = "time_recovery"
		}
	}
}

func (s *immersiveSession) applyTrafficNudgeLocked(amount float64, reason string) {
	if s == nil || amount <= 0 {
		return
	}
	s.energy = clampEnergy(math.Min(s.energyTarget, s.energy+amount))
	s.energyLastDeltaReason = strings.TrimSpace(reason)
}

func (s *immersiveSession) raiseEnergyTowardsTargetLocked(amount float64, reason string) {
	if s == nil || amount <= 0 {
		return
	}
	s.energy = clampEnergy(math.Min(s.energyTarget, s.energy+amount))
	s.energyLastDeltaReason = strings.TrimSpace(reason)
}

func (s *immersiveSession) maybeFastRecoveryLocked(chance, boost float64, reason string, now time.Time) bool {
	if s == nil || chance <= 0 || boost <= 0 {
		return false
	}
	if s.energy >= s.energyTarget {
		return false
	}
	if rand.Float64() >= chance {
		return false
	}
	s.raiseEnergyTowardsTargetLocked(boost, reason)
	s.recordFastRecoveryLocked(reason, now)
	return true
}

func (s *immersiveSession) spendReplyEnergyLocked(reply string, now time.Time, reason string) {
	if s == nil {
		return
	}
	cost := defaultReplyEnergyCost
	if replyLooksLikeQuestion(reply) {
		cost = defaultQuestionReplyEnergyCost
	}
	s.energy = clampEnergy(s.energy - cost)
	s.lastEnergyUpdateAt = now
	s.energyLastDeltaReason = strings.TrimSpace(reason)
}

func (s *immersiveSession) evaluateSpeakGateLocked(sessionKey string, meta queueMeta, now time.Time) (behaviorSnapshot, speakGateDecision) {
	if s == nil {
		return behaviorSnapshot{}, speakGateDecision{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.ensureBehaviorDefaultsLocked(now)
	s.settleEnergyLocked(now, "lazy_gate_recovery")
	s.decayBehaviorLocked(now)

	snapshot := s.snapshotBehaviorLocked(now)
	score := scoreSignals(sessionKey, meta, snapshot, now)
	strongCall := score.Band == BandHighPriority

	switch score.Band {
	case BandHighPriority:
		s.raiseEnergyTowardsTargetLocked(defaultHighPriorityScoreBoost, "high_priority_score_boost")
	case BandEngage:
		s.applyTrafficNudgeLocked(defaultEngageScoreNudge, "engage_score_nudge")
	}

	allow := false
	reason := "signal_below_threshold"
	switch {
	case score.Band == BandHighPriority:
		allow = true
		reason = "high_priority_signal"
	case snapshot.Mode == ModeInThread && sameSpeaker(meta.LastSpeaker, snapshot.FocusSpeaker) && snapshot.EnergyValue >= defaultSpeakCloseThreshold:
		allow = true
		reason = "active_thread"
	case score.Band == BandEngage && snapshot.EnergyValue >= defaultSpeakCloseThreshold:
		allow = true
		reason = "engage_signal"
	case score.Band == BandObserve:
		energyThreshold := defaultSpeakOpenThreshold
		if snapshot.SpeakGateOpen {
			energyThreshold = defaultSpeakCloseThreshold
		}
		if snapshot.EnergyValue >= energyThreshold {
			allow = true
			reason = "observe_energy_pass"
		}
	}

	s.speakGateOpen = allow
	snapshot = s.snapshotBehaviorLocked(now)
	decision := speakGateDecision{
		Allow:          allow,
		StrongCall:     strongCall,
		SignalScore:    score.TotalScore,
		SignalBand:     score.Band,
		SignalFeatures: score.Features,
		Reason:         reason,
	}
	decision.ReplyTier = replyTierForSnapshot(snapshot, strongCall)
	decision.MaxReplySegments = maxReplySegmentsForTier(decision.ReplyTier)
	decision.FollowupAllowed = followupAllowedForSnapshot(snapshot, decision.ReplyTier)
	s.recordSpeakGateLocked(decision, now)
	return snapshot, decision
}

func replyTierForSnapshot(snapshot behaviorSnapshot, strongCall bool) string {
	switch {
	case snapshot.Mode == ModeCoolingDown || snapshot.Mode == ModeWaitingUser || snapshot.EnergyBand == "low":
		return "brief"
	case strongCall || snapshot.Mode == ModeInThread || snapshot.EnergyBand == "high":
		return "engaged"
	default:
		return "normal"
	}
}

func maxReplySegmentsForTier(tier string) int {
	switch tier {
	case "brief":
		return 1
	case "engaged":
		return 3
	default:
		return 2
	}
}

func followupAllowedForSnapshot(snapshot behaviorSnapshot, tier string) bool {
	if snapshot.PendingQuestion || snapshot.Mode == ModeCoolingDown || snapshot.Mode == ModeWaitingUser {
		return false
	}
	if snapshot.EnergyBand == "low" || tier == "brief" {
		return false
	}
	return snapshot.Mode == ModeAddressed || snapshot.Mode == ModeInThread
}
