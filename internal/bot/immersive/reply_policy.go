package immersive

import (
	"fmt"
	"strings"

	"github.com/dongwlin/nekomimi/internal/ctxasm"
)

const privateSessionPrefix = "private:"

func shouldBypassControlIntent(sessionKey string, meta queueMeta, gate speakGateDecision) bool {
	if !strings.HasPrefix(strings.TrimSpace(sessionKey), privateSessionPrefix) {
		return false
	}
	if !gate.StrongCall {
		return false
	}
	if meta.AddressedToBot <= 0 {
		return false
	}
	return meta.QuestionsCount > 0 || meta.MentionsToBot > 0 || meta.DirectedQuestions > 0
}

func buildImmersiveReplyPrompt(immersiveCtx *ctxasm.ImmersiveContext) string {
	maxSegments := 1
	replyTier := "brief"
	if immersiveCtx != nil {
		if immersiveCtx.MaxReplySegments > 0 {
			maxSegments = immersiveCtx.MaxReplySegments
		}
		if trimmed := strings.TrimSpace(immersiveCtx.ReplyTier); trimmed != "" {
			replyTier = trimmed
		}
	}

	rules := make([]string, 0, 4)
	switch replyTier {
	case "engaged":
		rules = append(rules, "保持聊天感，可以稍微展开，但不要写成长段。")
	case "normal":
		rules = append(rules, "保持自然、轻松，优先短句。")
	default:
		rules = append(rules, "保持简短，优先一两句。")
	}

	if maxSegments <= 1 {
		rules = append(rules, "直接输出 1 段回复，不要使用分段分隔符。")
	} else {
		rules = append(rules, fmt.Sprintf("优先 1-%d 段短句回复。", maxSegments))
		rules = append(rules, "如需多段输出，只能使用精确分隔符 \\n---\\n。")
		rules = append(rules, "不要在开头或结尾输出分隔符，也不要使用其他分段标记。")
	}

	return strings.Join(rules, " ")
}
