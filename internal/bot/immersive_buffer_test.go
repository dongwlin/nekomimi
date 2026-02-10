package bot

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

	input := buildCombinedInput(queue)
	for _, token := range []string{
		"batch_meta:",
		"now_date:",
		"now_time:",
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
		SpeakGate: config.SpeakGateConfig{
			Enabled:                 true,
			Threshold:               5,
			SuppressAfterBotReplyMS: 2500,
			MaxConsecutiveBotTurns:  1,
		},
	})
	buffer := &ImmersiveBuffer{cfg: cfg}
	result := buffer.shouldSpeak(&immersiveSession{}, []queuedMessage{
		{
			text:             "@bot 在吗",
			speaker:          "name=alice",
			isMentionBot:     true,
			isAddressedToBot: true,
		},
	})
	if !result.shouldSpeak {
		t.Fatalf("expected mention to pass speak gate, got %+v", result)
	}
}

func TestShouldSpeak_RecentBotReplyIsSuppressed(t *testing.T) {
	cfg := normalizeImmersiveConfig(config.ImmersiveConfig{
		SpeakGate: config.SpeakGateConfig{
			Enabled:                 true,
			Threshold:               3,
			SuppressAfterBotReplyMS: 3000,
			MaxConsecutiveBotTurns:  1,
		},
	})
	buffer := &ImmersiveBuffer{cfg: cfg}
	state := &immersiveSession{
		lastReply: time.Now().Add(-500 * time.Millisecond),
		botTurns:  1,
	}
	result := buffer.shouldSpeak(state, []queuedMessage{
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
	})
	if result.shouldSpeak {
		t.Fatalf("expected recent bot reply to suppress speaking, got %+v", result)
	}
	if !strings.Contains(result.reason, "recent_bot_reply") {
		t.Fatalf("expected suppression reason to include recent_bot_reply, got %q", result.reason)
	}
}
