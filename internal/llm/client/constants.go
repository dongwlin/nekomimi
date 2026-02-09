package client

import "time"

const (
	DefaultLLMAPI         = "https://api.openai.com/v1/responses"
	DefaultOpenAIAPI      = "https://api.openai.com/v1/chat/completions"
	DefaultRequestTimeout = 30 * time.Second
)
