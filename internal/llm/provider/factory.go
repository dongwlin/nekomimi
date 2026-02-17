package provider

import (
	"context"
	"errors"
	"strings"

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
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai":
		return openAIProvider{client: f.client}
	case "gemini":
		return unsupportedProvider{err: errors.New("gemini 尚未接入")}
	default:
		return responsesProvider{client: f.client}
	}
}

type responsesProvider struct {
	client *llmclient.Client
}

func (p responsesProvider) Generate(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error) {
	return p.client.GenerateResponses(ctx, modelName, systemPrompt, messages)
}

func (p responsesProvider) GenerateStream(ctx context.Context, modelName, systemPrompt string, messages []model.Message, onDelta StreamHandler) (string, error) {
	return p.client.GenerateResponsesStream(ctx, modelName, systemPrompt, messages, onDelta)
}

type openAIProvider struct {
	client *llmclient.Client
}

func (p openAIProvider) Generate(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error) {
	return p.client.GenerateOpenAI(ctx, modelName, systemPrompt, messages)
}

func (p openAIProvider) GenerateStream(ctx context.Context, modelName, systemPrompt string, messages []model.Message, onDelta StreamHandler) (string, error) {
	return p.client.GenerateOpenAIStream(ctx, modelName, systemPrompt, messages, onDelta)
}

type unsupportedProvider struct {
	err error
}

func (p unsupportedProvider) Generate(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error) {
	return "", p.err
}

func (p unsupportedProvider) GenerateStream(ctx context.Context, modelName, systemPrompt string, messages []model.Message, onDelta StreamHandler) (string, error) {
	_ = onDelta
	return "", p.err
}
