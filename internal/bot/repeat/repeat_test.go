package repeat

import (
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestDetectConsecutiveRepeat_TwoSpeakers(t *testing.T) {
	text, count, participants := detectConsecutiveRepeat([]queuedMessage{
		{text: "开冲", speaker: "name=alice;id=1"},
		{text: "开冲", speaker: "name=bob;id=2"},
	})
	if text != "开冲" {
		t.Fatalf("expected repeat text 开冲, got %q", text)
	}
	if count != 2 {
		t.Fatalf("expected repeat count 2, got %d", count)
	}
	if participants != 2 {
		t.Fatalf("expected participants 2, got %d", participants)
	}
}

func TestDetectConsecutiveRepeat_SameSpeakerOnly(t *testing.T) {
	text, count, participants := detectConsecutiveRepeat([]queuedMessage{
		{text: "1", speaker: "name=alice;id=1"},
		{text: "1", speaker: "name=alice;id=1"},
	})
	if text != "" || count != 0 || participants != 0 {
		t.Fatalf("expected no repeat trigger, got text=%q count=%d participants=%d", text, count, participants)
	}
}

func TestDetectConsecutiveRepeat_RequiresConsecutive(t *testing.T) {
	text, count, participants := detectConsecutiveRepeat([]queuedMessage{
		{text: "喵", speaker: "name=alice;id=1"},
		{text: "不是喵", speaker: "name=bob;id=2"},
		{text: "喵", speaker: "name=carol;id=3"},
	})
	if text != "" || count != 0 || participants != 0 {
		t.Fatalf("expected no repeat trigger, got text=%q count=%d participants=%d", text, count, participants)
	}
}

func TestDetectConsecutiveRepeat_UsesLatestRun(t *testing.T) {
	text, count, participants := detectConsecutiveRepeat([]queuedMessage{
		{text: "第一段", speaker: "name=alice;id=1"},
		{text: "第一段", speaker: "name=bob;id=2"},
		{text: "第二段", speaker: "name=carol;id=3"},
		{text: "第二段", speaker: "name=dave;id=4"},
		{text: "第二段", speaker: "name=erin;id=5"},
	})
	if text != "第二段" {
		t.Fatalf("expected latest repeated text 第二段, got %q", text)
	}
	if count != 3 {
		t.Fatalf("expected repeat count 3, got %d", count)
	}
	if participants != 3 {
		t.Fatalf("expected participants 3, got %d", participants)
	}
}

func TestNormalizeRepeatText_CompressesWhitespace(t *testing.T) {
	normalized := normalizeRepeatText("  我 喜欢\t猫咪 \n")
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
	engine := NewEngine(config.RepeatConfig{Enabled: true}, nil)
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

func TestComputeFlushDecision_RespectsBatchDeadline(t *testing.T) {
	engine := NewEngine(config.RepeatConfig{
		Enabled: true,
		FlushPolicy: config.FlushPolicyConfig{
			MinBatchWaitMS: 800,
			MaxBatchWaitMS: 1000,
			MaxBatchSize:   10,
		},
	}, nil)
	state := &sessionState{
		nextBatch: []queuedMessage{
			{text: "喵", ts: time.Unix(0, 0)},
		},
		batchStartTime: time.Unix(0, 0),
	}

	decision := engine.computeFlushDecision(state, time.Unix(0, 700*int64(time.Millisecond)))
	if decision.Delay != 300*time.Millisecond {
		t.Fatalf("expected batch deadline to cap delay, got %s", decision.Delay)
	}
}

type stubHistoryWriter struct {
	userEvents      []historyUserEvent
	assistantEvents []historyAssistantEvent
	nextSeq         int64
}

type historyUserEvent struct {
	sessionKey string
	text       string
	speaker    string
	at         time.Time
	seq        int64
}

type historyAssistantEvent struct {
	sessionKey string
	text       string
	speaker    string
	cutoffSeq  int64
	at         time.Time
}

func (s *stubHistoryWriter) AppendUserEventAt(sessionKey, userInput, speaker string, eventTime time.Time) (int64, bool) {
	s.nextSeq++
	s.userEvents = append(s.userEvents, historyUserEvent{
		sessionKey: sessionKey,
		text:       userInput,
		speaker:    speaker,
		at:         eventTime,
		seq:        s.nextSeq,
	})
	return s.nextSeq, true
}

func (s *stubHistoryWriter) AppendAssistantEventWithSpeakerAt(sessionKey, assistantReply, speaker string, replyToCutoffSeq int64, eventTime time.Time) bool {
	s.assistantEvents = append(s.assistantEvents, historyAssistantEvent{
		sessionKey: sessionKey,
		text:       assistantReply,
		speaker:    speaker,
		cutoffSeq:  replyToCutoffSeq,
		at:         eventTime,
	})
	return true
}

func TestFlush_AppendsRepeatMessagesToHistory(t *testing.T) {
	history := &stubHistoryWriter{}
	engine := NewEngine(config.RepeatConfig{Enabled: true}, history)
	sessionKey := "group:history"
	base := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)

	state := engine.session(sessionKey)
	state.nextBatch = []queuedMessage{
		{text: "开冲", speaker: "name=alice;id=1", ts: base},
		{text: "开冲", speaker: "name=bob;id=2", ts: base.Add(time.Second)},
	}
	state.assistantLabel = "name=nekomimi;id=10000"
	state.batchStartTime = base

	engine.flush(sessionKey)

	if len(history.userEvents) != 2 {
		t.Fatalf("expected 2 user history events, got %d", len(history.userEvents))
	}
	if history.userEvents[0].text != "开冲" || history.userEvents[1].text != "开冲" {
		t.Fatalf("unexpected user history content: %+v", history.userEvents)
	}
	if len(history.assistantEvents) != 1 {
		t.Fatalf("expected 1 assistant history event, got %d", len(history.assistantEvents))
	}
	if history.assistantEvents[0].text != "开冲" {
		t.Fatalf("unexpected assistant history content: %+v", history.assistantEvents[0])
	}
	if history.assistantEvents[0].speaker != "name=nekomimi;id=10000" {
		t.Fatalf("unexpected assistant history speaker: %q", history.assistantEvents[0].speaker)
	}
	if history.assistantEvents[0].cutoffSeq != 2 {
		t.Fatalf("expected assistant cutoff seq 2, got %d", history.assistantEvents[0].cutoffSeq)
	}
}
