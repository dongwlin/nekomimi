package token

import (
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm/model"
)

func EstimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	ascii := 0
	nonASCII := 0
	for _, r := range text {
		if r <= 0x7f {
			ascii++
		} else {
			nonASCII++
		}
	}
	tokens := (ascii+3)/4 + nonASCII
	if tokens < 0 {
		return 0
	}
	return tokens
}

func EstimateContextTokens(systemPrompt string, messages []model.Message) int {
	total := EstimateTextTokens(systemPrompt) + 6
	for _, msg := range messages {
		total += EstimateTextTokens(msg.Content) + 4
	}
	return total
}

func TruncateToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return ""
	}
	if EstimateTextTokens(text) <= maxTokens {
		return text
	}
	target := maxTokens
	if maxTokens > 4 {
		target = maxTokens - 1
	}
	var builder strings.Builder
	ascii := 0
	nonASCII := 0
	for _, r := range text {
		if r <= 0x7f {
			ascii++
		} else {
			nonASCII++
		}
		tokens := (ascii+3)/4 + nonASCII
		if tokens > target {
			break
		}
		builder.WriteRune(r)
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return ""
	}
	return result + "..."
}

