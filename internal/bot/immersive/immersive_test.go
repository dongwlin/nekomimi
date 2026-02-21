package immersive

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
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

	wrapped := fmt.Errorf("%w: %w", errControlHeaderProtocol, errControlHeaderMissingNewline)
	if !isControlHeaderProtocolError(wrapped) {
		t.Fatal("expected wrapped control header protocol error to be detected")
	}

	if isControlHeaderProtocolError(errors.New("control header protocol: something wrong")) {
		t.Fatal("expected plain string message to be rejected")
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

func TestFlush_ProtocolErrorFirstRoundWaitThenFollowupSkip(t *testing.T) {
	t.Run("first round protocol error uses wait", func(t *testing.T) {
		server := newResponsesSSEServer(t, []string{
			`{"type":"response.output_text.delta","delta":"REPLY"}`,
			`{"type":"response.completed"}`,
		})
		defer server.Close()

		buffer, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses")
		seedQueueForFlushTest(buffer, sessionKey, 0)

		buffer.flush(sessionKey)

		state := buffer.session(sessionKey)
		state.mu.Lock()
		defer state.mu.Unlock()
		if got := len(state.queue); got != 1 {
			t.Fatalf("expected queue to be requeued on wait, got %d", got)
		}
		if state.waitRounds != 1 {
			t.Fatalf("expected waitRounds=1, got %d", state.waitRounds)
		}
		if state.timer == nil {
			t.Fatal("expected wait timer to be scheduled")
		}
		state.timer.Stop()
		state.timer = nil
	})

	t.Run("follow-up protocol error uses skip", func(t *testing.T) {
		server := newResponsesSSEServer(t, []string{
			`{"type":"response.output_text.delta","delta":"REPLY"}`,
			`{"type":"response.completed"}`,
		})
		defer server.Close()

		buffer, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses")
		seedQueueForFlushTest(buffer, sessionKey, 1)

		buffer.flush(sessionKey)

		state := buffer.session(sessionKey)
		state.mu.Lock()
		defer state.mu.Unlock()
		if got := len(state.queue); got != 0 {
			t.Fatalf("expected queue to be dropped on skip, got %d", got)
		}
		if state.waitRounds != 0 {
			t.Fatalf("expected waitRounds reset to 0 after skip, got %d", state.waitRounds)
		}
	})
}

func TestFlush_NonProtocolStreamErrorSkips(t *testing.T) {
	server := newResponsesSSEServer(t, []string{
		`{"type":"response.error","error":{"message":"boom"}}`,
	})
	defer server.Close()

	buffer, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses")
	seedQueueForFlushTest(buffer, sessionKey, 0)

	buffer.flush(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	if got := len(state.queue); got != 0 {
		t.Fatalf("expected queue to be dropped on non-protocol stream error, got %d", got)
	}
	if state.waitRounds != 0 {
		t.Fatalf("expected waitRounds reset to 0, got %d", state.waitRounds)
	}
}

func TestFlush_WaitRoundsLimitConvertsWaitToSkip(t *testing.T) {
	server := newResponsesSSEServer(t, []string{
		`{"type":"response.output_text.delta","delta":"WAIT:100\n"}`,
		`{"type":"response.completed"}`,
	})
	defer server.Close()

	buffer, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses")
	seedQueueForFlushTest(buffer, sessionKey, maxImmersiveWaitRounds)

	buffer.flush(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	if got := len(state.queue); got != 0 {
		t.Fatalf("expected queue to be dropped when wait limit is reached, got %d", got)
	}
	if state.waitRounds != 0 {
		t.Fatalf("expected waitRounds reset to 0 after limit skip, got %d", state.waitRounds)
	}
}

func newResponsesSSEServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, event := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}

func newImmersiveBufferForFlushTest(t *testing.T, apiURL string) (*ImmersiveBuffer, string) {
	t.Helper()
	sessionKey := "group:flush-test"
	manager := llm.NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      apiURL,
		Key:      "test-key",
	})
	manager.SetImmersive(sessionKey, true)
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{}, manager, []string{"neko"})
	return buffer, sessionKey
}

func seedQueueForFlushTest(buffer *ImmersiveBuffer, sessionKey string, waitRounds int) {
	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.queue = []queuedMessage{
		{
			text:    "hello",
			speaker: "name=alice",
			ts:      time.Now(),
			chars:   len([]rune("hello")),
		},
	}
	state.queueChars = len([]rune("hello"))
	state.waitRounds = waitRounds
	state.lastCtx = nil
}
