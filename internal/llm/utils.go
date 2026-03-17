package llm

import (
	"strings"
	"time"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
)

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
	return formatUserContentAt(userInput, speaker, time.Now())
}

func formatUserContentAt(userInput, speaker string, at time.Time) string {
	content := strings.TrimSpace(userInput)
	if content == "" {
		return ""
	}
	timeLabel := formatMessageTime(at)
	label := strings.TrimSpace(speaker)
	if label == "" {
		if timeLabel == "" {
			return content
		}
		return "[time=" + timeLabel + "]: " + content
	}
	if timeLabel == "" {
		return "[" + label + "]: " + content
	}
	return "[" + label + ";time=" + timeLabel + "]: " + content
}

func formatMessageTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format("2006-01-02 15:04:05")
}

func normalizeAPIURL(apiURL string) string {
	trimmed := strings.TrimSpace(apiURL)
	if trimmed == "" {
		return llmclient.DefaultAnthropicAPI
	}
	return trimmed
}
