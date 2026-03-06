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
	MessagesCount  int
	Participants   []string
	MentionsToBot  int
	AddressedToBot int
	QuestionsCount int
	LastSpeaker    string
	TimeSpanMS     int64
	Transcript     string
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
	if transcript := strings.TrimSpace(ic.Transcript); transcript != "" {
		b.WriteString("\ntranscript:\n")
		b.WriteString(transcript)
	}
	return b.String()
}
