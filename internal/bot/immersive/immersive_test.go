package immersive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
	logpkg "github.com/rs/zerolog/log"
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

func TestIsControlIntentProtocolError_Integration(t *testing.T) {
	if !isControlIntentProtocolError(llmintent.ErrProtocol) {
		t.Fatal("expected protocol sentinel error to be detected")
	}
	wrapped := fmt.Errorf("wrapped: %w", llmintent.ErrProtocol)
	if !isControlIntentProtocolError(wrapped) {
		t.Fatal("expected wrapped protocol error to be detected")
	}
	if isControlIntentProtocolError(fmt.Errorf("wrapped: %w", fmt.Errorf("network timeout"))) {
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

	if len(state.nextBatch) != 0 {
		t.Fatalf("expected next batch to remain empty, got %d", len(state.nextBatch))
	}
	if len(state.runtimeBuffer) != 2 {
		t.Fatalf("expected runtime buffer size 2, got %d", len(state.runtimeBuffer))
	}
	if state.runtimeBuffer[0].speaker != "name=alice" {
		t.Fatalf("unexpected first speaker: %q", state.runtimeBuffer[0].speaker)
	}
	if state.runtimeBuffer[1].speaker != "assistant" {
		t.Fatalf("unexpected second speaker: %q", state.runtimeBuffer[1].speaker)
	}
}

func TestFlush_IntentProtocolErrorFirstRoundWaitThenFollowupSkip(t *testing.T) {
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		return scriptedResponse{
			JSONBody: responsesOutputTextJSON("not-json"),
		}
	})
	defer server.Close()

	t.Run("first round protocol error uses wait", func(t *testing.T) {
		buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
		seedQueueForFlushTest(buffer, sessionKey, 0)
		buffer.flush(sessionKey)

		state := buffer.session(sessionKey)
		state.mu.Lock()
		defer state.mu.Unlock()
		if got := len(state.nextBatch); got != 1 {
			t.Fatalf("expected requeue on first protocol error, got %d", got)
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
		buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
		seedQueueForFlushTest(buffer, sessionKey, 1)
		buffer.flush(sessionKey)

		state := buffer.session(sessionKey)
		state.mu.Lock()
		defer state.mu.Unlock()
		if got := len(state.nextBatch); got != 0 {
			t.Fatalf("expected queue dropped after repeated protocol error, got %d", got)
		}
		if state.waitRounds != 0 {
			t.Fatalf("expected waitRounds reset, got %d", state.waitRounds)
		}
	})
}

func TestFlush_IntentWaitFromModelRequeues(t *testing.T) {
	intent := mustMarshalJSON(t, map[string]any{
		"action":  "wait",
		"wait_ms": 120,
		"reason":  "user is still typing",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		return scriptedResponse{JSONBody: responsesOutputTextJSON(intent)}
	})
	defer server.Close()

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0)
	buffer.flush(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	if got := len(state.nextBatch); got != 1 {
		t.Fatalf("expected wait action to requeue batch, got %d", got)
	}
	if state.waitRounds != 1 {
		t.Fatalf("expected waitRounds=1, got %d", state.waitRounds)
	}
	if state.timer == nil {
		t.Fatal("expected wait timer to be scheduled")
	}
	state.timer.Stop()
	state.timer = nil
}

func TestFlush_IntentReplyGeneratesAndPersistsAssistant(t *testing.T) {
	var callCount int64
	control := mustMarshalJSON(t, map[string]any{
		"action": "reply",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		atomic.StoreInt64(&callCount, call)
		if !stream {
			return scriptedResponse{
				JSONBody: responsesOutputTextJSON(control),
			}
		}
		return scriptedResponse{
			SSEEvents: []string{
				mustResponsesDeltaEventRaw(t, "ok"),
				`{"type":"response.completed"}`,
			},
		}
	})
	defer server.Close()

	buffer, manager, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0)
	buffer.flush(sessionKey)

	if atomic.LoadInt64(&callCount) != 2 {
		t.Fatalf("expected 2 provider calls (intent+reply), got %d", atomic.LoadInt64(&callCount))
	}
	entries := mustListChatEvents(t, manager, sessionKey, 20)
	foundAssistant := false
	for _, entry := range entries {
		if entry.Role != chatlog.RoleAssistant {
			continue
		}
		if strings.Contains(entry.Content, "ok") {
			foundAssistant = true
			break
		}
	}
	if !foundAssistant {
		t.Fatalf("expected assistant reply 'ok' in history, entries=%+v", entries)
	}
}

func TestFlush_ReplyDelimiterSegmentsAreSentWithoutControlMarker(t *testing.T) {
	control := mustMarshalJSON(t, map[string]any{
		"action": "reply",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		if !stream {
			return scriptedResponse{
				JSONBody: responsesOutputTextJSON(control),
			}
		}
		return scriptedResponse{
			SSEEvents: []string{
				mustResponsesDeltaEventRaw(t, "first\n---\nsecond"),
				`{"type":"response.completed"}`,
			},
		}
	})
	defer server.Close()

	buffer, manager, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0)
	buffer.flush(sessionKey)

	entries := mustListChatEvents(t, manager, sessionKey, 20)
	foundAssistant := false
	for _, entry := range entries {
		if entry.Role != chatlog.RoleAssistant {
			continue
		}
		if strings.Contains(entry.Content, "---") {
			t.Fatalf("assistant history should not contain delimiter marker: %q", entry.Content)
		}
		if strings.Contains(entry.Content, "first") && strings.Contains(entry.Content, "second") {
			foundAssistant = true
		}
	}
	if !foundAssistant {
		t.Fatalf("expected segmented assistant reply in history, entries=%+v", entries)
	}
}

func TestFlush_DecisionLogIncludesModelReasonAndCode(t *testing.T) {
	intent := mustMarshalJSON(t, map[string]any{
		"action":  "wait",
		"wait_ms": 120,
		"reason":  "user is still typing",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		return scriptedResponse{JSONBody: responsesOutputTextJSON(intent)}
	})
	defer server.Close()

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0)

	events := captureLogEvents(t, func() {
		buffer.flush(sessionKey)
	})

	state := buffer.session(sessionKey)
	state.mu.Lock()
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.mu.Unlock()

	found := false
	for _, event := range events {
		if event["message"] != "immersive control decision evaluated" {
			continue
		}
		if toString(event["action"]) != string(controlActionWait) {
			continue
		}
		if toString(event["reason"]) != "user is still typing" {
			t.Fatalf("unexpected decision reason: %#v", event["reason"])
		}
		if toString(event["reason_code"]) != "model" {
			t.Fatalf("unexpected decision reason_code: %#v", event["reason_code"])
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("missing decision log with action=%q, events=%d", controlActionWait, len(events))
	}
}

func TestFlush_PersistsAtomicUserEventsWithoutBatchEnvelope(t *testing.T) {
	control := mustMarshalJSON(t, map[string]any{
		"action": "reply",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		if !stream {
			return scriptedResponse{
				JSONBody: responsesOutputTextJSON(control),
			}
		}
		return scriptedResponse{
			SSEEvents: []string{
				mustResponsesDeltaEventRaw(t, "ok"),
				`{"type":"response.completed"}`,
			},
		}
	})
	defer server.Close()

	buffer, manager, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0,
		queuedMessage{text: "hello-1", speaker: "name=alice", ts: time.Now().Add(-2 * time.Second), chars: len([]rune("hello-1"))},
		queuedMessage{text: "hello-2", speaker: "name=bob", ts: time.Now().Add(-1 * time.Second), chars: len([]rune("hello-2"))},
	)
	buffer.flush(sessionKey)

	entries := mustListChatEvents(t, manager, sessionKey, 20)
	userCount := 0
	assistantCount := 0
	for _, entry := range entries {
		if strings.Contains(entry.Content, "batch_meta:") || strings.Contains(entry.Content, "transcript:") {
			t.Fatalf("history should not include batch envelope markers, got: %q", entry.Content)
		}
		switch entry.Role {
		case chatlog.RoleUser:
			userCount++
		case chatlog.RoleAssistant:
			assistantCount++
		}
	}
	if userCount != 2 {
		t.Fatalf("expected 2 user entries, got %d", userCount)
	}
	if assistantCount != 1 {
		t.Fatalf("expected 1 assistant entry, got %d", assistantCount)
	}
}

func TestFlush_WaitRetryDoesNotDuplicateUserPersistence(t *testing.T) {
	var callCount int64
	waitIntent := mustMarshalJSON(t, map[string]any{
		"action":  "wait",
		"wait_ms": 10,
		"reason":  "still collecting context",
	})
	replyIntent := mustMarshalJSON(t, map[string]any{
		"action": "reply",
	})
	server := newScriptedResponsesServer(t, func(call int64, stream bool, body map[string]any) scriptedResponse {
		callNo := atomic.AddInt64(&callCount, 1)
		if !stream {
			text := waitIntent
			if callNo >= 2 {
				text = replyIntent
			}
			return scriptedResponse{JSONBody: responsesOutputTextJSON(text)}
		}
		return scriptedResponse{
			SSEEvents: []string{
				mustResponsesDeltaEventRaw(t, "ok"),
				`{"type":"response.completed"}`,
			},
		}
	})
	defer server.Close()

	buffer, manager, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0)
	buffer.flush(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.mu.Unlock()

	buffer.flush(sessionKey)

	entries := mustListChatEvents(t, manager, sessionKey, 20)
	userCount := 0
	for _, entry := range entries {
		if entry.Role == chatlog.RoleUser {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("expected exactly 1 persisted user entry after WAIT retry, got %d", userCount)
	}
}

func TestClear_ResetsRuntimeBuffers(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{}, nil, []string{"neko"})
	sessionKey := "group:clear"
	seedQueueForFlushTest(buffer, sessionKey, 1,
		queuedMessage{text: "hello", speaker: "name=alice", ts: time.Now(), chars: len([]rune("hello"))},
	)
	buffer.RecordTimelineEvent(sessionKey, "assistant note", "assistant")

	buffer.Clear(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.nextBatch) != 0 || len(state.processingBatch) != 0 || len(state.runtimeBuffer) != 0 {
		t.Fatalf("expected all runtime buffers to be empty, got next=%d processing=%d runtime=%d", len(state.nextBatch), len(state.processingBatch), len(state.runtimeBuffer))
	}
	if state.waitRounds != 0 {
		t.Fatalf("expected waitRounds reset to 0, got %d", state.waitRounds)
	}
}

type scriptedResponse struct {
	StatusCode int
	JSONBody   string
	SSEEvents  []string
}

func newScriptedResponsesServer(t *testing.T, script func(call int64, stream bool, body map[string]any) scriptedResponse) *httptest.Server {
	t.Helper()
	var callCount int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		call := atomic.AddInt64(&callCount, 1)
		stream, _ := body["stream"].(bool)

		resp := script(call, stream, body)
		status := resp.StatusCode
		if status == 0 {
			status = http.StatusOK
		}

		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(status)
			events := resp.SSEEvents
			if len(events) == 0 {
				events = []string{`{"type":"response.completed"}`}
			}
			for _, event := range events {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			}
			return
		}

		if strings.TrimSpace(resp.JSONBody) == "" {
			resp.JSONBody = responsesOutputTextJSON(`{""action":"skip","reason":"default"}`)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp.JSONBody))
	}))
}

func responsesOutputTextJSON(text string) string {
	return fmt.Sprintf(`{"output":[{"content":[{"type":"output_text","text":%s}]}]}`, mustJSONString(text))
}

func mustJSONString(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

func mustResponsesDeltaEventRaw(t *testing.T, delta string) string {
	t.Helper()
	event := map[string]any{
		"type":  "response.output_text.delta",
		"delta": delta,
	}
	return mustMarshalJSON(t, event)
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json failed: %v", err)
	}
	return string(data)
}

func captureLogEvents(t *testing.T, fn func()) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	previous := logpkg.Logger
	logpkg.Logger = logpkg.Output(&buf)
	defer func() {
		logpkg.Logger = previous
	}()

	fn()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("unmarshal log event failed: %v, line=%q", err, line)
		}
		events = append(events, payload)
	}
	return events
}

func toString(value any) string {
	text, _ := value.(string)
	return text
}

func newImmersiveBufferForFlushTest(t *testing.T, apiURL string, cfg config.ImmersiveConfig) (*ImmersiveBuffer, *llm.Manager, string) {
	t.Helper()
	sessionKey := "group:flush-test"
	manager := llm.NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      apiURL,
		Key:      "test-key",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}, llm.ManagerDeps{})
	manager.SetImmersive(sessionKey, true)
	buffer := NewImmersiveBuffer(cfg, manager, []string{"neko"})
	return buffer, manager, sessionKey
}

func seedQueueForFlushTest(buffer *ImmersiveBuffer, sessionKey string, waitRounds int, messages ...queuedMessage) {
	if len(messages) == 0 {
		messages = []queuedMessage{
			{
				text:    "hello",
				speaker: "name=alice",
				ts:      time.Now(),
				chars:   len([]rune("hello")),
			},
		}
	}
	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.nextBatch = make([]queuedMessage, len(messages))
	copy(state.nextBatch, messages)
	state.nextBatchChars = sumQueueChars(state.nextBatch)
	state.runtimeBuffer = append(state.runtimeBuffer[:0], messages...)
	state.waitRounds = waitRounds
	state.lastCtx = nil
}

func mustListChatEvents(t *testing.T, manager *llm.Manager, sessionKey string, limit int) []chatlog.Entry {
	t.Helper()
	result, err := manager.ListChatEvents(sessionKey, chatlog.ListOptions{Limit: limit})
	if err != nil {
		t.Fatalf("list chat events failed: %v", err)
	}
	return result.Entries
}

func TestEnqueue_ConcurrentDoesNotPanic(t *testing.T) {
	manager := llm.NewManager(config.LLMConfig{
		Enabled:  false,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		Key:      "test-key",
	}, llm.ManagerDeps{})
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{}, manager, []string{"neko"})
	sessionKey := "group:concurrent"
	const goroutines = 20
	const messagesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < messagesPerGoroutine; i++ {
				text := fmt.Sprintf("msg-%d-%d", id, i)
				buffer.Enqueue(nil, sessionKey, text, fmt.Sprintf("name=user%d", id), false)
			}
		}(g)
	}
	wg.Wait()

	time.Sleep(50 * time.Millisecond)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.runtimeBuffer) == 0 {
		t.Fatal("expected messages in runtime buffer after concurrent enqueue")
	}
}
