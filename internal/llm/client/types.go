package client

type responsesRequest struct {
	Model     string                  `json:"model"`
	Input     []responsesInputMessage `json:"input"`
	Reasoning *responsesReasoning     `json:"reasoning,omitempty"`
	Thinking  *thinkingConfig         `json:"thinking,omitempty"`
	Stream    bool                    `json:"stream,omitempty"`
}

type responsesInputMessage struct {
	Role    string             `json:"role"`
	Content []responsesContent `json:"content"`
}

type responsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesReasoning struct {
	Effort string `json:"effort"`
}

type thinkingConfig struct {
	Type string `json:"type"`
}

type responsesResponse struct {
	Output []struct {
		Type    string `json:"type"`
		Summary []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type chatCompletionsRequest struct {
	Model           string          `json:"model"`
	Messages        []chatMessage   `json:"messages"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	Thinking        *thinkingConfig `json:"thinking,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type chatCompletionsStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}
