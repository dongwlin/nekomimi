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

func pokeMaxReplyChars(cfg config.PokeReactionConfig) int {
	if cfg.MaxReplyChars <= 0 {
		return 20
	}
	return cfg.MaxReplyChars
}

func buildPokeReplyPrompt(count int, tier pokeMoodTier, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 20
	}
	var moodRule string
	switch tier {
	case pokeMoodAnnoyed:
		moodRule = "语气：明显烦躁，先温和表达边界感，再给一句可继续聊天的话。"
	case pokeMoodMild:
		moodRule = "语气：轻微烦躁，可以小吐槽，但不要辱骂或攻击。"
	default:
		moodRule = "语气：自然、可爱、愿意接话。"
	}
	return fmt.Sprintf(
		"你在当前会话最近窗口内已经连续被戳了 %d 次，这代表有人想和你对话。请用中文回复一句话，长度不超过%d字，不要加引号。%s 回复要带一点互动感，最好能自然接到对话。",
		count,
		maxChars,
		moodRule,
	)
}

func pokeFallbackReplies(tier pokeMoodTier) []string {
	switch tier {
	case pokeMoodAnnoyed:
		return []string{
			"别连戳啦，有话直接说~",
			"我知道你在啦，直接聊嘛",
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
