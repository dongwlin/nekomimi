package llm

import (
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
)

func reduceMessagesToFit(messages []Message, systemPrompt string, maxTokens int) []Message {
	if maxTokens <= 0 || len(messages) == 0 {
		return messages
	}
	for len(messages) > 1 && estimateContextTokens(systemPrompt, messages) > maxTokens {
		dropIndex := 0
		if isSummaryMessage(messages[0]) && len(messages) > 1 {
			dropIndex = 1
		}
		messages = append(messages[:dropIndex], messages[dropIndex+1:]...)
	}
	if estimateContextTokens(systemPrompt, messages) <= maxTokens {
		return messages
	}
	if len(messages) > 0 && isSummaryMessage(messages[0]) {
		budget := maxTokens - estimateContextTokens(systemPrompt, messages[1:])
		if budget > 4 {
			messages[0].Content = truncateToTokens(messages[0].Content, budget-4)
		}
	}
	if estimateContextTokens(systemPrompt, messages) <= maxTokens {
		return messages
	}
	for i := 0; i < len(messages) && estimateContextTokens(systemPrompt, messages) > maxTokens; i++ {
		contentTokens := estimateTokens(messages[i].Content)
		if contentTokens <= 20 {
			continue
		}
		target := contentTokens / 2
		if target < 20 {
			target = 20
		}
		messages[i].Content = truncateToTokens(messages[i].Content, target)
	}
	for len(messages) > 1 && estimateContextTokens(systemPrompt, messages) > maxTokens {
		messages = messages[1:]
	}
	return messages
}

func isSummaryMessage(msg Message) bool {
	return strings.TrimSpace(msg.Role) == "system" &&
		(strings.HasPrefix(strings.TrimSpace(msg.Content), "对话摘要（自动压缩）") ||
			strings.HasPrefix(strings.TrimSpace(msg.Content), "对话摘要（轻量压缩）") ||
			strings.HasPrefix(strings.TrimSpace(msg.Content), "对话摘要（历史压缩）"))
}

func estimateContextTokens(systemPrompt string, messages []Message) int {
	total := estimateTokens(systemPrompt) + 6
	for _, msg := range messages {
		total += estimateTokens(msg.Content) + 4
	}
	return total
}

func estimateTokens(text string) int {
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

func buildSummary(messages []Message, maxChars int) string {
	if len(messages) == 0 || maxChars <= 0 {
		return ""
	}
	var builder strings.Builder
	for _, msg := range messages {
		content := compactText(msg.Content, 80)
		if content == "" {
			continue
		}
		role := "用户"
		switch strings.TrimSpace(msg.Role) {
		case "assistant":
			role = "助手"
		case "system":
			role = "系统"
		}
		entry := role + ":" + content
		if builder.Len() > 0 {
			entry = "；" + entry
		}
		if builder.Len()+len([]rune(entry)) > maxChars {
			remaining := maxChars - len([]rune(builder.String()))
			if remaining > 0 {
				builder.WriteString(limitRunes(entry, remaining))
			}
			break
		}
		builder.WriteString(entry)
	}
	return builder.String()
}

func formatConversation(messages []Message, contextMax int) string {
	var builder strings.Builder
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := "用户"
		switch strings.TrimSpace(msg.Role) {
		case "assistant":
			role = "助手"
		case "system":
			role = "系统"
		}
		builder.WriteString(role)
		builder.WriteString(": ")
		builder.WriteString(content)
		builder.WriteString("\n")
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return ""
	}
	if contextMax > 0 {
		limit := contextMax - 256
		if limit < 256 {
			limit = contextMax
		}
		if limit > 0 {
			result = truncateToTokens(result, limit)
		}
	}
	return result
}

func composeSystemPrompt(basePrompt, extraPrompt string) string {
	base := strings.TrimSpace(basePrompt)
	extra := strings.TrimSpace(extraPrompt)
	if base == "" {
		return extra
	}
	if extra == "" {
		return base
	}
	return base + "\n" + extra
}

func formatUserContent(userInput, speaker string) string {
	content := strings.TrimSpace(userInput)
	if content == "" {
		return ""
	}
	label := strings.TrimSpace(speaker)
	if label == "" {
		return content
	}
	return "[" + label + "]: " + content
}

func compactText(text string, maxRunes int) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.Join(strings.Fields(clean), " ")
	return limitRunes(clean, maxRunes)
}

func limitRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func truncateToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return ""
	}
	if estimateTokens(text) <= maxTokens {
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

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case llmProviderOpenAI:
		return llmProviderOpenAI
	case llmProviderGemini:
		return llmProviderGemini
	default:
		return llmProviderResponses
	}
}

func normalizeAPIURL(provider, apiURL string) string {
	trimmed := strings.TrimSpace(apiURL)
	if trimmed == "" {
		if provider == llmProviderOpenAI {
			return llmclient.DefaultOpenAIAPI
		}
		return llmclient.DefaultLLMAPI
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "/chat/completions") {
		if provider == llmProviderOpenAI {
			return trimmed
		}
		return strings.Replace(trimmed, "/chat/completions", "/responses", 1)
	}
	if strings.Contains(lower, "/responses") {
		if provider != llmProviderOpenAI {
			return trimmed
		}
		return strings.Replace(trimmed, "/responses", "/chat/completions", 1)
	}
	trimmed = strings.TrimRight(trimmed, "/")
	if provider == llmProviderOpenAI {
		return trimmed + "/chat/completions"
	}
	return trimmed + "/responses"
}
