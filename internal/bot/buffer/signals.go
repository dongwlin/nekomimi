package buffer

import (
	"strconv"
	"strings"

	zero "github.com/wdvxdr1123/ZeroBot"
)

// detectMessageSignals analyzes a message to determine if it contains signals
// that indicate the bot should respond. Returns three booleans:
// - mention: whether the bot was explicitly mentioned
// - addressed: whether the message appears to be directed at the bot
// - question: whether the message appears to be a question
func (b *ImmersiveBuffer) detectMessageSignals(ctx *zero.Ctx, text string) (bool, bool, bool) {
	mention := b.isExplicitMention(ctx)
	addressed := mention || b.containsNickname(text) || strings.Contains(text, "@")
	question := looksLikeQuestion(text)
	return mention, addressed, question
}

// isExplicitMention checks if the message contains an explicit @mention of the bot
// through ZeroBot event data (IsToMe flag or at segment in message).
func (b *ImmersiveBuffer) isExplicitMention(ctx *zero.Ctx) bool {
	if ctx != nil && ctx.Event != nil {
		if ctx.Event.IsToMe {
			return true
		}
		for _, seg := range ctx.Event.Message {
			if seg.Type != "at" {
				continue
			}
			qq := strings.TrimSpace(seg.Data["qq"])
			if qq == "" || qq == "all" {
				continue
			}
			if ctx.Event.SelfID != 0 && qq == strconv.FormatInt(ctx.Event.SelfID, 10) {
				return true
			}
			if ctx.Event.SelfTinyID != "" && qq == ctx.Event.SelfTinyID {
				return true
			}
		}
	}
	return false
}

// containsNickname checks if the message text contains any of the bot's nicknames.
func (b *ImmersiveBuffer) containsNickname(text string) bool {
	lower := strings.ToLower(text)
	for _, name := range b.nicknames {
		trimmed := strings.ToLower(strings.TrimSpace(name))
		if trimmed == "" {
			continue
		}
		if strings.Contains(lower, trimmed) {
			return true
		}
	}
	return false
}

// looksLikeQuestion determines if the text appears to be a question by checking
// for question marks (both English and Chinese) or question-forming keywords.
func looksLikeQuestion(text string) bool {
	if strings.Contains(text, "?") || strings.Contains(text, "？") {
		return true
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "吗") || strings.Contains(lower, "能否")
}
