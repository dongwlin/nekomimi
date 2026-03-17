package repeat

import (
	"strconv"
	"testing"
	"time"

	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestBuildRepeatDecision_TwoDistinctUsersHit(t *testing.T) {
	decision := buildRepeatDecision([]chatlog.Entry{
		userEntry(2, "hello", "name=bob;id=2"),
		userEntry(1, "hello", "name=alice;id=1"),
	})

	if decision.Reason != "" {
		t.Fatalf("expected hit, got reason %q", decision.Reason)
	}
	if decision.Text != "hello" {
		t.Fatalf("expected repeat text hello, got %q", decision.Text)
	}
	if decision.RunCount != 2 {
		t.Fatalf("expected run count 2, got %d", decision.RunCount)
	}
	if decision.DistinctUsers != 2 {
		t.Fatalf("expected distinct users 2, got %d", decision.DistinctUsers)
	}
	if decision.RoundStartSeq != 1 {
		t.Fatalf("expected round start seq 1, got %d", decision.RoundStartSeq)
	}
	if decision.LatestSeq != 2 {
		t.Fatalf("expected latest seq 2, got %d", decision.LatestSeq)
	}
}

func TestBuildRepeatDecision_RequiresDistinctUsers(t *testing.T) {
	decision := buildRepeatDecision([]chatlog.Entry{
		userEntry(2, "1", "name=alice;id=1"),
		userEntry(1, "1", "name=alice;id=1"),
	})

	if decision.Reason != "insufficient_distinct_users" {
		t.Fatalf("expected insufficient_distinct_users, got %q", decision.Reason)
	}
}

func TestBuildRepeatDecision_UsesLatestTailAndIgnoresAssistant(t *testing.T) {
	decision := buildRepeatDecision([]chatlog.Entry{
		userEntry(6, "second", "name=erin;id=5"),
		assistantEntry(5, "second"),
		userEntry(4, "second", "name=dave;id=4"),
		userEntry(3, "first", "name=carol;id=3"),
		userEntry(2, "first", "name=bob;id=2"),
		userEntry(1, "first", "name=alice;id=1"),
	})

	if decision.Reason != "" {
		t.Fatalf("expected hit, got reason %q", decision.Reason)
	}
	if decision.Text != "second" {
		t.Fatalf("expected latest run text second, got %q", decision.Text)
	}
	if decision.RunCount != 2 {
		t.Fatalf("expected latest run count 2, got %d", decision.RunCount)
	}
	if decision.RoundStartSeq != 4 {
		t.Fatalf("expected round start seq 4, got %d", decision.RoundStartSeq)
	}
}

func TestNormalizeRepeatText_CompressesWhitespace(t *testing.T) {
	normalized := normalizeRepeatText("  我\t喜欢 \n猫咪  ")
	if normalized != "我 喜欢 猫咪" {
		t.Fatalf("unexpected normalized text %q", normalized)
	}
}

func TestNormalizeConfig_Defaults(t *testing.T) {
	cfg := normalizeConfig(config.RepeatConfig{})
	if cfg.FlushPolicy.MinBatchWaitMS != defaultMinBatchWaitMS {
		t.Fatalf("unexpected min batch wait: %d", cfg.FlushPolicy.MinBatchWaitMS)
	}
	if cfg.FlushPolicy.MaxBatchWaitMS != defaultMaxBatchWaitMS {
		t.Fatalf("unexpected max batch wait: %d", cfg.FlushPolicy.MaxBatchWaitMS)
	}
	if cfg.FlushPolicy.MaxBatchSize != defaultMaxBatchSize {
		t.Fatalf("unexpected max batch size: %d", cfg.FlushPolicy.MaxBatchSize)
	}
}

func TestSetEnabled_UsesConfigDefaultAsBaseline(t *testing.T) {
	engine := NewEngine(config.RepeatConfig{Enabled: true}, nil, nil)
	sessionKey := "group:1"

	if !engine.IsEnabled(sessionKey) {
		t.Fatal("expected config default to enable session")
	}

	engine.SetEnabled(sessionKey, false)
	if engine.IsEnabled(sessionKey) {
		t.Fatal("expected explicit disable to override config default")
	}

	engine.SetEnabled(sessionKey, true)
	if !engine.IsEnabled(sessionKey) {
		t.Fatal("expected enabling back to config default to clear override")
	}
}

type stubHistoryStore struct {
	listEntries     []chatlog.Entry
	assistantEvents []historyAssistantEvent
}

type historyAssistantEvent struct {
	sessionKey string
	text       string
	speaker    string
	cutoffSeq  int64
	at         time.Time
}

func (s *stubHistoryStore) ListChatEvents(sessionKey string, opts chatlog.ListOptions) (chatlog.ListResult, error) {
	limit := opts.Limit
	if limit <= 0 || limit > len(s.listEntries) {
		limit = len(s.listEntries)
	}
	return chatlog.ListResult{Entries: append([]chatlog.Entry(nil), s.listEntries[:limit]...)}, nil
}

func (s *stubHistoryStore) AppendAssistantEventWithSpeakerAt(sessionKey, assistantReply, speaker string, replyToCutoffSeq int64, eventTime time.Time) bool {
	s.assistantEvents = append(s.assistantEvents, historyAssistantEvent{
		sessionKey: sessionKey,
		text:       assistantReply,
		speaker:    speaker,
		cutoffSeq:  replyToCutoffSeq,
		at:         eventTime,
	})
	return true
}

type stubRecorder struct {
	calls []historyAssistantEvent
}

func (s *stubRecorder) RecordAssistantDelivered(sessionKey, text, speaker string) {
	s.calls = append(s.calls, historyAssistantEvent{
		sessionKey: sessionKey,
		text:       text,
		speaker:    speaker,
	})
}

func TestTryRepeat_SingleRoundOnlyOnce(t *testing.T) {
	history := &stubHistoryStore{
		listEntries: []chatlog.Entry{
			userEntry(2, "hello", "name=bob;id=2"),
			userEntry(1, "hello", "name=alice;id=1"),
		},
	}
	recorder := &stubRecorder{}
	engine := NewEngine(config.RepeatConfig{Enabled: true}, history, recorder)

	sent := make([]string, 0, 1)
	sendFn := func(payload interface{}) message.ID {
		if text, ok := payload.(string); ok {
			sent = append(sent, text)
		}
		return message.ID{}
	}

	meta := historyMeta("hello")
	if !engine.tryRepeatWithSend("group:1", meta, "name=nekomimi;id=10000", sendFn) {
		t.Fatal("expected first repeat attempt to hit")
	}
	if len(sent) != 1 {
		t.Fatalf("expected one outbound message, got %d", len(sent))
	}
	if len(history.assistantEvents) != 1 {
		t.Fatalf("expected one assistant history event, got %d", len(history.assistantEvents))
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("expected one immersive sync call, got %d", len(recorder.calls))
	}

	history.listEntries = []chatlog.Entry{
		userEntry(4, "hello", "name=carol;id=3"),
		assistantEntry(3, "hello"),
		userEntry(2, "hello", "name=bob;id=2"),
		userEntry(1, "hello", "name=alice;id=1"),
	}
	if engine.tryRepeatWithSend("group:1", meta, "name=nekomimi;id=10000", sendFn) {
		t.Fatal("expected same round to be suppressed after bot already joined")
	}
	if len(sent) != 1 {
		t.Fatalf("expected outbound count to remain 1, got %d", len(sent))
	}
}

func TestTryRepeat_AllowsNewRoundAfterInterruption(t *testing.T) {
	history := &stubHistoryStore{
		listEntries: []chatlog.Entry{
			userEntry(2, "hello", "name=bob;id=2"),
			userEntry(1, "hello", "name=alice;id=1"),
		},
	}
	engine := NewEngine(config.RepeatConfig{Enabled: true}, history, &stubRecorder{})

	sendCount := 0
	sendFn := func(payload interface{}) message.ID {
		sendCount++
		return message.ID{}
	}

	meta := historyMeta("hello")
	if !engine.tryRepeatWithSend("group:1", meta, "name=nekomimi;id=10000", sendFn) {
		t.Fatal("expected first round to hit")
	}

	history.listEntries = []chatlog.Entry{
		userEntry(6, "hello", "name=erin;id=5"),
		userEntry(5, "hello", "name=dave;id=4"),
		userEntry(4, "other", "name=carol;id=3"),
		assistantEntry(3, "hello"),
		userEntry(2, "hello", "name=bob;id=2"),
		userEntry(1, "hello", "name=alice;id=1"),
	}
	if !engine.tryRepeatWithSend("group:1", meta, "name=nekomimi;id=10000", sendFn) {
		t.Fatal("expected interrupted tail to create a new repeat round")
	}
	if sendCount != 2 {
		t.Fatalf("expected two successful repeats across different rounds, got %d", sendCount)
	}
}

func historyMeta(text string) immersivepkg.AmbientMessageMeta {
	return immersivepkg.AmbientMessageMeta{
		Text: text,
		At:   time.Date(2026, 3, 7, 21, 20, 39, 0, time.Local),
	}
}

func userEntry(seq int64, text, speaker string) chatlog.Entry {
	return chatlog.Entry{
		Role: chatlog.RoleUser,
		Metadata: map[string]string{
			llm.MetadataRawText:      text,
			llm.MetadataSpeakerLabel: speaker,
			llm.MetadataCausalSeq:    formatSeq(seq),
		},
	}
}

func assistantEntry(seq int64, text string) chatlog.Entry {
	return chatlog.Entry{
		Role: chatlog.RoleAssistant,
		Metadata: map[string]string{
			llm.MetadataRawText:   text,
			llm.MetadataCausalSeq: formatSeq(seq),
		},
	}
}

func formatSeq(seq int64) string {
	return strconv.FormatInt(seq, 10)
}
