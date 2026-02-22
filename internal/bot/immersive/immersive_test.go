package immersive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
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

func TestFlush_ProtocolErrorFirstRoundWaitThenFollowupSkip(t *testing.T) {
	t.Run("first round protocol error uses wait", func(t *testing.T) {
		server := newResponsesJSONServer(t, http.StatusOK, `{"output":[{"content":[{"type":"output_text","text":"hello"}]}]}`)
		defer server.Close()

		buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
		seedQueueForFlushTest(buffer, sessionKey, 0)

		buffer.flush(sessionKey)

		state := buffer.session(sessionKey)
		state.mu.Lock()
		defer state.mu.Unlock()
		if got := len(state.nextBatch); got != 1 {
			t.Fatalf("expected next batch to be requeued on wait, got %d", got)
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
		server := newResponsesJSONServer(t, http.StatusOK, `{"output":[{"content":[{"type":"output_text","text":"hello"}]}]}`)
		defer server.Close()

		buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
		seedQueueForFlushTest(buffer, sessionKey, 1)

		buffer.flush(sessionKey)

		state := buffer.session(sessionKey)
		state.mu.Lock()
		defer state.mu.Unlock()
		if got := len(state.nextBatch); got != 0 {
			t.Fatalf("expected next batch to be dropped on skip, got %d", got)
		}
		if state.waitRounds != 0 {
			t.Fatalf("expected waitRounds reset to 0 after skip, got %d", state.waitRounds)
		}
	})
}

func TestFlush_MissingNewlineReplyUsesFallbackParser(t *testing.T) {
	server := newResponsesJSONServer(t, http.StatusOK, `{"output":[{"content":[{"type":"output_text","text":"REPLY: ok"}]}]}`)
	defer server.Close()

	buffer, manager, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0)

	buffer.flush(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	if got := len(state.nextBatch); got != 0 {
		state.mu.Unlock()
		t.Fatalf("expected next batch to be consumed, got %d", got)
	}
	if state.waitRounds != 0 {
		state.mu.Unlock()
		t.Fatalf("expected waitRounds reset to 0, got %d", state.waitRounds)
	}
	state.mu.Unlock()

	entries := mustListChatEvents(t, manager, sessionKey, 10)
	if len(entries) == 0 {
		t.Fatal("expected chat history entries")
	}
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

func TestFlush_ToolLoopDeltaNoiseFallsBackToFinalControlHeader(t *testing.T) {
	sessionKey := "group:flush-tools-noise"
	var callCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		_ = r.Body.Close()

		call := atomic.AddInt64(&callCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		switch call {
		case 1:
			_, _ = fmt.Fprintf(
				w,
				"data: %s\n\n",
				mustResponsesDeltaEventRaw(t, `{"version":"v2","type":"delta","delta":{"text":"thinking\n"}}`+"\n"),
			)
			_, _ = fmt.Fprintf(
				w,
				"data: %s\n\n",
				mustResponsesDeltaEventRaw(
					t,
					fmt.Sprintf(`{"version":"v2","type":"tool_call","tool_call":{"call_id":"c1","name":"internal/read_diary","arguments":{"session_key":"%s","limit":1}}}`+"\n", sessionKey),
				),
			)
		case 2:
			_, _ = fmt.Fprintf(
				w,
				"data: %s\n\n",
				mustResponsesDeltaEventRaw(t, `{"version":"v2","type":"final","final":{"content":"REPLY\nok","stop_reason":"final"}}`+"\n"),
			)
		default:
			_, _ = fmt.Fprintf(
				w,
				"data: %s\n\n",
				mustResponsesDeltaEventRaw(t, `{"version":"v2","type":"error","error":{"code":"internal_error","message":"unexpected call","retryable":false}}`+"\n"),
			)
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.completed"}`)
	}))
	defer server.Close()

	manager := llm.NewManager(config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
		API:      server.URL + "/responses",
		Key:      "test-key",
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	})
	manager.SetImmersive(sessionKey, true)
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{}, manager, []string{"neko"})
	seedQueueForFlushTest(buffer, sessionKey, 0)

	buffer.flush(sessionKey)

	if atomic.LoadInt64(&callCount) != 2 {
		t.Fatalf("expected two provider calls for tool loop, got %d", atomic.LoadInt64(&callCount))
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
	server := newResponsesJSONServer(t, http.StatusOK, `{"output":[{"content":[{"type":"output_text","text":"REPLY\n第一段\n---\n第二段"}]}]}`)
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
		if strings.Contains(entry.Content, "第一段") && strings.Contains(entry.Content, "第二段") {
			foundAssistant = true
		}
	}
	if !foundAssistant {
		t.Fatalf("expected segmented assistant reply in history, entries=%+v", entries)
	}
}

func TestFlush_NonProtocolModelErrorSkips(t *testing.T) {
	server := newResponsesJSONServer(t, http.StatusOK, `{"error":{"message":"boom"}}`)
	defer server.Close()

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0)

	buffer.flush(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	if got := len(state.nextBatch); got != 0 {
		t.Fatalf("expected next batch to be dropped on non-protocol model error, got %d", got)
	}
	if state.waitRounds != 0 {
		t.Fatalf("expected waitRounds reset to 0, got %d", state.waitRounds)
	}
}

func TestFlush_WaitRoundsLimitConvertsWaitToSkip(t *testing.T) {
	server := newResponsesJSONServer(t, http.StatusOK, `{"output":[{"content":[{"type":"output_text","text":"WAIT:100\n"}]}]}`)
	defer server.Close()

	buffer, _, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, maxImmersiveWaitRounds)

	buffer.flush(sessionKey)

	state := buffer.session(sessionKey)
	state.mu.Lock()
	defer state.mu.Unlock()
	if got := len(state.nextBatch); got != 0 {
		t.Fatalf("expected next batch to be dropped when wait limit is reached, got %d", got)
	}
	if state.waitRounds != 0 {
		t.Fatalf("expected waitRounds reset to 0 after limit skip, got %d", state.waitRounds)
	}
}

func TestFlush_PersistsAtomicUserEventsWithoutBatchEnvelope(t *testing.T) {
	server := newResponsesJSONServer(t, http.StatusOK, `{"output":[{"content":[{"type":"output_text","text":"REPLY\nok"}]}]}`)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		_ = r.Body.Close()
		call := atomic.AddInt64(&callCount, 1)
		text := "WAIT:10\n"
		if call > 1 {
			text = "REPLY\nok"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", mustResponsesDeltaEventRaw(t, text))
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.completed"}`)
	}))
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

func TestFlush_InFlightMessagesGoToNextBatchAndAnchorCutoff(t *testing.T) {
	var (
		callCount    int64
		firstStarted = make(chan struct{})
		releaseFirst = make(chan struct{})
		mu           sync.Mutex
		requestBody  = map[int64]string{}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		call := atomic.AddInt64(&callCount, 1)
		mu.Lock()
		requestBody[call] = string(bodyBytes)
		mu.Unlock()

		text := "REPLY\nfirst-reply"
		if call == 2 {
			text = "REPLY\nsecond-reply"
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", mustResponsesDeltaEventRaw(t, text))
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.completed"}`)
	}))
	defer server.Close()

	buffer, manager, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
	seedQueueForFlushTest(buffer, sessionKey, 0,
		queuedMessage{text: "m1", speaker: "name=alice", ts: time.Now(), chars: len([]rune("m1"))},
	)

	done := make(chan struct{})
	go func() {
		buffer.flush(sessionKey)
		close(done)
	}()

	<-firstStarted
	buffer.Enqueue(nil, sessionKey, "m2", "name=bob", false)
	close(releaseFirst)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("first flush did not complete in time")
	}
	state := buffer.session(sessionKey)
	state.mu.Lock()
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
	}
	state.mu.Unlock()

	entriesAfterFirst := mustListChatEvents(t, manager, sessionKey, 20)
	if hasUserContent(entriesAfterFirst, "m2") {
		t.Fatal("message m2 should not be persisted in the first in-flight round")
	}
	firstUserSeq := findCausalSeqByUserContent(entriesAfterFirst, "m1")
	if firstUserSeq == 0 {
		t.Fatal("missing first user causal sequence")
	}
	firstReplyCutoff := findReplyCutoffByAssistantContent(entriesAfterFirst, "first-reply")
	if firstReplyCutoff != firstUserSeq {
		t.Fatalf("first assistant cutoff mismatch: got %d, want %d", firstReplyCutoff, firstUserSeq)
	}

	buffer.flush(sessionKey)

	entriesAfterSecond := mustListChatEvents(t, manager, sessionKey, 40)
	secondUserSeq := findCausalSeqByUserContent(entriesAfterSecond, "m2")
	if secondUserSeq == 0 {
		t.Fatal("missing second user causal sequence")
	}
	if firstReplyCutoff >= secondUserSeq {
		t.Fatalf("first reply cutoff should exclude in-flight message: cutoff=%d, second_user_seq=%d", firstReplyCutoff, secondUserSeq)
	}

	mu.Lock()
	firstBody := requestBody[1]
	secondBody := requestBody[2]
	mu.Unlock()
	if strings.Contains(firstBody, "m2") {
		t.Fatalf("first request should not include m2, body=%q", firstBody)
	}
	if !strings.Contains(secondBody, "m2") {
		t.Fatalf("second request should include m2, body=%q", secondBody)
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

func TestFlush_GrowthRegression_NoBatchEnvelopeLeak(t *testing.T) {
	for _, rounds := range []int{20, 50, 100} {
		t.Run(fmt.Sprintf("rounds_%d", rounds), func(t *testing.T) {
			var (
				mu       sync.Mutex
				requests []string
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/responses" {
					http.NotFound(w, r)
					return
				}
				bodyBytes, _ := io.ReadAll(r.Body)
				_ = r.Body.Close()
				mu.Lock()
				requests = append(requests, string(bodyBytes))
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", mustResponsesDeltaEventRaw(t, "REPLY\nok"))
				_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.completed"}`)
			}))
			defer server.Close()

			buffer, manager, sessionKey := newImmersiveBufferForFlushTest(t, server.URL+"/responses", config.ImmersiveConfig{})
			for i := 1; i <= rounds; i++ {
				text := fmt.Sprintf("msg-%03d", i)
				seedQueueForFlushTest(buffer, sessionKey, 0,
					queuedMessage{text: text, speaker: "name=user", ts: time.Now().Add(time.Duration(i) * time.Second), chars: len([]rune(text))},
				)
				buffer.flush(sessionKey)
			}

			entries := mustListChatEvents(t, manager, sessionKey, rounds*4)
			for _, entry := range entries {
				if entry.Role != chatlog.RoleUser {
					continue
				}
				if strings.Contains(entry.Content, "batch_meta:") || strings.Contains(entry.Content, "transcript:") {
					t.Fatalf("user history leaked batch envelope at rounds=%d: %q", rounds, entry.Content)
				}
			}

			mu.Lock()
			captured := append([]string(nil), requests...)
			mu.Unlock()
			if len(captured) != rounds {
				t.Fatalf("unexpected request count: got %d, want %d", len(captured), rounds)
			}
			for _, body := range captured {
				if strings.Contains(body, "batch_meta:") || strings.Contains(body, "transcript:") {
					t.Fatalf("request body should not contain batch envelope markers, body=%q", body)
				}
			}
		})
	}
}

func newResponsesJSONServer(t *testing.T, statusCode int, responseBody string) *httptest.Server {
	t.Helper()
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	body := strings.TrimSpace(responseBody)
	if body == "" {
		body = `{"output":[{"content":[{"type":"output_text","text":"REPLY\nok"}]}]}`
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(statusCode)
		for _, event := range buildResponsesStreamEvents(t, body) {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
}

func buildResponsesStreamEvents(t *testing.T, body string) []string {
	t.Helper()
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return []string{
			`{"type":"response.completed"}`,
		}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return []string{
			mustResponsesDeltaEventRaw(t, trimmed),
			`{"type":"response.completed"}`,
		}
	}

	if rawErr, ok := payload["error"].(map[string]any); ok {
		message := strings.TrimSpace(toString(rawErr["message"]))
		if message == "" {
			message = "model error"
		}
		event := map[string]any{
			"type": "response.error",
			"error": map[string]any{
				"message": message,
			},
		}
		return []string{mustMarshalJSON(t, event)}
	}

	text := extractResponsesText(payload)
	return []string{
		mustResponsesDeltaEventRaw(t, text),
		`{"type":"response.completed"}`,
	}
}

func extractResponsesText(payload map[string]any) string {
	output, ok := payload["output"].([]any)
	if !ok || len(output) == 0 {
		return ""
	}
	first, ok := output[0].(map[string]any)
	if !ok {
		return ""
	}
	content, ok := first["content"].([]any)
	if !ok || len(content) == 0 {
		return ""
	}
	part, ok := content[0].(map[string]any)
	if !ok {
		return ""
	}
	return toString(part["text"])
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
	})
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

func hasUserContent(entries []chatlog.Entry, token string) bool {
	for _, entry := range entries {
		if entry.Role != chatlog.RoleUser {
			continue
		}
		if strings.Contains(entry.Content, token) {
			return true
		}
	}
	return false
}

func findCausalSeqByUserContent(entries []chatlog.Entry, token string) int64 {
	for _, entry := range entries {
		if entry.Role != chatlog.RoleUser {
			continue
		}
		if !strings.Contains(entry.Content, token) {
			continue
		}
		seq, _ := strconv.ParseInt(strings.TrimSpace(entry.Metadata["causal_seq"]), 10, 64)
		return seq
	}
	return 0
}

func findReplyCutoffByAssistantContent(entries []chatlog.Entry, token string) int64 {
	for _, entry := range entries {
		if entry.Role != chatlog.RoleAssistant {
			continue
		}
		if !strings.Contains(entry.Content, token) {
			continue
		}
		seq, _ := strconv.ParseInt(strings.TrimSpace(entry.Metadata["reply_to_cutoff_seq"]), 10, 64)
		return seq
	}
	return 0
}
