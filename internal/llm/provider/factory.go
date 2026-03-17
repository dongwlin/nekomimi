package provider

import (
	"context"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/model"
)

type Factory struct {
	client *llmclient.Client
}

func NewFactory(client *llmclient.Client) *Factory {
	return &Factory{client: client}
}

func (f *Factory) From(name string) Provider {
	// Since we're only using Anthropic SDK, always return the Anthropic provider
	return anthropicProvider{client: f.client}
}

type anthropicProvider struct {
	client *llmclient.Client
}

func (p anthropicProvider) Generate(
	ctx context.Context,
	modelName, systemPrompt string,
	messages []model.Message,
) (string, error) {
	return p.client.Generate(ctx, modelName, systemPrompt, messages)
}

func (p anthropicProvider) GenerateStream(
	ctx context.Context,
	modelName, systemPrompt string,
	messages []model.Message,
	onDelta StreamHandler,
) (string, error) {
	return p.client.GenerateStream(ctx, modelName, systemPrompt, messages, onDelta)
}
