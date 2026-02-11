package llm

import (
	"context"
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/summarizer"
	"github.com/dongwlin/nekomimi/internal/llm/token"
)

func (m *Manager) compressMessages(ctx context.Context, provider, model, systemPrompt, sessionKey string, messages []Message) []Message {
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
	compressedCounted := false
	markCompressed := func(result []Message) []Message {
		if !compressedCounted && !messagesEqual(messages, result) {
			m.incrementContextCompressCount(sessionKey)
			compressedCounted = true
		}
		return result
	}
	if len(messages) <= keepLast {
		tailCount := 2
		if len(messages) < tailCount {
			tailCount = len(messages)
		}
		head := messages[:len(messages)-tailCount]
		if len(head) == 0 {
			return markCompressed(reduceMessagesToFit(messages, systemPrompt, m.contextMax))
		}
		summary := m.summarizeWithProvider(ctx, provider, model, summarizer.ModeLight, head, 0)
		if strings.TrimSpace(summary) == "" {
			return markCompressed(reduceMessagesToFit(messages, systemPrompt, m.contextMax))
		}
		compressed := make([]Message, 0, tailCount+1)
		compressed = append(compressed, Message{
			Role:    "system",
			Content: "对话摘要（轻量压缩）: " + summary,
		})
		compressed = append(compressed, messages[len(messages)-tailCount:]...)
		if token.EstimateContextTokens(systemPrompt, compressed) <= m.contextMax {
			return markCompressed(compressed)
		}
		return markCompressed(reduceMessagesToFit(compressed, systemPrompt, m.contextMax))
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
		return markCompressed(compressed)
	}
	return markCompressed(reduceMessagesToFit(compressed, systemPrompt, m.contextMax))
}

func messagesEqual(a, b []Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content {
			return false
		}
	}
	return true
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
	timeout := m.requestTimeout
	if timeout <= 0 {
		timeout = llmclient.DefaultRequestTimeout
	}
	reqCtx, cancel := context.WithTimeout(reqCtx, timeout)
	defer cancel()
	return chain.Summarize(reqCtx, model, messages)
}
