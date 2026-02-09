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
	summarySystemPrompt   = "你是对话摘要助手。请将以下对话压缩为简洁要点，保留关键信息、结论、用户偏好与待办事项。使用中文，不要加入无关内容，不超过200字。"
	lightSummaryPrompt    = "你是对话压缩助手。请在不丢关键信息与意图的前提下，将以下对话做轻量压缩为要点，保持语气自然，使用中文，字数尽量短但不过度省略。"
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
	contextMax    int
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
	contextMax := cfg.LLM.ContextMax
	if contextMax < 0 {
		contextMax = 0
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
		contextMax:    contextMax,
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
	m.compressHistoryIfNeeded(ctx, provider, model, sessionKey)
	history := m.historySnapshot(sessionKey)
	messages := append(history, llmMessage{Role: "user", Content: userInput})
	messages = m.compressMessages(ctx, provider, model, systemPrompt, messages)
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
	if strings.TrimSpace(sessionKey) == "" {
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
	if m.historyMax > 0 && len(history) > maxMessages {
		history = history[len(history)-maxMessages:]
	}
	m.history[sessionKey] = history
}

func (m *llmManager) compressHistoryIfNeeded(ctx context.Context, provider, model, sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" || m.historyMax <= 0 {
		return
	}
	m.mu.RLock()
	history, ok := m.history[sessionKey]
	if !ok || len(history) < m.historyMax*2 {
		m.mu.RUnlock()
		return
	}
	oldRounds := m.historyMax / 2
	if oldRounds < 1 {
		oldRounds = 1
	}
	oldMsgCount := oldRounds * 2
	if len(history) <= oldMsgCount {
		m.mu.RUnlock()
		return
	}
	summarySrc := make([]llmMessage, oldMsgCount)
	copy(summarySrc, history[:oldMsgCount])
	m.mu.RUnlock()

	summary := m.buildSummaryWithModel(ctx, provider, model, summarySrc)
	if strings.TrimSpace(summary) == "" {
		summary = buildSummary(summarySrc, 600)
	}
	if strings.TrimSpace(summary) == "" {
		return
	}
	summaryMsg := llmMessage{
		Role:    "system",
		Content: "对话摘要（历史压缩）: " + summary,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	history, ok = m.history[sessionKey]
	if !ok || len(history) < m.historyMax*2 || len(history) <= oldMsgCount {
		return
	}
	tail := make([]llmMessage, len(history)-oldMsgCount)
	copy(tail, history[oldMsgCount:])
	compressed := make([]llmMessage, 0, len(tail)+1)
	compressed = append(compressed, summaryMsg)
	compressed = append(compressed, tail...)
	m.history[sessionKey] = compressed
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

func (m *llmManager) compressMessages(ctx context.Context, provider, model, systemPrompt string, messages []llmMessage) []llmMessage {
	if m.contextMax <= 0 || len(messages) == 0 {
		return messages
	}
	if estimateContextTokens(systemPrompt, messages) <= m.contextMax {
		return messages
	}
	keepLast := 6
	if keepLast < 2 {
		keepLast = 2
	}
	if len(messages) <= keepLast {
		tailCount := 2
		if len(messages) < tailCount {
			tailCount = len(messages)
		}
		head := messages[:len(messages)-tailCount]
		if len(head) == 0 {
			return reduceMessagesToFit(messages, systemPrompt, m.contextMax)
		}
		summary := m.buildLightSummaryWithModel(ctx, provider, model, head)
		if strings.TrimSpace(summary) == "" {
			return reduceMessagesToFit(messages, systemPrompt, m.contextMax)
		}
		compressed := make([]llmMessage, 0, tailCount+1)
		compressed = append(compressed, llmMessage{
			Role:    "system",
			Content: "对话摘要（轻量压缩）: " + summary,
		})
		compressed = append(compressed, messages[len(messages)-tailCount:]...)
		if estimateContextTokens(systemPrompt, compressed) <= m.contextMax {
			return compressed
		}
		return reduceMessagesToFit(compressed, systemPrompt, m.contextMax)
	}
	tailStart := len(messages) - keepLast
	summarySrc := messages[:tailStart]
	tail := messages[tailStart:]
	summary := m.buildSummaryWithModel(ctx, provider, model, summarySrc)
	if strings.TrimSpace(summary) == "" {
		summary = buildSummary(summarySrc, 600)
	}
	compressed := make([]llmMessage, 0, len(tail)+1)
	if strings.TrimSpace(summary) != "" {
		compressed = append(compressed, llmMessage{
			Role:    "system",
			Content: "对话摘要（自动压缩）: " + summary,
		})
	}
	compressed = append(compressed, tail...)
	if estimateContextTokens(systemPrompt, compressed) <= m.contextMax {
		return compressed
	}
	return reduceMessagesToFit(compressed, systemPrompt, m.contextMax)
}

func (m *llmManager) buildSummaryWithModel(ctx context.Context, provider, model string, messages []llmMessage) string {
	if strings.TrimSpace(model) == "" || len(messages) == 0 {
		return ""
	}
	conversation := formatConversation(messages, m.contextMax)
	if strings.TrimSpace(conversation) == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()
	input := []llmMessage{{Role: "user", Content: conversation}}
	var summary string
	var err error
	switch provider {
	case llmProviderOpenAI:
		summary, err = m.client.GenerateOpenAI(reqCtx, model, summarySystemPrompt, input)
	case llmProviderGemini:
		return ""
	default:
		summary, err = m.client.GenerateResponses(reqCtx, model, summarySystemPrompt, input)
	}
	if err != nil {
		return ""
	}
	return strings.TrimSpace(summary)
}

func (m *llmManager) buildLightSummaryWithModel(ctx context.Context, provider, model string, messages []llmMessage) string {
	if strings.TrimSpace(model) == "" || len(messages) == 0 {
		return ""
	}
	conversation := formatConversation(messages, m.contextMax)
	if strings.TrimSpace(conversation) == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()
	input := []llmMessage{{Role: "user", Content: conversation}}
	var summary string
	var err error
	switch provider {
	case llmProviderOpenAI:
		summary, err = m.client.GenerateOpenAI(reqCtx, model, lightSummaryPrompt, input)
	case llmProviderGemini:
		return ""
	default:
		summary, err = m.client.GenerateResponses(reqCtx, model, lightSummaryPrompt, input)
	}
	if err != nil {
		return ""
	}
	return strings.TrimSpace(summary)
}

func reduceMessagesToFit(messages []llmMessage, systemPrompt string, maxTokens int) []llmMessage {
	if maxTokens <= 0 || len(messages) == 0 {
		return messages
	}
	for len(messages) > 1 && estimateContextTokens(systemPrompt, messages) > maxTokens {
		dropIndex := 0
		if isSummaryMessage(messages[0]) && len(messages) > 1 {
			dropIndex = 1
		}
		messages = append(messages[:dropIndex], messages[dropIndex+1:]...)
	}
	if estimateContextTokens(systemPrompt, messages) <= maxTokens {
		return messages
	}
	if len(messages) > 0 && isSummaryMessage(messages[0]) {
		budget := maxTokens - estimateContextTokens(systemPrompt, messages[1:])
		if budget > 4 {
			messages[0].Content = truncateToTokens(messages[0].Content, budget-4)
		}
	}
	if estimateContextTokens(systemPrompt, messages) <= maxTokens {
		return messages
	}
	for i := 0; i < len(messages) && estimateContextTokens(systemPrompt, messages) > maxTokens; i++ {
		contentTokens := estimateTokens(messages[i].Content)
		if contentTokens <= 20 {
			continue
		}
		target := contentTokens / 2
		if target < 20 {
			target = 20
		}
		messages[i].Content = truncateToTokens(messages[i].Content, target)
	}
	for len(messages) > 1 && estimateContextTokens(systemPrompt, messages) > maxTokens {
		messages = messages[1:]
	}
	return messages
}

func isSummaryMessage(msg llmMessage) bool {
	return strings.TrimSpace(msg.Role) == "system" &&
		(strings.HasPrefix(strings.TrimSpace(msg.Content), "对话摘要（自动压缩）") ||
			strings.HasPrefix(strings.TrimSpace(msg.Content), "对话摘要（轻量压缩）") ||
			strings.HasPrefix(strings.TrimSpace(msg.Content), "对话摘要（历史压缩）"))
}

func estimateContextTokens(systemPrompt string, messages []llmMessage) int {
	total := estimateTokens(systemPrompt) + 6
	for _, msg := range messages {
		total += estimateTokens(msg.Content) + 4
	}
	return total
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	ascii := 0
	nonASCII := 0
	for _, r := range text {
		if r <= 0x7f {
			ascii++
		} else {
			nonASCII++
		}
	}
	tokens := (ascii+3)/4 + nonASCII
	if tokens < 0 {
		return 0
	}
	return tokens
}

func buildSummary(messages []llmMessage, maxChars int) string {
	if len(messages) == 0 || maxChars <= 0 {
		return ""
	}
	var builder strings.Builder
	for _, msg := range messages {
		content := compactText(msg.Content, 80)
		if content == "" {
			continue
		}
		role := "用户"
		switch strings.TrimSpace(msg.Role) {
		case "assistant":
			role = "助手"
		case "system":
			role = "系统"
		}
		entry := role + ":" + content
		if builder.Len() > 0 {
			entry = "；" + entry
		}
		if builder.Len()+len([]rune(entry)) > maxChars {
			remaining := maxChars - len([]rune(builder.String()))
			if remaining > 0 {
				builder.WriteString(limitRunes(entry, remaining))
			}
			break
		}
		builder.WriteString(entry)
	}
	return builder.String()
}

func formatConversation(messages []llmMessage, contextMax int) string {
	var builder strings.Builder
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := "用户"
		switch strings.TrimSpace(msg.Role) {
		case "assistant":
			role = "助手"
		case "system":
			role = "系统"
		}
		builder.WriteString(role)
		builder.WriteString(": ")
		builder.WriteString(content)
		builder.WriteString("\n")
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return ""
	}
	if contextMax > 0 {
		limit := contextMax - 256
		if limit < 256 {
			limit = contextMax
		}
		if limit > 0 {
			result = truncateToTokens(result, limit)
		}
	}
	return result
}

func compactText(text string, maxRunes int) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.Join(strings.Fields(clean), " ")
	return limitRunes(clean, maxRunes)
}

func limitRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func truncateToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return ""
	}
	if estimateTokens(text) <= maxTokens {
		return text
	}
	target := maxTokens
	if maxTokens > 4 {
		target = maxTokens - 1
	}
	var builder strings.Builder
	ascii := 0
	nonASCII := 0
	for _, r := range text {
		if r <= 0x7f {
			ascii++
		} else {
			nonASCII++
		}
		tokens := (ascii+3)/4 + nonASCII
		if tokens > target {
			break
		}
		builder.WriteRune(r)
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return ""
	}
	return result + "..."
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
