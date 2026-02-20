package immersive

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestBuildCombinedInput_ContainsStructuredMeta(t *testing.T) {
	base := time.Date(2026, 2, 10, 21, 30, 5, 0, time.Local)
	queue := []queuedMessage{
		{
			text:             "hello",
			speaker:          "name=alice",
			ts:               base,
			chars:            5,
			isQuestion:       true,
			isAddressedToBot: true,
		},
		{
			text:             "here",
			speaker:          "name=bob",
			ts:               base.Add(2 * time.Second),
			chars:            4,
			isMentionBot:     true,
			isAddressedToBot: true,
		},
	}

	input := buildCombinedInput(queue, botIdentity{})
	for _, token := range []string{
		"batch_meta:",
		"now_date:",
		"now_time:",
		"bot_names:",
		"bot_primary_name:",
		"messages_count: 2",
		"participants: [name=alice,name=bob]",
		"mentions_to_bot: 1",
		"questions_count: 1",
		"transcript:",
		"- [name=alice;time=2026-02-10 21:30:05]: hello",
	} {
		if !strings.Contains(input, token) {
			t.Fatalf("structured input missing %q, got:\n%s", token, input)
		}
	}
}

func TestIsControlHeaderProtocolError(t *testing.T) {
	if !isControlHeaderProtocolError(errControlHeaderInvalid) {
		t.Fatal("expected parser error to be treated as protocol error")
	}

	wrapped := errors.New("control header protocol: something wrong")
	if !isControlHeaderProtocolError(wrapped) {
		t.Fatal("expected wrapped control header protocol message to be detected")
	}

	if isControlHeaderProtocolError(errors.New("network timeout")) {
		t.Fatal("expected non-protocol error to be rejected")
	}
}

func TestRecordTimelineEvent_AppendsWithoutQueueing(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{}, nil, []string{"neko"})
	sessionKey := "group:1"
	buffer.RecordTimelineEvent(sessionKey, "user poked bot", "name=alice")
	buffer.RecordTimelineEvent(sessionKey, "bot replied", "assistant")

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.queue) != 0 {
		t.Fatalf("expected queue to remain empty, got %d", len(state.queue))
	}
	if len(state.timeline) != 2 {
		t.Fatalf("expected timeline size 2, got %d", len(state.timeline))
	}
	if state.timeline[0].speaker != "name=alice" {
		t.Fatalf("unexpected first speaker: %q", state.timeline[0].speaker)
	}
	if state.timeline[1].speaker != "assistant" {
		t.Fatalf("unexpected second speaker: %q", state.timeline[1].speaker)
	}
}
