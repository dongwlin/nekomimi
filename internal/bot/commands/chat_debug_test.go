package commands

import (
	"strings"
	"testing"
	"time"

	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type stubImmersiveEngine struct {
	snapshot immersivepkg.DebugSnapshot
}

func (s stubImmersiveEngine) Enqueue(ctx *zero.Ctx, sessionKey, text, speaker string, isPrivate bool) {
}
func (s stubImmersiveEngine) RecordEvent(sessionKey string, event immersivepkg.TimelineEvent) {}
func (s stubImmersiveEngine) RecordTimelineEvent(sessionKey, text, speaker string)            {}
func (s stubImmersiveEngine) DebugSnapshot(sessionKey string) immersivepkg.DebugSnapshot {
	result := s.snapshot
	if strings.TrimSpace(result.SessionKey) == "" {
		result.SessionKey = sessionKey
	}
	return result
}
func (s stubImmersiveEngine) Clear(sessionKey string)                                     {}
func (s stubImmersiveEngine) RefreshIdentityFromCtx(ctx *zero.Ctx)                        {}
func (s stubImmersiveEngine) ReloadConfig(cfg config.ImmersiveConfig, nicknames []string) {}

func TestBuildChatDebugResponse_RequiresSuperUser(t *testing.T) {
	previous := zero.BotConfig.SuperUsers
	zero.BotConfig.SuperUsers = []int64{42}
	defer func() {
		zero.BotConfig.SuperUsers = previous
	}()

	ctx := &zero.Ctx{
		Event: &zero.Event{
			UserID:     7,
			PostType:   "message",
			DetailType: "group",
			GroupID:    10001,
		},
	}

	response, ok := buildChatDebugResponse(ctx, stubImmersiveEngine{}, "")
	if ok {
		t.Fatalf("expected non-superuser to be rejected, got ok with response %q", response)
	}
	if response != "" {
		t.Fatalf("expected empty response for non-superuser, got %q", response)
	}
}

func TestBuildChatDebugResponse_TruncatesReplyPreview(t *testing.T) {
	previous := zero.BotConfig.SuperUsers
	zero.BotConfig.SuperUsers = []int64{42}
	defer func() {
		zero.BotConfig.SuperUsers = previous
	}()

	longReply := strings.Repeat("a", 220)
	ctx := &zero.Ctx{
		Event: &zero.Event{
			UserID:     42,
			PostType:   "message",
			DetailType: "group",
			GroupID:    10001,
		},
	}
	engine := stubImmersiveEngine{
		snapshot: immersivepkg.DebugSnapshot{
			SessionKey:              "group:10001",
			UpdatedAt:               time.Date(2026, 3, 7, 12, 0, 0, 0, time.Local),
			ConversationMode:        "cooling_down",
			EnergyValue:             31,
			EnergyTarget:            45,
			EnergyBaseline:          45,
			EnergyLastDeltaReason:   "assistant_reply_cost",
			LastSignalBand:          "observe",
			LastSchedulerReason:     "post_reply_cooldown",
			LastSchedulerPriority:   "deferred",
			LastFinalAction:         "skip",
			LastFinalReasonCode:     "signal_below_threshold",
			LastReplyPreview:        longReply,
			LastStrongCallLatencyMS: 350,
		},
	}

	response, ok := buildChatDebugResponse(ctx, engine, "")
	if !ok {
		t.Fatal("expected superuser debug response to be generated")
	}
	if !strings.Contains(response, "沉浸调试") {
		t.Fatalf("expected debug header, got %q", response)
	}
	if strings.Contains(response, longReply) {
		t.Fatal("expected long reply preview to be truncated in command output")
	}
	if !strings.Contains(response, strings.Repeat("a", 120)) {
		t.Fatalf("expected truncated preview content to remain visible, got %q", response)
	}
}
