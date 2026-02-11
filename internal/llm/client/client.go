package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/rs/zerolog/log"
)

type Client struct {
	mu              sync.RWMutex
	apiURL          string
	apiKey          string
	reasoningEffort string
	showReasoning   bool
	httpClient      *http.Client
}

func New(apiURL, apiKey string) *Client {
	if strings.TrimSpace(apiURL) == "" {
		apiURL = DefaultLLMAPI
	}
	return &Client{
		apiURL: apiURL,
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: DefaultRequestTimeout,
		},
	}
}

func (c *Client) SetAPIURL(apiURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(apiURL) == "" {
		c.apiURL = DefaultLLMAPI
		return
	}
	c.apiURL = apiURL
}

func (c *Client) APIURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiURL
}

func normalizeReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low", "medium", "high", "none":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return ""
	}
}

func (c *Client) SetReasoningEffort(effort string) {
	raw := strings.TrimSpace(effort)
	normalized := normalizeReasoningEffort(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	if normalized == "none" {
		c.reasoningEffort = ""
		return
	}
	c.reasoningEffort = normalized
	if raw != "" && normalized == "" {
		log.Warn().
			Str("reasoning_effort", raw).
			Msg("invalid reasoning_effort, reasoning disabled")
	}
}

func (c *Client) reasoningEffortSnapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reasoningEffort
}

func (c *Client) SetShowReasoning(show bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.showReasoning = show
}

func (c *Client) showReasoningSnapshot() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.showReasoning
}

func (c *Client) apiURLSnapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiURL
}

func (c *Client) ensureAPIKey() error {
	if strings.TrimSpace(c.apiKey) == "" {
		return errors.New("未配置 API Key")
	}
	return nil
}

func buildResponsesInput(systemPrompt string, messages []model.Message) []responsesInputMessage {
	input := make([]responsesInputMessage, 0, len(messages)+1)
	if strings.TrimSpace(systemPrompt) != "" {
		input = append(input, responsesInputMessage{
			Role: "system",
			Content: []responsesContent{
				{Type: "text", Text: systemPrompt},
			},
		})
	}
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}
		input = append(input, responsesInputMessage{
			Role: role,
			Content: []responsesContent{
				{Type: "text", Text: content},
			},
		})
	}
	return input
}

func buildChatMessages(systemPrompt string, messages []model.Message) []chatMessage {
	chatMessages := make([]chatMessage, 0, len(messages)+1)
	if strings.TrimSpace(systemPrompt) != "" {
		chatMessages = append(chatMessages, chatMessage{Role: "system", Content: systemPrompt})
	}
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}
		chatMessages = append(chatMessages, chatMessage{Role: role, Content: content})
	}
	return chatMessages
}

func (c *Client) postJSON(ctx context.Context, apiURL string, reqBody any) ([]byte, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("请求失败: %s", strings.TrimSpace(string(body)))
	}
	return body, nil
}

func parseResponsesText(parsed responsesResponse) (string, string, error) {
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", "", errors.New(parsed.Error.Message)
	}

	var builder strings.Builder
	var reasoningBuilder strings.Builder
	for _, item := range parsed.Output {
		if strings.EqualFold(strings.TrimSpace(item.Type), "reasoning") {
			for _, summary := range item.Summary {
				if strings.TrimSpace(summary.Text) != "" {
					if reasoningBuilder.Len() > 0 {
						reasoningBuilder.WriteString("\n")
					}
					reasoningBuilder.WriteString(strings.TrimSpace(summary.Text))
				}
			}
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				builder.WriteString(content.Text)
			}
			if strings.Contains(strings.ToLower(strings.TrimSpace(content.Type)), "reasoning") && strings.TrimSpace(content.Text) != "" {
				if reasoningBuilder.Len() > 0 {
					reasoningBuilder.WriteString("\n")
				}
				reasoningBuilder.WriteString(strings.TrimSpace(content.Text))
			}
		}
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return "", "", errors.New("模型未返回文本内容")
	}
	return result, strings.TrimSpace(reasoningBuilder.String()), nil
}

func parseChatCompletionText(parsed chatCompletionsResponse) (string, string, error) {
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", "", errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", "", errors.New("模型未返回文本内容")
	}
	result := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if result == "" {
		return "", "", errors.New("模型未返回文本内容")
	}
	return result, strings.TrimSpace(parsed.Choices[0].Message.ReasoningContent), nil
}

func (c *Client) GenerateResponses(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error) {
	if err := c.ensureAPIKey(); err != nil {
		return "", err
	}
	requestOptions, _ := requestOptionsFromContext(ctx)
	reasoningEffort := c.reasoningEffortSnapshot()
	logReasoningEffort := reasoningEffort
	if override := normalizeReasoningEffort(requestOptions.ReasoningEffort); override != "" {
		if override == "none" {
			reasoningEffort = ""
			logReasoningEffort = "none"
		} else {
			reasoningEffort = override
			logReasoningEffort = override
		}
	}
	requestSource := strings.TrimSpace(requestOptions.Source)
	if requestSource == "" {
		requestSource = "default"
	}
	input := buildResponsesInput(systemPrompt, messages)
	reqBody := responsesRequest{
		Model: modelName,
		Input: input,
	}
	if reasoningEffort != "" {
		reqBody.Reasoning = &responsesReasoning{Effort: reasoningEffort}
	}
	apiURL := c.apiURLSnapshot()
	log.Info().
		Str("llm_api", "responses").
		Str("request_source", requestSource).
		Str("api_url", apiURL).
		Str("model", strings.TrimSpace(modelName)).
		Int("message_count", len(input)).
		Bool("has_system_prompt", strings.TrimSpace(systemPrompt) != "").
		Bool("reasoning_enabled", reqBody.Reasoning != nil).
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Str("reasoning_effort", logReasoningEffort).
		Msg("sending llm request")
	body, err := c.postJSON(ctx, apiURL, reqBody)
	if err != nil {
		return "", err
	}

	var parsed responsesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	reply, reasoning, err := parseResponsesText(parsed)
	if err != nil {
		return "", err
	}
	log.Info().
		Str("llm_api", "responses").
		Str("request_source", requestSource).
		Int("reply_chars", len([]rune(reply))).
		Int("reasoning_chars", len([]rune(reasoning))).
		Msg("llm response received")
	if c.showReasoningSnapshot() && strings.TrimSpace(reasoning) != "" {
		log.Info().
			Str("llm_api", "responses").
			Str("request_source", requestSource).
			Str("reasoning_content", reasoning).
			Msg("llm reasoning content")
	}
	return reply, nil
}

func (c *Client) GenerateOpenAI(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error) {
	if err := c.ensureAPIKey(); err != nil {
		return "", err
	}
	requestOptions, _ := requestOptionsFromContext(ctx)
	reasoningEffort := c.reasoningEffortSnapshot()
	logReasoningEffort := reasoningEffort
	if override := normalizeReasoningEffort(requestOptions.ReasoningEffort); override != "" {
		if override == "none" {
			reasoningEffort = ""
			logReasoningEffort = "none"
		} else {
			reasoningEffort = override
			logReasoningEffort = override
		}
	}
	requestSource := strings.TrimSpace(requestOptions.Source)
	if requestSource == "" {
		requestSource = "default"
	}
	chatMessages := buildChatMessages(systemPrompt, messages)
	reqBody := chatCompletionsRequest{
		Model:           modelName,
		Messages:        chatMessages,
		ReasoningEffort: reasoningEffort,
	}
	apiURL := c.apiURLSnapshot()
	log.Info().
		Str("llm_api", "chat_completions").
		Str("request_source", requestSource).
		Str("api_url", apiURL).
		Str("model", strings.TrimSpace(modelName)).
		Int("message_count", len(chatMessages)).
		Bool("has_system_prompt", strings.TrimSpace(systemPrompt) != "").
		Bool("reasoning_enabled", strings.TrimSpace(reqBody.ReasoningEffort) != "").
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Str("reasoning_effort", logReasoningEffort).
		Msg("sending llm request")
	body, err := c.postJSON(ctx, apiURL, reqBody)
	if err != nil {
		return "", err
	}

	var parsed chatCompletionsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	reply, reasoning, err := parseChatCompletionText(parsed)
	if err != nil {
		return "", err
	}
	log.Info().
		Str("llm_api", "chat_completions").
		Str("request_source", requestSource).
		Int("reply_chars", len([]rune(reply))).
		Int("reasoning_chars", len([]rune(reasoning))).
		Msg("llm response received")
	if c.showReasoningSnapshot() && strings.TrimSpace(reasoning) != "" {
		log.Info().
			Str("llm_api", "chat_completions").
			Str("request_source", requestSource).
			Str("reasoning_content", reasoning).
			Msg("llm reasoning content")
	}
	return reply, nil
}
