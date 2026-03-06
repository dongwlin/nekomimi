package contextassemble

import (
	"strings"
	"testing"
)

func TestFormatImmersiveContext_Nil(t *testing.T) {
	result := FormatImmersiveContext(nil)
	if result != "" {
		t.Fatalf("expected empty string for nil context, got %q", result)
	}
}

func TestFormatImmersiveContext_AllFields(t *testing.T) {
	ic := &ImmersiveContext{
		MessagesCount:  5,
		Participants:   []string{"alice", "bob"},
		MentionsToBot:  2,
		AddressedToBot: 1,
		QuestionsCount: 3,
		LastSpeaker:    "bob",
		TimeSpanMS:     4200,
		Transcript:     "- [alice]: hello\n- [bob]: hi",
	}
	result := FormatImmersiveContext(ic)

	checks := []string{
		"messages_count: 5",
		"participants: [alice,bob]",
		"mentions_to_bot: 2",
		"addressed_to_bot: 1",
		"questions_count: 3",
		"last_speaker: bob",
		"time_span_ms: 4200",
		"transcript:",
		"- [alice]: hello",
		"- [bob]: hi",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("expected %q in output, got:\n%s", check, result)
		}
	}
}

func TestFormatImmersiveContext_EmptyTranscript(t *testing.T) {
	ic := &ImmersiveContext{
		MessagesCount: 0,
		LastSpeaker:   "unknown",
	}
	result := FormatImmersiveContext(ic)
	if strings.Contains(result, "transcript:") {
		t.Fatalf("empty transcript should not produce transcript section, got:\n%s", result)
	}
}

func TestFormatImmersiveContext_ZeroValues(t *testing.T) {
	ic := &ImmersiveContext{}
	result := FormatImmersiveContext(ic)
	if !strings.Contains(result, "messages_count: 0") {
		t.Fatalf("expected zero messages_count, got:\n%s", result)
	}
	if !strings.Contains(result, "mentions_to_bot: 0") {
		t.Fatalf("expected zero mentions_to_bot, got:\n%s", result)
	}
}
