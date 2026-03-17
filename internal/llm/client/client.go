package client

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/rs/zerolog/log"
)

const (
	DefaultMaxTokens = 8192
)

type Client struct {
	mu              sync.RWMutex
	apiKey          string
	baseURL         string
	reasoningEffort string
	thinkingType    string
	showReasoning   bool
	httpClient      anthropic.Client
}

var urlInTextPattern = regexp.MustCompile(`https?://[^\s"]+`)

func redactURLs(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return trimmed
	}
	return urlInTextPattern.ReplaceAllString(trimmed, "[redacted-url]")
}

func sanitizeRequestError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(redactURLs(err.Error()))
}

func New(apiURL, apiKey string) *Client {
	if strings.TrimSpace(apiURL) == "" {
		apiURL = DefaultAnthropicAPI
	}
	c := &Client{
		baseURL: apiURL,
		apiKey:  apiKey,
	}
	c.rebuildHTTPClient()
	return c
}

func (c *Client) rebuildHTTPClient() {
	c.mu.Lock()
	defer c.mu.Unlock()

	opts := []option.RequestOption{
		option.WithAPIKey(c.apiKey),
		option.WithBaseURL(c.baseURL),
	}
	c.httpClient = anthropic.NewClient(opts...)
}

func (c *Client) SetAPIURL(apiURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(apiURL) == "" {
		c.baseURL = DefaultAnthropicAPI
	} else {
		c.baseURL = apiURL
	}
	c.rebuildHTTPClient()
}

func (c *Client) APIURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

func (c *Client) SetAPIKey(apiKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.apiKey = strings.TrimSpace(apiKey)
	c.rebuildHTTPClient()
}

func normalizeReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal", "low", "medium", "high", "none":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return ""
	}
}

func normalizeThinkingType(thinkingType string) string {
	switch strings.ToLower(strings.TrimSpace(thinkingType)) {
	case "enabled", "disabled", "auto":
		return strings.ToLower(strings.TrimSpace(thinkingType))
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

func (c *Client) SetThinkingType(thinkingType string) {
	raw := strings.TrimSpace(thinkingType)
	normalized := normalizeThinkingType(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.thinkingType = normalized
	if raw != "" && normalized == "" {
		log.Warn().
			Str("thinking_type", raw).
			Msg("invalid thinking_type, thinking disabled")
	}
}

func (c *Client) reasoningEffortSnapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reasoningEffort
}

func (c *Client) thinkingTypeSnapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.thinkingType
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

func (c *Client) baseURLSnapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

func (c *Client) ensureAPIKey() error {
	if strings.TrimSpace(c.apiKey) == "" {
		return errors.New("未配置 API Key")
	}
	return nil
}

func (c *Client) Generate(
	ctx context.Context,
	modelName, systemPrompt string,
	messages []model.Message,
) (string, error) {
	startedAt := time.Now()
	if err := c.ensureAPIKey(); err != nil {
		return "", err
	}

	requestOptions, _ := requestOptionsFromContext(ctx)
	reasoningEffort := c.reasoningEffortSnapshot()
	thinkingType := c.thinkingTypeSnapshot()
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
	if strings.EqualFold(strings.TrimSpace(requestOptions.ThinkingType), "none") {
		thinkingType = ""
	}
	if override := normalizeThinkingType(requestOptions.ThinkingType); override != "" {
		thinkingType = override
	}

	requestSource := strings.TrimSpace(requestOptions.Source)
	if requestSource == "" {
		requestSource = "default"
	}

	inputMessages := buildMessagesInput(messages)

	log.Info().
		Str("llm_api", "messages").
		Str("request_source", requestSource).
		Bool("api_url_configured", strings.TrimSpace(c.baseURLSnapshot()) != "").
		Str("model", strings.TrimSpace(modelName)).
		Int("message_count", len(inputMessages)).
		Bool("has_system_prompt", strings.TrimSpace(systemPrompt) != "").
		Bool("reasoning_enabled", reasoningEffort != "").
		Bool("thinking_enabled", thinkingType != "").
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Str("reasoning_effort", logReasoningEffort).
		Str("thinking_type", thinkingType).
		Msg("sending llm request")

	params := buildMessageParams(modelName, inputMessages, systemPrompt, reasoningEffort, thinkingType)
	resp, err := c.httpClient.Messages.New(ctx, params)
	if err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "messages").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm request failed")
		return "", sanitizeRequestError(err)
	}

	reply, reasoning, err := extractMessageContent(resp)
	if err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "messages").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm response extraction failed")
		return "", err
	}

	log.Info().
		Str("llm_api", "messages").
		Str("request_source", requestSource).
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Int("reply_chars", len([]rune(reply))).
		Int("reasoning_chars", len([]rune(reasoning))).
		Msg("llm response received")

	if c.showReasoningSnapshot() && strings.TrimSpace(reasoning) != "" {
		log.Info().
			Str("llm_api", "messages").
			Str("request_source", requestSource).
			Str("reasoning_content", reasoning).
			Msg("llm reasoning content")
	}

	return reply, nil
}

func (c *Client) GenerateStream(
	ctx context.Context,
	modelName, systemPrompt string,
	messages []model.Message,
	onDelta func(delta string) error,
) (string, error) {
	startedAt := time.Now()
	if err := c.ensureAPIKey(); err != nil {
		return "", err
	}

	requestOptions, _ := requestOptionsFromContext(ctx)
	reasoningEffort := c.reasoningEffortSnapshot()
	thinkingType := c.thinkingTypeSnapshot()
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
	if strings.EqualFold(strings.TrimSpace(requestOptions.ThinkingType), "none") {
		thinkingType = ""
	}
	if override := normalizeThinkingType(requestOptions.ThinkingType); override != "" {
		thinkingType = override
	}

	requestSource := strings.TrimSpace(requestOptions.Source)
	if requestSource == "" {
		requestSource = "default"
	}

	inputMessages := buildMessagesInput(messages)

	log.Info().
		Str("llm_api", "messages_stream").
		Str("request_source", requestSource).
		Bool("api_url_configured", strings.TrimSpace(c.baseURLSnapshot()) != "").
		Str("model", strings.TrimSpace(modelName)).
		Int("message_count", len(inputMessages)).
		Bool("has_system_prompt", strings.TrimSpace(systemPrompt) != "").
		Bool("reasoning_enabled", reasoningEffort != "").
		Bool("thinking_enabled", thinkingType != "").
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Str("reasoning_effort", logReasoningEffort).
		Str("thinking_type", thinkingType).
		Msg("sending llm streaming request")

	params := buildMessageParams(modelName, inputMessages, systemPrompt, reasoningEffort, thinkingType)
	stream := c.httpClient.Messages.NewStreaming(ctx, params)

	var replyBuilder strings.Builder
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "content_block_delta":
			delta := event.Delta.Text
			if delta != "" {
				replyBuilder.WriteString(delta)
				if onDelta != nil {
					if err := onDelta(delta); err != nil {
						stream.Close()
						return "", err
					}
				}
			}
		}
	}

	if err := stream.Close(); err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "messages_stream").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm streaming close failed")
		return "", sanitizeRequestError(err)
	}

	reply := strings.TrimSpace(replyBuilder.String())
	if reply == "" {
		log.Warn().
			Str("llm_api", "messages_stream").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm streaming response empty")
		return "", errors.New("模型未返回文本内容")
	}

	log.Info().
		Str("llm_api", "messages_stream").
		Str("request_source", requestSource).
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Int("reply_chars", len([]rune(reply))).
		Msg("llm streaming response received")

	return reply, nil
}

func buildMessagesInput(messages []model.Message) []anthropic.MessageParam {
	result := make([]anthropic.MessageParam, 0, len(messages))

	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}

		switch role {
		case "user":
			result = append(result, anthropic.NewUserMessage(
				anthropic.NewTextBlock(content),
			))
		case "assistant":
			result = append(result, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(content),
			))
		}
	}

	return result
}

func buildMessageParams(
	modelName string,
	messages []anthropic.MessageParam,
	systemPrompt, reasoningEffort, thinkingType string,
) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(strings.TrimSpace(modelName)),
		Messages:  messages,
		MaxTokens: int64(DefaultMaxTokens),
	}

	// Handle system prompt
	if strings.TrimSpace(systemPrompt) != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: systemPrompt},
		}
	}

	// Handle thinking/reasoning configuration
	if reasoningEffort != "" {
		// Use thinking with effort when reasoning is enabled
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(1024)
	} else if thinkingType != "" && thinkingType != "disabled" {
		// Use thinking with type when explicitly enabled but no effort
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(1024)
	}

	return params
}

func extractMessageContent(msg *anthropic.Message) (string, string, error) {
	var builder strings.Builder
	var reasoningBuilder strings.Builder

	for _, block := range msg.Content {
		switch block.Type {
		case "thinking":
			if block.Thinking != "" {
				if reasoningBuilder.Len() > 0 {
					reasoningBuilder.WriteString("\n")
				}
				reasoningBuilder.WriteString(strings.TrimSpace(block.Thinking))
			}
		case "text":
			if block.Text != "" {
				builder.WriteString(block.Text)
			}
		}
	}

	result := strings.TrimSpace(builder.String())
	if result == "" {
		return "", "", errors.New("模型未返回文本内容")
	}

	return result, strings.TrimSpace(reasoningBuilder.String()), nil
}
