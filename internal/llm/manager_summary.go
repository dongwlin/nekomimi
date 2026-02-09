package llm

import (
	"context"
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/summarizer"
	"github.com/dongwlin/nekomimi/internal/llm/token"
)

func (m *Manager) compressMessages(ctx context.Context, provider, model, systemPrompt string, messages []Message) []Message {
	if m.contextMax <= 0 || len(messages) == 0 {
		return messages
	}
	threshold := m.contextMax * 80 / 100
	if threshold <= 0 {
		threshold = m.contextMax
	}
	if token.EstimateContextTokens(systemPrompt, messages) <= threshold {
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
		summary := m.summarizeWithProvider(ctx, provider, model, summarizer.ModeLight, head, 0)
		if strings.TrimSpace(summary) == "" {
			return reduceMessagesToFit(messages, systemPrompt, m.contextMax)
		}
		compressed := make([]Message, 0, tailCount+1)
		compressed = append(compressed, Message{
			Role:    "system",
			Content: "对话摘要（轻量压缩）: " + summary,
		})
		compressed = append(compressed, messages[len(messages)-tailCount:]...)
		if token.EstimateContextTokens(systemPrompt, compressed) <= m.contextMax {
			return compressed
		}
		return reduceMessagesToFit(compressed, systemPrompt, m.contextMax)
	}
	tailStart := len(messages) - keepLast
	summarySrc := messages[:tailStart]
	tail := messages[tailStart:]
	summary := m.summarizeWithProvider(ctx, provider, model, summarizer.ModeFull, summarySrc, 600)
	compressed := make([]Message, 0, len(tail)+1)
	if strings.TrimSpace(summary) != "" {
		compressed = append(compressed, Message{
			Role:    "system",
			Content: "对话摘要（自动压缩）: " + summary,
		})
	}
	compressed = append(compressed, tail...)
	if token.EstimateContextTokens(systemPrompt, compressed) <= m.contextMax {
		return compressed
	}
	return reduceMessagesToFit(compressed, systemPrompt, m.contextMax)
}

func (m *Manager) summarizeWithProvider(ctx context.Context, providerName, model string, mode summarizer.Mode, messages []Message, fallbackMaxChars int) string {
	if strings.TrimSpace(model) == "" || len(messages) == 0 {
		return ""
	}
	providerClient := m.providers.From(providerName)
	llmSummarizer := summarizer.NewLLM(providerClient, mode, m.contextMax)
	var fallback summarizer.Summarizer
	if fallbackMaxChars > 0 {
		fallback = summarizer.NewFallback(fallbackMaxChars)
	}
	chain := summarizer.NewChain(llmSummarizer, fallback)
	reqCtx := ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(reqCtx, llmclient.DefaultRequestTimeout)
	defer cancel()
	return chain.Summarize(reqCtx, model, messages)
}
