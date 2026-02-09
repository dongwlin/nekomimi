package llm

import (
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/token"
)

func reduceMessagesToFit(messages []Message, systemPrompt string, maxTokens int) []Message {
	if maxTokens <= 0 || len(messages) == 0 {
		return messages
	}
	for len(messages) > 1 && token.EstimateContextTokens(systemPrompt, messages) > maxTokens {
		dropIndex := 0
		if isSummaryMessage(messages[0]) && len(messages) > 1 {
			dropIndex = 1
		}
		messages = append(messages[:dropIndex], messages[dropIndex+1:]...)
	}
	if token.EstimateContextTokens(systemPrompt, messages) <= maxTokens {
		return messages
	}
	if len(messages) > 0 && isSummaryMessage(messages[0]) {
		budget := maxTokens - token.EstimateContextTokens(systemPrompt, messages[1:])
		if budget > 4 {
			messages[0].Content = token.TruncateToTokens(messages[0].Content, budget-4)
		}
	}
	if token.EstimateContextTokens(systemPrompt, messages) <= maxTokens {
		return messages
	}
	for i := 0; i < len(messages) && token.EstimateContextTokens(systemPrompt, messages) > maxTokens; i++ {
		contentTokens := token.EstimateTextTokens(messages[i].Content)
		if contentTokens <= 20 {
			continue
		}
		target := contentTokens / 2
		if target < 20 {
			target = 20
		}
		messages[i].Content = token.TruncateToTokens(messages[i].Content, target)
	}
	for len(messages) > 1 && token.EstimateContextTokens(systemPrompt, messages) > maxTokens {
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
