package immersive

import (
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
			text:             "你好",
			speaker:          "name=alice",
			ts:               base,
			chars:            2,
			isQuestion:       true,
			isAddressedToBot: true,
		},
		{
			text:             "我在",
			speaker:          "name=bob",
			ts:               base.Add(2 * time.Second),
			chars:            2,
			isMentionBot:     true,
			isQuestion:       false,
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
		"- [name=alice;time=2026-02-10 21:30:05]: 你好",
	} {
		if !strings.Contains(input, token) {
			t.Fatalf("structured input missing %q, got:\n%s", token, input)
		}
	}
}

func TestShouldSpeak_ExplicitMentionAlwaysPasses(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		SpeakGate: config.SpeakGateConfig{},
	})
	buffer := &ImmersiveBuffer{cfg: cfg}
	queue := []queuedMessage{
		{
			text:             "@bot 在吗",
			speaker:          "name=alice",
			isMentionBot:     true,
			isAddressedToBot: true,
		},
	}
	result := buffer.shouldSpeak(&immersiveSession{}, queue, buildCombinedInput(queue, botIdentity{}))
	if !result.shouldSpeak {
		t.Fatalf("expected mention to pass speak gate, got %+v", result)
	}
}

func TestShouldSpeak_RuleSuppressionRemoved_DefaultAllow(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		SpeakGate: config.SpeakGateConfig{},
	})
	buffer := &ImmersiveBuffer{cfg: cfg}
	state := &immersiveSession{}
	queue := []queuedMessage{
		{
			text:             "我们继续聊",
			speaker:          "name=alice",
			isAddressedToBot: false,
			isQuestion:       false,
		},
		{
			text:             "好的",
			speaker:          "name=bob",
			isAddressedToBot: false,
			isQuestion:       false,
		},
		{
			text:             "冲冲冲",
			speaker:          "name=alice",
			isAddressedToBot: false,
			isQuestion:       false,
		},
	}
	result := buffer.shouldSpeak(state, queue, buildCombinedInput(queue, botIdentity{}))
	if !result.shouldSpeak {
		t.Fatalf("expected assistant-only mode to allow speaking when assistant is disabled, got %+v", result)
	}
	if !strings.Contains(result.reason, "assistant_not_enabled_allow") {
		t.Fatalf("expected default-allow reason in assistant-only mode, got %q", result.reason)
	}
}

func TestShouldSpeak_AssistantErrorFailOpenAllow(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		SpeakGate: config.SpeakGateConfig{
			Enabled:  true,
			FailOpen: true,
		},
	})
	manager := llm.NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "",
		Immersive: config.ImmersiveConfig{
			SpeakGate: config.SpeakGateConfig{
				Enabled: true,
			},
		},
	})
	buffer := &ImmersiveBuffer{cfg: cfg, llm: manager}
	queue := []queuedMessage{{text: "测试", speaker: "name=alice"}}

	result := buffer.shouldSpeak(&immersiveSession{}, queue, buildCombinedInput(queue, botIdentity{}))
	if !result.shouldSpeak {
		t.Fatalf("expected fail_open=true to allow speaking on assistant error, got %+v", result)
	}
	if result.assistantStatus != "error_allow" {
		t.Fatalf("expected assistantStatus=error_allow, got %q", result.assistantStatus)
	}
}

func TestShouldSpeak_AssistantErrorFailCloseBlock(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		SpeakGate: config.SpeakGateConfig{
			Enabled:  true,
			FailOpen: false,
		},
	})
	manager := llm.NewManager(config.LLMConfig{
		Enabled: true,
		Model:   "",
		Immersive: config.ImmersiveConfig{
			SpeakGate: config.SpeakGateConfig{
				Enabled: true,
			},
		},
	})
	buffer := &ImmersiveBuffer{cfg: cfg, llm: manager}
	queue := []queuedMessage{{text: "测试", speaker: "name=alice"}}

	result := buffer.shouldSpeak(&immersiveSession{}, queue, buildCombinedInput(queue, botIdentity{}))
	if result.shouldSpeak {
		t.Fatalf("expected fail_open=false to block speaking on assistant error, got %+v", result)
	}
	if result.assistantStatus != "error_block" {
		t.Fatalf("expected assistantStatus=error_block, got %q", result.assistantStatus)
	}
}

func TestRecordTimelineEvent_AppendsWithoutQueueing(t *testing.T) {
	buffer := NewImmersiveBuffer(config.ImmersiveConfig{}, nil, []string{"neko"})
	sessionKey := "group:1"
	buffer.RecordTimelineEvent(sessionKey, "用户戳了你一下。", "name=alice")
	buffer.RecordTimelineEvent(sessionKey, "你回戳了对方。", "assistant")

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
