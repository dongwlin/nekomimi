package llm

import "github.com/dongwlin/nekomimi/internal/llm/model"

const (
	llmProviderResponses = "responses"
	llmProviderOpenAI    = "openai"
	llmProviderGemini    = "gemini"
)

type Message = model.Message
