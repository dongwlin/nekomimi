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
		MessagesCount:          5,
		Participants:           []string{"alice", "bob"},
		MentionsToBot:          2,
		AddressedToBot:         1,
		QuestionsCount:         3,
		LastSpeaker:            "bob",
		TimeSpanMS:             4200,
		ConversationMode:       "in_thread",
		FocusSpeaker:           "alice",
		EnergyValue:            68,
		EnergyBaseline:         45,
		EnergyTarget:           70,
		EnergyBand:             "high",
		SpeakGateOpen:          true,
		SignalScore:            9,
		StrongCall:             true,
		LastBotReplyMS:         1200,
		LastAddressedMS:        300,
		PendingQuestion:        true,
		FollowupDueMS:          90000,
		FollowupBudget:         1,
		NextColdOpenEligibleMS: 600000,
		ReplyTier:              "engaged",
		MaxReplySegments:       3,
		FollowupAllowed:        false,
		Transcript:             "- [alice]: hello\n- [bob]: hi",
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
		"conversation_mode: in_thread",
		"focus_speaker: alice",
		"energy_value: 68",
		"energy_baseline: 45",
		"energy_target: 70",
		"energy_band: high",
		"speak_gate_open: true",
		"signal_score: 9",
		"strong_call: true",
		"last_bot_reply_ms: 1200",
		"last_addressed_ms: 300",
		"pending_question: true",
		"followup_due_ms: 90000",
		"followup_budget: 1",
		"next_cold_open_eligible_ms: 600000",
		"reply_tier: engaged",
		"max_reply_segments: 3",
		"followup_allowed: false",
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
