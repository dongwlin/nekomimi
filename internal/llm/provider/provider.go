package provider

import (
	"context"

	"github.com/dongwlin/nekomimi/internal/llm/model"
)

type StreamHandler func(delta string) error

type Provider interface {
	Generate(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error)
	GenerateStream(ctx context.Context, modelName, systemPrompt string, messages []model.Message, onDelta StreamHandler) (string, error)
}
