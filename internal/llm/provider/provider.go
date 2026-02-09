package provider

import (
	"context"

	"github.com/dongwlin/nekomimi/internal/llm/model"
)

type Provider interface {
	Generate(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error)
}

