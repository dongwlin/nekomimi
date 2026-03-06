package contextassemble

import (
	"strconv"
	"strings"
)

const BlockImmersiveSignals = "immersive_signals"

// ImmersiveContext carries batch-level signals from the immersive layer that
// must reach the control-intent and reply LLM calls. Without this, the
// assembler's chatStore content would overwrite the fallback content and
// signals like mentions_to_bot would never be seen by the model.
type ImmersiveContext struct {
	MessagesCount          int
	Participants           []string
	MentionsToBot          int
	AddressedToBot         int
	QuestionsCount         int
	LastSpeaker            string
	TimeSpanMS             int64
	ConversationMode       string
	FocusSpeaker           string
	EnergyValue            int
	EnergyBaseline         int
	EnergyTarget           int
	EnergyBand             string
	SpeakGateOpen          bool
	SignalScore            int
	StrongCall             bool
	LastBotReplyMS         int64
	LastAddressedMS        int64
	PendingQuestion        bool
	FollowupDueMS          int64
	FollowupBudget         int
	NextColdOpenEligibleMS int64
	ReplyTier              string
	MaxReplySegments       int
	FollowupAllowed        bool
	Transcript             string
}

// FormatImmersiveContext renders the context as a human-readable key-value
// block suitable for injection into the assembled prompt.
func FormatImmersiveContext(ic *ImmersiveContext) string {
	if ic == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("messages_count: ")
	b.WriteString(strconv.Itoa(ic.MessagesCount))
	b.WriteString("\nparticipants: [")
	b.WriteString(strings.Join(ic.Participants, ","))
	b.WriteString("]\nmentions_to_bot: ")
	b.WriteString(strconv.Itoa(ic.MentionsToBot))
	b.WriteString("\naddressed_to_bot: ")
	b.WriteString(strconv.Itoa(ic.AddressedToBot))
	b.WriteString("\nquestions_count: ")
	b.WriteString(strconv.Itoa(ic.QuestionsCount))
	b.WriteString("\nlast_speaker: ")
	b.WriteString(strings.TrimSpace(ic.LastSpeaker))
	b.WriteString("\ntime_span_ms: ")
	b.WriteString(strconv.FormatInt(ic.TimeSpanMS, 10))
	b.WriteString("\nconversation_mode: ")
	b.WriteString(strings.TrimSpace(ic.ConversationMode))
	b.WriteString("\nfocus_speaker: ")
	b.WriteString(strings.TrimSpace(ic.FocusSpeaker))
	b.WriteString("\nenergy_value: ")
	b.WriteString(strconv.Itoa(ic.EnergyValue))
	b.WriteString("\nenergy_baseline: ")
	b.WriteString(strconv.Itoa(ic.EnergyBaseline))
	b.WriteString("\nenergy_target: ")
	b.WriteString(strconv.Itoa(ic.EnergyTarget))
	b.WriteString("\nenergy_band: ")
	b.WriteString(strings.TrimSpace(ic.EnergyBand))
	b.WriteString("\nspeak_gate_open: ")
	b.WriteString(strconv.FormatBool(ic.SpeakGateOpen))
	b.WriteString("\nsignal_score: ")
	b.WriteString(strconv.Itoa(ic.SignalScore))
	b.WriteString("\nstrong_call: ")
	b.WriteString(strconv.FormatBool(ic.StrongCall))
	b.WriteString("\nlast_bot_reply_ms: ")
	b.WriteString(strconv.FormatInt(ic.LastBotReplyMS, 10))
	b.WriteString("\nlast_addressed_ms: ")
	b.WriteString(strconv.FormatInt(ic.LastAddressedMS, 10))
	b.WriteString("\npending_question: ")
	b.WriteString(strconv.FormatBool(ic.PendingQuestion))
	b.WriteString("\nfollowup_due_ms: ")
	b.WriteString(strconv.FormatInt(ic.FollowupDueMS, 10))
	b.WriteString("\nfollowup_budget: ")
	b.WriteString(strconv.Itoa(ic.FollowupBudget))
	b.WriteString("\nnext_cold_open_eligible_ms: ")
	b.WriteString(strconv.FormatInt(ic.NextColdOpenEligibleMS, 10))
	b.WriteString("\nreply_tier: ")
	b.WriteString(strings.TrimSpace(ic.ReplyTier))
	b.WriteString("\nmax_reply_segments: ")
	b.WriteString(strconv.Itoa(ic.MaxReplySegments))
	b.WriteString("\nfollowup_allowed: ")
	b.WriteString(strconv.FormatBool(ic.FollowupAllowed))
	if transcript := strings.TrimSpace(ic.Transcript); transcript != "" {
		b.WriteString("\ntranscript:\n")
		b.WriteString(transcript)
	}
	return b.String()
}
