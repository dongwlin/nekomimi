package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/rs/zerolog/log"
)

type Client struct {
	mu              sync.RWMutex
	apiURL          string
	apiKey          string
	reasoningEffort string
	thinkingType    string
	showReasoning   bool
	httpClient      *http.Client
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
		apiURL = DefaultLLMAPI
	}
	return &Client{
		apiURL: apiURL,
		apiKey: apiKey,
		httpClient: &http.Client{
			// Keep client timeout disabled and rely on per-request context deadlines.
			// This avoids accidentally overriding configured request timeouts (e.g. llm.timeout_ms).
			Timeout: 0,
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

func (c *Client) SetAPIKey(apiKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.apiKey = strings.TrimSpace(apiKey)
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
		return nil, sanitizeRequestError(err)
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

func (c *Client) postSSE(ctx context.Context, apiURL string, reqBody any, onData func(data string) error) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return sanitizeRequestError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return readErr
		}
		return fmt.Errorf("请求失败: %s", strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	dataLines := make([]string, 0, 4)
	emit := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if onData == nil {
			return nil
		}
		return onData(payload)
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := emit(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return emit()
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
	input := buildResponsesInput(systemPrompt, messages)
	reqBody := responsesRequest{
		Model: modelName,
		Input: input,
	}
	if reasoningEffort != "" {
		reqBody.Reasoning = &responsesReasoning{Effort: reasoningEffort}
	}
	if thinkingType != "" {
		reqBody.Thinking = &thinkingConfig{Type: thinkingType}
	}
	apiURL := c.apiURLSnapshot()
	log.Info().
		Str("llm_api", "responses").
		Str("request_source", requestSource).
		Bool("api_url_configured", strings.TrimSpace(apiURL) != "").
		Str("model", strings.TrimSpace(modelName)).
		Int("message_count", len(input)).
		Bool("has_system_prompt", strings.TrimSpace(systemPrompt) != "").
		Bool("reasoning_enabled", reqBody.Reasoning != nil).
		Bool("thinking_enabled", reqBody.Thinking != nil).
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Str("reasoning_effort", logReasoningEffort).
		Str("thinking_type", thinkingType).
		Msg("sending llm request")
	body, err := c.postJSON(ctx, apiURL, reqBody)
	if err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "responses").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm request failed")
		return "", err
	}

	var parsed responsesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "responses").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm response parse failed")
		return "", err
	}
	reply, reasoning, err := parseResponsesText(parsed)
	if err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "responses").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm response extraction failed")
		return "", err
	}
	log.Info().
		Str("llm_api", "responses").
		Str("request_source", requestSource).
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
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

func (c *Client) GenerateResponsesStream(ctx context.Context, modelName, systemPrompt string, messages []model.Message, onDelta func(delta string) error) (string, error) {
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
	input := buildResponsesInput(systemPrompt, messages)
	reqBody := responsesRequest{
		Model:  modelName,
		Input:  input,
		Stream: true,
	}
	if reasoningEffort != "" {
		reqBody.Reasoning = &responsesReasoning{Effort: reasoningEffort}
	}
	if thinkingType != "" {
		reqBody.Thinking = &thinkingConfig{Type: thinkingType}
	}
	apiURL := c.apiURLSnapshot()
	log.Info().
		Str("llm_api", "responses_stream").
		Str("request_source", requestSource).
		Bool("api_url_configured", strings.TrimSpace(apiURL) != "").
		Str("model", strings.TrimSpace(modelName)).
		Int("message_count", len(input)).
		Bool("has_system_prompt", strings.TrimSpace(systemPrompt) != "").
		Bool("reasoning_enabled", reqBody.Reasoning != nil).
		Bool("thinking_enabled", reqBody.Thinking != nil).
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Str("reasoning_effort", logReasoningEffort).
		Str("thinking_type", thinkingType).
		Msg("sending llm streaming request")

	var replyBuilder strings.Builder
	err := c.postSSE(ctx, apiURL, reqBody, func(data string) error {
		if strings.EqualFold(strings.TrimSpace(data), "[DONE]") {
			return nil
		}
		delta, done, eventErr := parseResponsesStreamEvent(data)
		if eventErr != nil {
			return eventErr
		}
		if done || delta == "" {
			return nil
		}
		replyBuilder.WriteString(delta)
		if onDelta != nil {
			return onDelta(delta)
		}
		return nil
	})
	if err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "responses_stream").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm streaming request failed")
		return "", err
	}
	reply := strings.TrimSpace(replyBuilder.String())
	if reply == "" {
		log.Warn().
			Str("llm_api", "responses_stream").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm streaming response empty")
		return "", errors.New("模型未返回文本内容")
	}
	log.Info().
		Str("llm_api", "responses_stream").
		Str("request_source", requestSource).
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Int("reply_chars", len([]rune(reply))).
		Msg("llm streaming response received")
	return reply, nil
}

func (c *Client) GenerateOpenAI(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error) {
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
	chatMessages := buildChatMessages(systemPrompt, messages)
	reqBody := chatCompletionsRequest{
		Model:           modelName,
		Messages:        chatMessages,
		ReasoningEffort: reasoningEffort,
	}
	if thinkingType != "" {
		reqBody.Thinking = &thinkingConfig{Type: thinkingType}
	}
	apiURL := c.apiURLSnapshot()
	log.Info().
		Str("llm_api", "chat_completions").
		Str("request_source", requestSource).
		Bool("api_url_configured", strings.TrimSpace(apiURL) != "").
		Str("model", strings.TrimSpace(modelName)).
		Int("message_count", len(chatMessages)).
		Bool("has_system_prompt", strings.TrimSpace(systemPrompt) != "").
		Bool("reasoning_enabled", strings.TrimSpace(reqBody.ReasoningEffort) != "").
		Bool("thinking_enabled", reqBody.Thinking != nil).
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Str("reasoning_effort", logReasoningEffort).
		Str("thinking_type", thinkingType).
		Msg("sending llm request")
	body, err := c.postJSON(ctx, apiURL, reqBody)
	if err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "chat_completions").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm request failed")
		return "", err
	}

	var parsed chatCompletionsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "chat_completions").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm response parse failed")
		return "", err
	}
	reply, reasoning, err := parseChatCompletionText(parsed)
	if err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "chat_completions").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm response extraction failed")
		return "", err
	}
	log.Info().
		Str("llm_api", "chat_completions").
		Str("request_source", requestSource).
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
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

func (c *Client) GenerateOpenAIStream(ctx context.Context, modelName, systemPrompt string, messages []model.Message, onDelta func(delta string) error) (string, error) {
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
	chatMessages := buildChatMessages(systemPrompt, messages)
	reqBody := chatCompletionsRequest{
		Model:           modelName,
		Messages:        chatMessages,
		ReasoningEffort: reasoningEffort,
		Stream:          true,
	}
	if thinkingType != "" {
		reqBody.Thinking = &thinkingConfig{Type: thinkingType}
	}
	apiURL := c.apiURLSnapshot()
	log.Info().
		Str("llm_api", "chat_completions_stream").
		Str("request_source", requestSource).
		Bool("api_url_configured", strings.TrimSpace(apiURL) != "").
		Str("model", strings.TrimSpace(modelName)).
		Int("message_count", len(chatMessages)).
		Bool("has_system_prompt", strings.TrimSpace(systemPrompt) != "").
		Bool("reasoning_enabled", strings.TrimSpace(reqBody.ReasoningEffort) != "").
		Bool("thinking_enabled", reqBody.Thinking != nil).
		Bool("show_reasoning", c.showReasoningSnapshot()).
		Str("reasoning_effort", logReasoningEffort).
		Str("thinking_type", thinkingType).
		Msg("sending llm streaming request")

	var replyBuilder strings.Builder
	err := c.postSSE(ctx, apiURL, reqBody, func(data string) error {
		if strings.EqualFold(strings.TrimSpace(data), "[DONE]") {
			return nil
		}
		deltas, eventErr := parseChatCompletionsStreamEvent(data)
		if eventErr != nil {
			return eventErr
		}
		for _, delta := range deltas {
			replyBuilder.WriteString(delta)
			if onDelta != nil {
				if err := onDelta(delta); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Warn().
			Err(err).
			Str("llm_api", "chat_completions_stream").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm streaming request failed")
		return "", err
	}
	reply := strings.TrimSpace(replyBuilder.String())
	if reply == "" {
		log.Warn().
			Str("llm_api", "chat_completions_stream").
			Str("request_source", requestSource).
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm streaming response empty")
		return "", errors.New("模型未返回文本内容")
	}
	log.Info().
		Str("llm_api", "chat_completions_stream").
		Str("request_source", requestSource).
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Int("reply_chars", len([]rune(reply))).
		Msg("llm streaming response received")
	return reply, nil
}

func parseChatCompletionsStreamEvent(data string) ([]string, error) {
	var parsed chatCompletionsStreamResponse
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return nil, nil
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return nil, errors.New(parsed.Error.Message)
	}
	deltas := make([]string, 0, len(parsed.Choices))
	for _, choice := range parsed.Choices {
		delta := choice.Delta.Content
		if delta == "" {
			continue
		}
		deltas = append(deltas, delta)
	}
	return deltas, nil
}

func parseResponsesStreamEvent(data string) (delta string, done bool, err error) {
	var payload map[string]any
	if unmarshalErr := json.Unmarshal([]byte(data), &payload); unmarshalErr != nil {
		return "", false, nil
	}
	eventType := strings.TrimSpace(toString(payload["type"]))
	if eventType == "" {
		return "", false, nil
	}
	if strings.Contains(eventType, "error") {
		if errMsg := responsesErrorMessage(payload); errMsg != "" {
			return "", false, errors.New(errMsg)
		}
		return "", false, errors.New("流式请求返回错误事件")
	}
	if strings.Contains(eventType, "completed") {
		return "", true, nil
	}
	rawDelta := toString(payload["delta"])
	return rawDelta, false, nil
}

func responsesErrorMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	errVal, ok := payload["error"]
	if !ok {
		return ""
	}
	switch v := errVal.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		return strings.TrimSpace(toString(v["message"]))
	default:
		return ""
	}
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}
