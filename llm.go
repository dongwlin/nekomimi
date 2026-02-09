package main

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
	"time"
)

const (
	defaultLLMAPI         = "https://api.openai.com/v1/responses"
	defaultOpenAIAPI      = "https://api.openai.com/v1/chat/completions"
	defaultSystemPrompt   = "你是一个可爱的猫娘，说话亲切可爱，回答时适当使用猫娘语气词。"
	defaultRequestTimeout = 30 * time.Second
)

const (
	llmProviderResponses = "responses"
	llmProviderOpenAI    = "openai"
	llmProviderGemini    = "gemini"
)

type llmClient struct {
	apiURL     string
	apiKey     string
	httpClient *http.Client
}

type llmMessage struct {
	Role    string
	Content string
}

func newLLMClient(apiURL, apiKey string) *llmClient {
	if strings.TrimSpace(apiURL) == "" {
		apiURL = defaultLLMAPI
	}
	return &llmClient{
		apiURL: apiURL,
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: defaultRequestTimeout,
		},
	}
}

type llmManager struct {
	mu            sync.RWMutex
	enabled       bool
	provider      string
	model         string
	systemPrompt  string
	defaultModel  string
	defaultPrompt string
	defaultAPI    string
	defaultProv   string
	client        *llmClient
	historyMax    int
	history       map[string][]llmMessage
	immersive     map[string]bool
}

func newLLMManager(cfg *appConfig) *llmManager {
	systemPrompt := strings.TrimSpace(cfg.LLM.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	provider := normalizeProvider(cfg.LLM.Provider)
	apiURL := normalizeAPIURL(provider, cfg.LLM.API)
	historyMax := cfg.LLM.HistoryMax
	if historyMax <= 0 {
		historyMax = 10
	}
	return &llmManager{
		enabled:       cfg.LLM.Enabled,
		provider:      provider,
		model:         strings.TrimSpace(cfg.LLM.Model),
		systemPrompt:  systemPrompt,
		defaultModel:  strings.TrimSpace(cfg.LLM.Model),
		defaultPrompt: systemPrompt,
		defaultAPI:    apiURL,
		defaultProv:   provider,
		client:        newLLMClient(apiURL, cfg.LLM.Key),
		historyMax:    historyMax,
		history:       make(map[string][]llmMessage),
	}
}

func (m *llmManager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

func (m *llmManager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = enabled
}

func (m *llmManager) SetProvider(provider string) error {
	normalized := normalizeProvider(provider)
	if normalized == llmProviderGemini {
		return errors.New("gemini 尚未接入")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = normalized
	m.client.apiURL = normalizeAPIURL(normalized, m.client.apiURL)
	return nil
}

func (m *llmManager) SetModel(model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = strings.TrimSpace(model)
}

func (m *llmManager) SetSystemPrompt(prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemPrompt = strings.TrimSpace(prompt)
}

func (m *llmManager) SetImmersive(sessionKey string, enabled bool) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.immersive == nil {
		m.immersive = make(map[string]bool)
	}
	if !enabled {
		delete(m.immersive, sessionKey)
		return
	}
	m.immersive[sessionKey] = true
}

func (m *llmManager) IsImmersive(sessionKey string) bool {
	if strings.TrimSpace(sessionKey) == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.immersive == nil {
		return false
	}
	return m.immersive[sessionKey]
}

func (m *llmManager) ResetDefaults() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = m.defaultProv
	m.model = m.defaultModel
	m.systemPrompt = m.defaultPrompt
	m.client.apiURL = m.defaultAPI
}

func (m *llmManager) Status() (enabled bool, provider string, model string, systemPrompt string, apiURL string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled, m.provider, m.model, m.systemPrompt, m.client.apiURL
}

func (m *llmManager) Reply(ctx context.Context, userInput, sessionKey string) (string, error) {
	m.mu.RLock()
	provider := m.provider
	model := m.model
	systemPrompt := m.systemPrompt
	m.mu.RUnlock()
	if strings.TrimSpace(model) == "" {
		return "", errors.New("未配置模型名")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	history := m.historySnapshot(sessionKey)
	messages := append(history, llmMessage{Role: "user", Content: userInput})
	reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()
	var reply string
	var err error
	switch provider {
	case llmProviderOpenAI:
		reply, err = m.client.GenerateOpenAI(reqCtx, model, systemPrompt, messages)
	case llmProviderGemini:
		return "", errors.New("gemini 尚未接入")
	default:
		reply, err = m.client.GenerateResponses(reqCtx, model, systemPrompt, messages)
	}
	if err != nil {
		return "", err
	}
	m.appendHistory(sessionKey, userInput, reply)
	return reply, nil
}

func (m *llmManager) historySnapshot(sessionKey string) []llmMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	history, ok := m.history[sessionKey]
	if !ok || len(history) == 0 {
		return nil
	}
	copied := make([]llmMessage, len(history))
	copy(copied, history)
	return copied
}

func (m *llmManager) appendHistory(sessionKey, userInput, assistantReply string) {
	if strings.TrimSpace(sessionKey) == "" || m.historyMax <= 0 {
		return
	}
	userInput = strings.TrimSpace(userInput)
	assistantReply = strings.TrimSpace(assistantReply)
	if userInput == "" || assistantReply == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.history == nil {
		m.history = make(map[string][]llmMessage)
	}
	history := m.history[sessionKey]
	history = append(history, llmMessage{Role: "user", Content: userInput})
	history = append(history, llmMessage{Role: "assistant", Content: assistantReply})
	maxMessages := m.historyMax * 2
	if maxMessages > 0 && len(history) > maxMessages {
		history = history[len(history)-maxMessages:]
	}
	m.history[sessionKey] = history
}

func (m *llmManager) ClearHistory(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.history) == 0 {
		return
	}
	delete(m.history, sessionKey)
}

type responsesRequest struct {
	Model string                  `json:"model"`
	Input []responsesInputMessage `json:"input"`
}

type responsesInputMessage struct {
	Role    string             `json:"role"`
	Content []responsesContent `json:"content"`
}

type responsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesResponse struct {
	Output []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *llmClient) GenerateResponses(ctx context.Context, model, systemPrompt string, messages []llmMessage) (string, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return "", errors.New("未配置 API Key")
	}
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
	reqBody := responsesRequest{
		Model: model,
		Input: input,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("请求失败: %s", strings.TrimSpace(string(body)))
	}

	var parsed responsesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", errors.New(parsed.Error.Message)
	}

	var builder strings.Builder
	for _, item := range parsed.Output {
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				builder.WriteString(content.Text)
			}
		}
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return "", errors.New("模型未返回文本内容")
	}
	return result, nil
}

type chatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *llmClient) GenerateOpenAI(ctx context.Context, model, systemPrompt string, messages []llmMessage) (string, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return "", errors.New("未配置 API Key")
	}
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
	reqBody := chatCompletionsRequest{
		Model: model,
		Messages: chatMessages,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("请求失败: %s", strings.TrimSpace(string(body)))
	}

	var parsed chatCompletionsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("模型未返回文本内容")
	}
	result := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if result == "" {
		return "", errors.New("模型未返回文本内容")
	}
	return result, nil
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case llmProviderOpenAI:
		return llmProviderOpenAI
	case llmProviderGemini:
		return llmProviderGemini
	default:
		return llmProviderResponses
	}
}

func normalizeAPIURL(provider, apiURL string) string {
	trimmed := strings.TrimSpace(apiURL)
	if trimmed == "" {
		if provider == llmProviderOpenAI {
			return defaultOpenAIAPI
		}
		return defaultLLMAPI
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "/chat/completions") {
		if provider == llmProviderOpenAI {
			return trimmed
		}
		return strings.Replace(trimmed, "/chat/completions", "/responses", 1)
	}
	if strings.Contains(lower, "/responses") {
		if provider != llmProviderOpenAI {
			return trimmed
		}
		return strings.Replace(trimmed, "/responses", "/chat/completions", 1)
	}
	trimmed = strings.TrimRight(trimmed, "/")
	if provider == llmProviderOpenAI {
		return trimmed + "/chat/completions"
	}
	return trimmed + "/responses"
}
