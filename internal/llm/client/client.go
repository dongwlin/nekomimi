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
	DefaultMaxTokens            = 8192
	DefaultThinkingBudgetTokens = 1024
)

type Client struct {
	mu             sync.RWMutex
	apiKey         string
	baseURL        string
	thinkingConfig ThinkingConfig
	outputConfig   OutputConfig
	showReasoning  bool
	httpClient     anthropic.Client
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
	c.rebuildHTTPClientLocked()
}

func (c *Client) rebuildHTTPClientLocked() {
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
	c.rebuildHTTPClientLocked()
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
	c.rebuildHTTPClientLocked()
}

func normalizeThinkingType(thinkingType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(thinkingType)) {
	case "", "disabled":
		return "disabled", true
	case "enabled", "adaptive":
		return strings.ToLower(strings.TrimSpace(thinkingType)), true
	default:
		return "disabled", false
	}
}

func normalizeOutputEffort(effort string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "":
		return "", true
	case "low", "medium", "high", "max":
		return strings.ToLower(strings.TrimSpace(effort)), true
	default:
		return "", false
	}
}

func normalizeThinkingConfig(raw ThinkingConfig) ThinkingConfig {
	normalizedType, _ := normalizeThinkingType(raw.Type)
	normalized := ThinkingConfig{Type: normalizedType}
	if normalizedType != "enabled" {
		return normalized
	}

	budget := raw.BudgetTokens
	switch {
	case budget == 0:
		budget = DefaultThinkingBudgetTokens
	case budget < 0:
		budget = DefaultThinkingBudgetTokens
	case budget < DefaultThinkingBudgetTokens:
		budget = DefaultThinkingBudgetTokens
	case budget >= DefaultMaxTokens:
		budget = DefaultMaxTokens - 1
	}
	normalized.BudgetTokens = budget
	return normalized
}

func normalizeOutputConfig(raw OutputConfig) OutputConfig {
	effort, _ := normalizeOutputEffort(raw.Effort)
	return OutputConfig{Effort: effort}
}

func logThinkingConfigIssues(scope string, raw ThinkingConfig, normalized ThinkingConfig) {
	trimmedType := strings.TrimSpace(raw.Type)
	if _, ok := normalizeThinkingType(raw.Type); !ok {
		log.Warn().
			Str("scope", scope).
			Str("thinking_type", trimmedType).
			Msg("invalid thinking.type, thinking disabled")
		return
	}
	if normalized.Type != "enabled" {
		return
	}
	switch {
	case raw.BudgetTokens < 0:
		log.Warn().
			Str("scope", scope).
			Int64("budget_tokens", raw.BudgetTokens).
			Int64("effective_budget_tokens", normalized.BudgetTokens).
			Msg("invalid thinking.budget_tokens, using default budget")
	case raw.BudgetTokens > 0 && raw.BudgetTokens < DefaultThinkingBudgetTokens:
		log.Warn().
			Str("scope", scope).
			Int64("budget_tokens", raw.BudgetTokens).
			Int64("effective_budget_tokens", normalized.BudgetTokens).
			Msg("thinking.budget_tokens below minimum, clamped")
	case raw.BudgetTokens >= DefaultMaxTokens:
		log.Warn().
			Str("scope", scope).
			Int64("budget_tokens", raw.BudgetTokens).
			Int64("effective_budget_tokens", normalized.BudgetTokens).
			Msg("thinking.budget_tokens exceeds max_tokens, clamped")
	}
}

func logOutputConfigIssues(scope string, raw OutputConfig, normalized OutputConfig) {
	trimmedEffort := strings.TrimSpace(raw.Effort)
	if trimmedEffort == "" {
		return
	}
	if _, ok := normalizeOutputEffort(raw.Effort); ok {
		return
	}
	log.Warn().
		Str("scope", scope).
		Str("effort", trimmedEffort).
		Str("effective_effort", normalized.Effort).
		Msg("invalid output_config.effort, effort disabled")
}

func (c *Client) SetThinkingConfig(cfg ThinkingConfig) {
	normalized := normalizeThinkingConfig(cfg)
	c.mu.Lock()
	c.thinkingConfig = normalized
	c.mu.Unlock()
	logThinkingConfigIssues("client_config", cfg, normalized)
}

func (c *Client) SetOutputConfig(cfg OutputConfig) {
	normalized := normalizeOutputConfig(cfg)
	c.mu.Lock()
	c.outputConfig = normalized
	c.mu.Unlock()
	logOutputConfigIssues("client_config", cfg, normalized)
}

func (c *Client) thinkingConfigSnapshot() ThinkingConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return normalizeThinkingConfig(c.thinkingConfig)
}

func (c *Client) outputConfigSnapshot() OutputConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return normalizeOutputConfig(c.outputConfig)
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

func (c *Client) resolveRequestConfig(requestOptions RequestOptions) (ThinkingConfig, OutputConfig) {
	thinkingConfig := c.thinkingConfigSnapshot()
	outputConfig := c.outputConfigSnapshot()

	if requestOptions.Thinking != nil {
		raw := *requestOptions.Thinking
		normalized := normalizeThinkingConfig(raw)
		logThinkingConfigIssues("request_override", raw, normalized)
		thinkingConfig = normalized
	}
	if requestOptions.OutputConfig != nil {
		raw := *requestOptions.OutputConfig
		normalized := normalizeOutputConfig(raw)
		logOutputConfigIssues("request_override", raw, normalized)
		outputConfig = normalized
	}
	if thinkingConfig.Type != "adaptive" {
		outputConfig = OutputConfig{}
	}
	return thinkingConfig, outputConfig
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
	thinkingConfig, outputConfig := c.resolveRequestConfig(requestOptions)

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
		Bool("thinking_enabled", thinkingConfig.Type != "disabled").
		Str("thinking_type", thinkingConfig.Type).
		Int64("thinking_budget_tokens", thinkingConfig.BudgetTokens).
		Str("output_effort", outputConfig.Effort).
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Msg("sending llm request")

	params := buildMessageParams(modelName, inputMessages, systemPrompt, thinkingConfig, outputConfig)
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
	thinkingConfig, outputConfig := c.resolveRequestConfig(requestOptions)

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
		Bool("thinking_enabled", thinkingConfig.Type != "disabled").
		Str("thinking_type", thinkingConfig.Type).
		Int64("thinking_budget_tokens", thinkingConfig.BudgetTokens).
		Str("output_effort", outputConfig.Effort).
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Msg("sending llm streaming request")

	params := buildMessageParams(modelName, inputMessages, systemPrompt, thinkingConfig, outputConfig)
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
	systemPrompt string,
	thinkingConfig ThinkingConfig,
	outputConfig OutputConfig,
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

	switch thinkingConfig.Type {
	case "enabled":
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfEnabled: &anthropic.ThinkingConfigEnabledParam{
				BudgetTokens: thinkingConfig.BudgetTokens,
			},
		}
	case "adaptive":
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		}
		if outputConfig.Effort != "" {
			params.OutputConfig = anthropic.OutputConfigParam{
				Effort: anthropic.OutputConfigEffort(outputConfig.Effort),
			}
		}
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
