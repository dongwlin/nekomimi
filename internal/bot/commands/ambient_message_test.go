package commands

import (
	"testing"

	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type stubAmbientLLMState struct {
	enabled   bool
	immersive map[string]bool
}

func (s stubAmbientLLMState) IsEnabled() bool {
	return s.enabled
}

func (s stubAmbientLLMState) IsImmersive(sessionKey string) bool {
	return s.immersive[sessionKey]
}

type recordingImmersiveEngine struct {
	enqueues []enqueueCall
}

func (r *recordingImmersiveEngine) Enqueue(ctx *zero.Ctx, sessionKey, text, speaker string, isPrivate bool) {
	r.enqueues = append(r.enqueues, enqueueCall{
		sessionKey: sessionKey,
		text:       text,
		speaker:    speaker,
		isPrivate:  isPrivate,
	})
}

func (r *recordingImmersiveEngine) RecordEvent(sessionKey string, event immersivepkg.TimelineEvent) {}

func (r *recordingImmersiveEngine) RecordTimelineEvent(sessionKey, text, speaker string) {}

func (r *recordingImmersiveEngine) DebugSnapshot(sessionKey string) immersivepkg.DebugSnapshot {
	return immersivepkg.DebugSnapshot{}
}

func (r *recordingImmersiveEngine) Clear(sessionKey string) {}

func (r *recordingImmersiveEngine) RefreshIdentityFromCtx(ctx *zero.Ctx) {}

func (r *recordingImmersiveEngine) ReloadConfig(cfg config.ImmersiveConfig, nicknames []string) {}

type recordingRepeatEngine struct {
	enabled  map[string]bool
	enqueues []enqueueCall
}

func (r *recordingRepeatEngine) Enqueue(ctx *zero.Ctx, sessionKey, text, speaker, assistantSpeaker string, isPrivate bool) bool {
	r.enqueues = append(r.enqueues, enqueueCall{
		sessionKey: sessionKey,
		text:       text,
		speaker:    speaker,
		isPrivate:  isPrivate,
	})
	return true
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

type enqueueCall struct {
	sessionKey string
	text       string
	speaker    string
	isPrivate  bool
}

func TestHandleAmbientMessage_PrefersRepeatOverImmersive(t *testing.T) {
	cfg := &config.Config{CommandPrefix: "/"}
	ctx := testMessageCtx("group", 10001, 42, "喵")
	repeatEngine := &recordingRepeatEngine{
		enabled: map[string]bool{
			"group:10001": true,
		},
	}
	immersiveEngine := &recordingImmersiveEngine{}

	handleAmbientMessage(cfg, stubAmbientLLMState{
		enabled: true,
		immersive: map[string]bool{
			"group:10001": true,
		},
	}, immersiveEngine, repeatEngine, ctx)

	if len(repeatEngine.enqueues) != 1 {
		t.Fatalf("expected repeat enqueue once, got %d", len(repeatEngine.enqueues))
	}
	if len(immersiveEngine.enqueues) != 0 {
		t.Fatalf("expected immersive enqueue to be skipped, got %d", len(immersiveEngine.enqueues))
	}
}

func TestHandleAmbientMessage_FallsBackToImmersive(t *testing.T) {
	cfg := &config.Config{CommandPrefix: "/"}
	ctx := testMessageCtx("group", 10001, 42, "喵")
	repeatEngine := &recordingRepeatEngine{enabled: map[string]bool{}}
	immersiveEngine := &recordingImmersiveEngine{}

	handleAmbientMessage(cfg, stubAmbientLLMState{
		enabled: true,
		immersive: map[string]bool{
			"group:10001": true,
		},
	}, immersiveEngine, repeatEngine, ctx)

	if len(immersiveEngine.enqueues) != 1 {
		t.Fatalf("expected immersive enqueue once, got %d", len(immersiveEngine.enqueues))
	}
	if len(repeatEngine.enqueues) != 0 {
		t.Fatalf("expected repeat enqueue to be skipped, got %d", len(repeatEngine.enqueues))
	}
}

func TestHandleAmbientMessage_IgnoresCommands(t *testing.T) {
	cfg := &config.Config{CommandPrefix: "/"}
	ctx := testMessageCtx("group", 10001, 42, "/repeat status")
	repeatEngine := &recordingRepeatEngine{
		enabled: map[string]bool{
			"group:10001": true,
		},
	}
	immersiveEngine := &recordingImmersiveEngine{}

	handleAmbientMessage(cfg, stubAmbientLLMState{
		enabled: true,
		immersive: map[string]bool{
			"group:10001": true,
		},
	}, immersiveEngine, repeatEngine, ctx)

	if len(immersiveEngine.enqueues) != 0 {
		t.Fatalf("expected command message to skip immersive enqueue, got %d", len(immersiveEngine.enqueues))
	}
	if len(repeatEngine.enqueues) != 0 {
		t.Fatalf("expected command message to skip repeat enqueue, got %d", len(repeatEngine.enqueues))
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
