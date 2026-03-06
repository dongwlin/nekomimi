package contextassemble

import (
	"strconv"
	"strings"
)

const BlockImmersiveSignals = "immersive_signals"
const BlockImmersiveState = "immersive_state"
const BlockImmersiveBatch = "immersive_batch"
const BlockImmersiveEvents = "immersive_events"

// ImmersiveContext carries batch-scoped state and signals from the immersive
// layer so the LLM pipeline can merge persistent history with current-turn
// incremental context as a single block-based prompt.
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
	SignalBand             string
	SignalFeatureSummary   string
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
	SystemEventSummary     string
}

// RenderImmersiveBlocks converts immersive incremental state into stable prompt blocks.
func RenderImmersiveBlocks(ic *ImmersiveContext) []Block {
	if ic == nil {
		return nil
	}
	blocks := []Block{
		{
			Name:    BlockImmersiveState,
			Content: formatImmersiveState(ic),
		},
		{
			Name:    BlockImmersiveBatch,
			Content: formatImmersiveBatch(ic),
		},
		{
			Name:    BlockImmersiveSignals,
			Content: formatImmersiveSignals(ic),
		},
	}
	if content := strings.TrimSpace(ic.SystemEventSummary); content != "" {
		blocks = append(blocks, Block{
			Name:    BlockImmersiveEvents,
			Content: content,
		})
	}
	return blocks
}

// FormatImmersiveContext renders immersive blocks into a readable multi-block string.
func FormatImmersiveContext(ic *ImmersiveContext) string {
	if ic == nil {
		return ""
	}
	blocks := RenderImmersiveBlocks(ic)
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		content := strings.TrimSpace(block.Content)
		if content == "" {
			continue
		}
		parts = append(parts, "["+block.Name+"]\n"+content)
	}
	return strings.Join(parts, "\n\n")
}

func formatImmersiveState(ic *ImmersiveContext) string {
	var b strings.Builder
	b.WriteString("conversation_mode: ")
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
	return b.String()
}

func formatImmersiveBatch(ic *ImmersiveContext) string {
	var b strings.Builder
	b.WriteString("messages_count: ")
	b.WriteString(strconv.Itoa(ic.MessagesCount))
	b.WriteString("\nparticipants: [")
	b.WriteString(strings.Join(ic.Participants, ","))
	b.WriteString("]\nlast_speaker: ")
	b.WriteString(strings.TrimSpace(ic.LastSpeaker))
	b.WriteString("\ntime_span_ms: ")
	b.WriteString(strconv.FormatInt(ic.TimeSpanMS, 10))
	return b.String()
}

func formatImmersiveSignals(ic *ImmersiveContext) string {
	var b strings.Builder
	b.WriteString("mentions_to_bot: ")
	b.WriteString(strconv.Itoa(ic.MentionsToBot))
	b.WriteString("\naddressed_to_bot: ")
	b.WriteString(strconv.Itoa(ic.AddressedToBot))
	b.WriteString("\nquestions_count: ")
	b.WriteString(strconv.Itoa(ic.QuestionsCount))
	b.WriteString("\nsignal_score: ")
	b.WriteString(strconv.Itoa(ic.SignalScore))
	b.WriteString("\nsignal_band: ")
	b.WriteString(strings.TrimSpace(ic.SignalBand))
	b.WriteString("\nstrong_call: ")
	b.WriteString(strconv.FormatBool(ic.StrongCall))
	if summary := strings.TrimSpace(ic.SignalFeatureSummary); summary != "" {
		b.WriteString("\nsignal_features: ")
		b.WriteString(summary)
	}
	return b.String()
}
