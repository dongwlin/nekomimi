package commands

import (
	"testing"
	"time"

	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type stubAmbientLLMState struct {
	enabled     bool
	immersive   map[string]bool
	nextSeq     int64
	appendCalls []ambientHistoryAppend
}

type ambientHistoryAppend struct {
	sessionKey string
	text       string
	speaker    string
	at         time.Time
	metadata   map[string]string
}

func (s stubAmbientLLMState) IsEnabled() bool {
	return s.enabled
}

func (s stubAmbientLLMState) IsImmersive(sessionKey string) bool {
	return s.immersive[sessionKey]
}

func (s *stubAmbientLLMState) AppendUserEventWithMetadataAt(sessionKey, userInput, speaker string, eventTime time.Time, metadata map[string]string) (int64, bool) {
	s.nextSeq++
	s.appendCalls = append(s.appendCalls, ambientHistoryAppend{
		sessionKey: sessionKey,
		text:       userInput,
		speaker:    speaker,
		at:         eventTime,
		metadata:   metadata,
	})
	return s.nextSeq, true
}

type recordingImmersiveEngine struct {
	yield    bool
	meta     immersivepkg.AmbientMessageMeta
	enqueues []ambientEnqueueCall
}

type ambientEnqueueCall struct {
	sessionKey   string
	meta         immersivepkg.AmbientMessageMeta
	persistedSeq int64
}

func (r *recordingImmersiveEngine) AnalyzeAmbientMessage(ctx *zero.Ctx, text, speaker string, isPrivate bool, at time.Time) immersivepkg.AmbientMessageMeta {
	meta := immersivepkg.NewAmbientMessageMeta(text, speaker, isPrivate, at)
	if r.meta.MentionBot {
		meta.MentionBot = true
	}
	if r.meta.AddressedToBot {
		meta.AddressedToBot = true
	}
	if r.meta.Question {
		meta.Question = true
	}
	if r.meta.DirectedQuestion {
		meta.DirectedQuestion = true
	}
	if r.meta.NicknamePosition != 0 {
		meta.NicknamePosition = r.meta.NicknamePosition
	}
	return meta
}

func (r *recordingImmersiveEngine) EnqueueAmbient(ctx *zero.Ctx, sessionKey string, meta immersivepkg.AmbientMessageMeta, persistedSeq int64) {
	r.enqueues = append(r.enqueues, ambientEnqueueCall{
		sessionKey:   sessionKey,
		meta:         meta,
		persistedSeq: persistedSeq,
	})
}

func (r *recordingImmersiveEngine) RecordEvent(sessionKey string, event immersivepkg.TimelineEvent) {}

func (r *recordingImmersiveEngine) RecordTimelineEvent(sessionKey, text, speaker string) {}

func (r *recordingImmersiveEngine) RecordAssistantDelivered(sessionKey, text, speaker string) {}

func (r *recordingImmersiveEngine) ShouldYieldToImmersive(sessionKey string, meta immersivepkg.AmbientMessageMeta) bool {
	return r.yield
}

func (r *recordingImmersiveEngine) DebugSnapshot(sessionKey string) immersivepkg.DebugSnapshot {
	return immersivepkg.DebugSnapshot{}
}

func (r *recordingImmersiveEngine) Clear(sessionKey string) {}

func (r *recordingImmersiveEngine) RefreshIdentityFromCtx(ctx *zero.Ctx) {}

func (r *recordingImmersiveEngine) ReloadConfig(cfg config.ImmersiveConfig, nicknames []string) {}

type recordingRepeatEngine struct {
	enabled map[string]bool
	hit     bool
	tries   []repeatTryCall
}

type repeatTryCall struct {
	sessionKey       string
	meta             immersivepkg.AmbientMessageMeta
	assistantSpeaker string
}

func (r *recordingRepeatEngine) TryRepeat(ctx *zero.Ctx, sessionKey string, meta immersivepkg.AmbientMessageMeta, assistantSpeaker string) bool {
	r.tries = append(r.tries, repeatTryCall{
		sessionKey:       sessionKey,
		meta:             meta,
		assistantSpeaker: assistantSpeaker,
	})
	return r.hit
}

func (r *recordingRepeatEngine) Clear(sessionKey string) {}

func (r *recordingRepeatEngine) ReloadConfig(cfg config.RepeatConfig) {}

func (r *recordingRepeatEngine) SetEnabled(sessionKey string, enabled bool) {
	if r.enabled == nil {
		r.enabled = make(map[string]bool)
	}
	r.enabled[sessionKey] = enabled
}

func (r *recordingRepeatEngine) IsEnabled(sessionKey string) bool {
	return r.enabled[sessionKey]
}

func TestHandleAmbientMessage_RepeatHitSkipsImmersive(t *testing.T) {
	cfg := &config.Config{CommandPrefix: "/"}
	ctx := testMessageCtx("group", 10001, 42, "hello")
	llmState := &stubAmbientLLMState{
		enabled: true,
		immersive: map[string]bool{
			"group:10001": true,
		},
	}
	repeatEngine := &recordingRepeatEngine{
		enabled: map[string]bool{
			"group:10001": true,
		},
		hit: true,
	}
	immersiveEngine := &recordingImmersiveEngine{}

	handleAmbientMessage(cfg, llmState, immersiveEngine, repeatEngine, ctx)

	if len(repeatEngine.tries) != 1 {
		t.Fatalf("expected repeat try once, got %d", len(repeatEngine.tries))
	}
	if len(immersiveEngine.enqueues) != 0 {
		t.Fatalf("expected immersive enqueue to be skipped, got %d", len(immersiveEngine.enqueues))
	}
	if len(llmState.appendCalls) != 1 {
		t.Fatalf("expected one history append, got %d", len(llmState.appendCalls))
	}
}

func TestHandleAmbientMessage_RepeatMissFallsBackToImmersive(t *testing.T) {
	cfg := &config.Config{CommandPrefix: "/"}
	ctx := testMessageCtx("group", 10001, 42, "hello")
	llmState := &stubAmbientLLMState{
		enabled: true,
		immersive: map[string]bool{
			"group:10001": true,
		},
	}
	repeatEngine := &recordingRepeatEngine{
		enabled: map[string]bool{
			"group:10001": true,
		},
	}
	immersiveEngine := &recordingImmersiveEngine{}

	handleAmbientMessage(cfg, llmState, immersiveEngine, repeatEngine, ctx)

	if len(repeatEngine.tries) != 1 {
		t.Fatalf("expected repeat try once, got %d", len(repeatEngine.tries))
	}
	if len(immersiveEngine.enqueues) != 1 {
		t.Fatalf("expected immersive enqueue once, got %d", len(immersiveEngine.enqueues))
	}
	if immersiveEngine.enqueues[0].persistedSeq != 1 {
		t.Fatalf("expected persisted seq 1, got %d", immersiveEngine.enqueues[0].persistedSeq)
	}
}

func TestHandleAmbientMessage_PrivateSkipsRepeat(t *testing.T) {
	cfg := &config.Config{CommandPrefix: "/"}
	ctx := testMessageCtx("private", 0, 42, "hello")
	llmState := &stubAmbientLLMState{
		enabled: true,
		immersive: map[string]bool{
			"private:42": true,
		},
	}
	repeatEngine := &recordingRepeatEngine{
		enabled: map[string]bool{
			"private:42": true,
		},
	}
	immersiveEngine := &recordingImmersiveEngine{}

	handleAmbientMessage(cfg, llmState, immersiveEngine, repeatEngine, ctx)

	if len(repeatEngine.tries) != 0 {
		t.Fatalf("expected private message to skip repeat, got %d", len(repeatEngine.tries))
	}
	if len(immersiveEngine.enqueues) != 1 {
		t.Fatalf("expected private message to enqueue immersive once, got %d", len(immersiveEngine.enqueues))
	}
}

func TestHandleAmbientMessage_AddressedMessagePrefersImmersive(t *testing.T) {
	cfg := &config.Config{CommandPrefix: "/"}
	ctx := testMessageCtx("group", 10001, 42, "neko hello")
	llmState := &stubAmbientLLMState{
		enabled: true,
		immersive: map[string]bool{
			"group:10001": true,
		},
	}
	repeatEngine := &recordingRepeatEngine{
		enabled: map[string]bool{
			"group:10001": true,
		},
	}
	immersiveEngine := &recordingImmersiveEngine{
		yield: true,
		meta: immersivepkg.AmbientMessageMeta{
			AddressedToBot:   true,
			NicknamePosition: immersivepkg.NickStart,
		},
	}

	handleAmbientMessage(cfg, llmState, immersiveEngine, repeatEngine, ctx)

	if len(repeatEngine.tries) != 0 {
		t.Fatalf("expected addressed message to skip repeat, got %d", len(repeatEngine.tries))
	}
	if len(immersiveEngine.enqueues) != 1 {
		t.Fatalf("expected addressed message to enqueue immersive once, got %d", len(immersiveEngine.enqueues))
	}
}

func TestHandleAmbientMessage_IgnoresCommands(t *testing.T) {
	cfg := &config.Config{CommandPrefix: "/"}
	ctx := testMessageCtx("group", 10001, 42, "/repeat status")
	llmState := &stubAmbientLLMState{
		enabled: true,
		immersive: map[string]bool{
			"group:10001": true,
		},
	}
	repeatEngine := &recordingRepeatEngine{
		enabled: map[string]bool{
			"group:10001": true,
		},
	}
	immersiveEngine := &recordingImmersiveEngine{}

	handleAmbientMessage(cfg, llmState, immersiveEngine, repeatEngine, ctx)

	if len(llmState.appendCalls) != 0 {
		t.Fatalf("expected command message to skip history append, got %d", len(llmState.appendCalls))
	}
	if len(immersiveEngine.enqueues) != 0 {
		t.Fatalf("expected command message to skip immersive enqueue, got %d", len(immersiveEngine.enqueues))
	}
	if len(repeatEngine.tries) != 0 {
		t.Fatalf("expected command message to skip repeat, got %d", len(repeatEngine.tries))
	}
}

func testMessageCtx(detailType string, groupID, userID int64, text string) *zero.Ctx {
	return &zero.Ctx{
		Event: &zero.Event{
			PostType:   "message",
			DetailType: detailType,
			GroupID:    groupID,
			UserID:     userID,
			RawMessage: text,
			Message: message.Message{
				message.Text(text),
			},
		},
	}
}
