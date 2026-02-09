package llm

import (
	"context"
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
)

func (m *Manager) compressMessages(ctx context.Context, provider, model, systemPrompt string, messages []Message) []Message {
	if m.contextMax <= 0 || len(messages) == 0 {
		return messages
	}
	if estimateContextTokens(systemPrompt, messages) <= m.contextMax {
		return messages
	}
	keepLast := 6
	if keepLast < 2 {
		keepLast = 2
	}
	if len(messages) <= keepLast {
		tailCount := 2
		if len(messages) < tailCount {
			tailCount = len(messages)
		}
		head := messages[:len(messages)-tailCount]
		if len(head) == 0 {
			return reduceMessagesToFit(messages, systemPrompt, m.contextMax)
		}
		summary := m.buildLightSummaryWithModel(ctx, provider, model, head)
		if strings.TrimSpace(summary) == "" {
			return reduceMessagesToFit(messages, systemPrompt, m.contextMax)
		}
		compressed := make([]Message, 0, tailCount+1)
		compressed = append(compressed, Message{
			Role:    "system",
			Content: "对话摘要（轻量压缩）: " + summary,
		})
		compressed = append(compressed, messages[len(messages)-tailCount:]...)
		if estimateContextTokens(systemPrompt, compressed) <= m.contextMax {
			return compressed
		}
		return reduceMessagesToFit(compressed, systemPrompt, m.contextMax)
	}
	tailStart := len(messages) - keepLast
	summarySrc := messages[:tailStart]
	tail := messages[tailStart:]
	summary := m.buildSummaryWithModel(ctx, provider, model, summarySrc)
	if strings.TrimSpace(summary) == "" {
		summary = buildSummary(summarySrc, 600)
	}
	compressed := make([]Message, 0, len(tail)+1)
	if strings.TrimSpace(summary) != "" {
		compressed = append(compressed, Message{
			Role:    "system",
			Content: "对话摘要（自动压缩）: " + summary,
		})
	}
	compressed = append(compressed, tail...)
	if estimateContextTokens(systemPrompt, compressed) <= m.contextMax {
		return compressed
	}
	return reduceMessagesToFit(compressed, systemPrompt, m.contextMax)
}

func (m *Manager) buildSummaryWithModel(ctx context.Context, provider, model string, messages []Message) string {
	if strings.TrimSpace(model) == "" || len(messages) == 0 {
		return ""
	}
	conversation := formatConversation(messages, m.contextMax)
	if strings.TrimSpace(conversation) == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, llmclient.DefaultRequestTimeout)
	defer cancel()
	input := []Message{{Role: "user", Content: conversation}}
	var summary string
	var err error
	switch provider {
	case llmProviderOpenAI:
		summary, err = m.client.GenerateOpenAI(reqCtx, model, llmprompt.SummarySystemPrompt, input)
	case llmProviderGemini:
		return ""
	default:
		summary, err = m.client.GenerateResponses(reqCtx, model, llmprompt.SummarySystemPrompt, input)
	}
	if err != nil {
		return ""
	}
	return strings.TrimSpace(summary)
}

func (m *Manager) buildLightSummaryWithModel(ctx context.Context, provider, model string, messages []Message) string {
	if strings.TrimSpace(model) == "" || len(messages) == 0 {
		return ""
	}
	conversation := formatConversation(messages, m.contextMax)
	if strings.TrimSpace(conversation) == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, llmclient.DefaultRequestTimeout)
	defer cancel()
	input := []Message{{Role: "user", Content: conversation}}
	var summary string
	var err error
	switch provider {
	case llmProviderOpenAI:
		summary, err = m.client.GenerateOpenAI(reqCtx, model, llmprompt.LightSummaryPrompt, input)
	case llmProviderGemini:
		return ""
	default:
		summary, err = m.client.GenerateResponses(reqCtx, model, llmprompt.LightSummaryPrompt, input)
	}
	if err != nil {
		return ""
	}
	return strings.TrimSpace(summary)
}
