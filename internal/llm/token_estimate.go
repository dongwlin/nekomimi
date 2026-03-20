package llm

import "github.com/dongwlin/nekomimi/internal/llm/model"

func estimateTextTokens(text string) int {
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

func estimateContextTokens(systemPrompt string, messages []model.Message) int {
	total := estimateTextTokens(systemPrompt) + 6
	for _, msg := range messages {
		total += estimateTextTokens(msg.Content) + 4
	}
	return total
}
