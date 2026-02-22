package commands

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type pokeMoodTier string

const (
	pokeMoodWarm    pokeMoodTier = "warm"
	pokeMoodMild    pokeMoodTier = "mild_annoyed"
	pokeMoodAnnoyed pokeMoodTier = "annoyed"
)

type pokeTracker struct {
	mu      sync.Mutex
	window  time.Duration
	mild    int
	annoyed int
	records map[string][]time.Time
}

func newPokeTracker(cfg config.PokeReactionConfig) *pokeTracker {
	windowMS := cfg.WindowMS
	if windowMS <= 0 {
		windowMS = 180000
	}
	mild := cfg.MildThreshold
	if mild <= 0 {
		mild = 3
	}
	annoyed := cfg.AnnoyedThreshold
	if annoyed <= 0 {
		annoyed = 6
	}
	if annoyed < mild {
		annoyed = mild
	}
	return &pokeTracker{
		window:  time.Duration(windowMS) * time.Millisecond,
		mild:    mild,
		annoyed: annoyed,
		records: make(map[string][]time.Time),
	}
}

func (t *pokeTracker) Observe(sessionKey string, now time.Time) (int, pokeMoodTier) {
	if t == nil {
		return 1, pokeMoodWarm
	}
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return 1, pokeMoodWarm
	}
	cutoff := now.Add(-t.window)
	t.mu.Lock()
	defer t.mu.Unlock()
	history := t.records[key]
	pruned := history[:0]
	for _, at := range history {
		if at.Before(cutoff) {
			continue
		}
		pruned = append(pruned, at)
	}
	pruned = append(pruned, now)
	if len(pruned) > 64 {
		pruned = pruned[len(pruned)-64:]
	}
	t.records[key] = pruned
	count := len(pruned)
	return count, resolvePokeMoodTier(count, t.mild, t.annoyed)
}

func resolvePokeMoodTier(count, mild, annoyed int) pokeMoodTier {
	if count >= annoyed {
		return pokeMoodAnnoyed
	}
	if count >= mild {
		return pokeMoodMild
	}
	return pokeMoodWarm
}

func buildPokeReplyPrompt(count int, tier pokeMoodTier) string {
	var moodRule string
	switch tier {
	case pokeMoodAnnoyed:
		moodRule = "语气建议：明显烦躁，先温和表达边界感，再自然接一句能继续聊的话。"
	case pokeMoodMild:
		moodRule = "语气建议：轻微烦躁，可以小吐槽，但不要辱骂或攻击。"
	default:
		moodRule = "语气建议：自然、像网友、愿意接话。"
	}

	segmentRule := "如需多段输出，请使用精确分隔符 \\n---\\n；不要在开头或结尾输出分隔符；不要使用其他分段标记。"
	return fmt.Sprintf(
		"你在当前会话最近窗口内已经连续被戳了 %d 次，这代表有人想和你对话。请用中文自然回复，优先 1-3 句，像在和网友聊天，带互动感并尽量接上当前语境。不要使用引号。%s %s",
		count,
		moodRule,
		segmentRule,
	)
}

func pokeFallbackReplies(tier pokeMoodTier) []string {
	switch tier {
	case pokeMoodAnnoyed:
		return []string{
			"别连戳啦，有话直接说~",
			"我知道你在啦，直接开聊吧",
			"再戳我就要生气了，先说事",
		}
	case pokeMoodMild:
		return []string{
			"又戳我呀，说说你想聊啥~",
			"轻点戳啦，我在听你说",
			"别只戳嘛，直接开聊呀",
		}
	default:
		return []string{
			"戳到我啦，要聊点什么？",
			"我在呢，想聊什么呀~",
			"被你戳到了，来聊天吗？",
		}
	}
}

func pokeActorInfo(ctx *zero.Ctx) (speaker string, displayName string) {
	if ctx == nil || ctx.Event == nil {
		return "user", "对方"
	}
	label := strings.TrimSpace(speakerLabel(ctx))
	name, speakerID := speakerNameAndID(ctx)
	displayName = strings.TrimSpace(name)
	if displayName == "" {
		displayName = strings.TrimSpace(speakerID)
	}
	if displayName == "" {
		displayName = "对方"
	}
	if label == "" {
		if strings.TrimSpace(speakerID) != "" {
			label = "id=" + strings.TrimSpace(speakerID)
		} else {
			label = "user"
		}
	}
	return label, displayName
}
