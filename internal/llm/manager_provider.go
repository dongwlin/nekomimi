package llm

import (
	"context"
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
)

func (m *Manager) generateWithProvider(ctx context.Context, model, systemPrompt string, messages []Message, options llmclient.RequestOptions) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = llmclient.WithRequestOptions(ctx, options)
	timeout := m.current.requestTimeout
	if timeout <= 0 {
		timeout = llmclient.DefaultRequestTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	providerClient := m.providers.From()
	return providerClient.Generate(reqCtx, model, systemPrompt, messages)
}

func (m *Manager) generateStreamWithProvider(ctx context.Context, model, systemPrompt string, messages []Message, options llmclient.RequestOptions, onDelta func(delta string) error) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = llmclient.WithRequestOptions(ctx, options)
	timeout := m.current.requestTimeout
	if timeout <= 0 {
		timeout = llmclient.DefaultRequestTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	providerClient := m.providers.From()
	return providerClient.GenerateStream(reqCtx, model, systemPrompt, messages, onDelta)
}

func withRequestSource(options llmclient.RequestOptions, source string) llmclient.RequestOptions {
	next := options
	next.Source = strings.TrimSpace(source)
	return next
}
