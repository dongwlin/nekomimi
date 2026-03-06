package immersive

import (
	"strconv"
	"strings"
	"time"
	"unicode"
)

// SignalBand classifies the overall signal strength into decision bands
// used by the speak gate and energy correction.
type SignalBand string

const (
	BandIgnore       SignalBand = "ignore"
	BandObserve      SignalBand = "observe"
	BandEngage       SignalBand = "engage"
	BandHighPriority SignalBand = "high_priority"
)

// NicknamePosition describes where a bot nickname appears in the message.
// Values are ordered by signal strength (higher = stronger).
type NicknamePosition int

const (
	NickNotFound NicknamePosition = iota
	NickMiddle
	NickEnd
	NickStart
	NickIsolated
)

// SignalFeature represents one detected feature contributing to the signal score.
type SignalFeature struct {
	Name   string
	Points int
}

// SignalScore is the aggregate result of signal evaluation for a message batch.
type SignalScore struct {
	TotalScore int
	Band       SignalBand
	Features   []SignalFeature
}

const (
	featurePrivateSession    = "private_session"
	featureExplicitMention   = "explicit_mention"
	featureNicknameIsolated  = "nickname_isolated"
	featureNicknameStart     = "nickname_start"
	featureNicknameEnd       = "nickname_end"
	featureNicknameMiddle    = "nickname_middle"
	featureDirectedQuestion  = "directed_question"
	featureAmbientQuestion   = "ambient_question"
	featureFocusSpeakerMatch = "focus_speaker_match"
	featureReplyToPendingQ   = "reply_to_pending_question"
	featureModeAddressed     = "mode_addressed"
	featureModeInThread      = "mode_in_thread"
	featureModeWaitingFocus  = "mode_waiting_focus"
	featureModeWaitingNoise  = "mode_waiting_noise"
	featureModeCooling       = "mode_cooling"
	featureBatchVolume       = "batch_volume"
	featureRecentReply       = "recent_reply_penalty"
)

const (
	bandThresholdObserve      = 2
	bandThresholdEngage       = 5
	bandThresholdHighPriority = 10

	scorePrivateSession   = 12
	scoreExplicitMention  = 10
	scoreNicknameIsolated = 8
	scoreNicknameStart    = 6
	scoreNicknameEnd      = 3
	scoreNicknameMiddle   = 1
	scoreDirectedQuestion = 4
	scoreAmbientQuestion  = 1
	scoreFocusSpeaker     = 3
	scoreReplyToPendingQ  = 4
	scoreModeAddressed    = 3
	scoreModeInThread     = 5
	scoreModeWaitingFocus = 4
	scoreModeWaitingNoise = -3
	scoreModeCooling      = -4
	scoreBatchVolume      = 1
	scoreRecentReply      = -2

	recentReplyWindow = 45 * time.Second
)

// scoreSignals computes a feature-based signal score from batch metadata and
// session behavior state, replacing the older flat computeSignalScore.
func scoreSignals(sessionKey string, meta queueMeta, snapshot behaviorSnapshot, now time.Time) SignalScore {
	var features []SignalFeature
	add := func(name string, points int) {
		if points != 0 {
			features = append(features, SignalFeature{Name: name, Points: points})
		}
	}

	if isPrivateSessionKey(sessionKey) {
		add(featurePrivateSession, scorePrivateSession)
	}

	add(featureExplicitMention, meta.MentionsToBot*scoreExplicitMention)
	add(featureNicknameIsolated, meta.NicknameIsolatedCount*scoreNicknameIsolated)
	add(featureNicknameStart, meta.NicknameStartCount*scoreNicknameStart)
	add(featureNicknameEnd, meta.NicknameEndCount*scoreNicknameEnd)
	add(featureNicknameMiddle, meta.NicknameMiddleCount*scoreNicknameMiddle)

	dirQ := meta.DirectedQuestions
	if dirQ > meta.QuestionsCount {
		dirQ = meta.QuestionsCount
	}
	ambientQ := meta.QuestionsCount - dirQ
	if ambientQ < 0 {
		ambientQ = 0
	}
	add(featureDirectedQuestion, dirQ*scoreDirectedQuestion)
	add(featureAmbientQuestion, ambientQ*scoreAmbientQuestion)

	if sameSpeaker(meta.LastSpeaker, snapshot.FocusSpeaker) {
		add(featureFocusSpeakerMatch, scoreFocusSpeaker)
	}

	switch snapshot.Mode {
	case ModeAddressed:
		add(featureModeAddressed, scoreModeAddressed)
	case ModeInThread:
		add(featureModeInThread, scoreModeInThread)
	case ModeWaitingUser:
		if sameSpeaker(meta.LastSpeaker, snapshot.FocusSpeaker) {
			add(featureModeWaitingFocus, scoreModeWaitingFocus)
		} else {
			add(featureModeWaitingNoise, scoreModeWaitingNoise)
		}
	case ModeCoolingDown:
		add(featureModeCooling, scoreModeCooling)
	}

	if snapshot.PendingQuestion && sameSpeaker(meta.LastSpeaker, snapshot.FocusSpeaker) {
		add(featureReplyToPendingQ, scoreReplyToPendingQ)
	}

	if meta.MessagesCount >= 3 {
		add(featureBatchVolume, scoreBatchVolume)
	}

	if !snapshot.LastBotReplyAt.IsZero() && now.Sub(snapshot.LastBotReplyAt) < recentReplyWindow {
		add(featureRecentReply, scoreRecentReply)
	}

	total := 0
	for _, f := range features {
		total += f.Points
	}
	if total < 0 {
		total = 0
	}

	return SignalScore{
		TotalScore: total,
		Band:       bandFromScore(total),
		Features:   features,
	}
}

func bandFromScore(score int) SignalBand {
	switch {
	case score >= bandThresholdHighPriority:
		return BandHighPriority
	case score >= bandThresholdEngage:
		return BandEngage
	case score >= bandThresholdObserve:
		return BandObserve
	default:
		return BandIgnore
	}
}

// classifyNicknamePosition determines where a nickname appears within text.
// Both text and name must already be lowercased.
func classifyNicknamePosition(text, name string) NicknamePosition {
	core := strings.TrimFunc(text, isNoiseRune)
	if core == name {
		return NickIsolated
	}

	leadStripped := strings.TrimLeftFunc(text, isNoiseRune)
	if strings.HasPrefix(leadStripped, name) {
		return NickStart
	}

	trailStripped := strings.TrimRightFunc(text, isNoiseRune)
	if strings.HasSuffix(trailStripped, name) {
		return NickEnd
	}

	return NickMiddle
}

func isNoiseRune(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// formatSignalFeatures renders hit features into a compact debug string.
func formatSignalFeatures(features []SignalFeature) string {
	if len(features) == 0 {
		return ""
	}
	parts := make([]string, 0, len(features))
	for _, f := range features {
		if f.Points == 0 {
			continue
		}
		sign := "+"
		if f.Points < 0 {
			sign = ""
		}
		parts = append(parts, f.Name+"("+sign+strconv.Itoa(f.Points)+")")
	}
	return strings.Join(parts, ", ")
}
