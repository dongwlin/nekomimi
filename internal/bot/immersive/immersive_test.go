package immersive

import (
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
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
