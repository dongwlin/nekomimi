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
)

type Client struct {
	mu         sync.RWMutex
	apiURL     string
	apiKey     string
	httpClient *http.Client
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

func parseResponsesText(parsed responsesResponse) (string, error) {
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

func parseChatCompletionText(parsed chatCompletionsResponse) (string, error) {
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

func (c *Client) GenerateResponses(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error) {
	if err := c.ensureAPIKey(); err != nil {
		return "", err
	}
	input := buildResponsesInput(systemPrompt, messages)
	reqBody := responsesRequest{
		Model: modelName,
		Input: input,
	}
	body, err := c.postJSON(ctx, c.apiURLSnapshot(), reqBody)
	if err != nil {
		return "", err
	}

	var parsed responsesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	return parseResponsesText(parsed)
}

func (c *Client) GenerateOpenAI(ctx context.Context, modelName, systemPrompt string, messages []model.Message) (string, error) {
	if err := c.ensureAPIKey(); err != nil {
		return "", err
	}
	chatMessages := buildChatMessages(systemPrompt, messages)
	reqBody := chatCompletionsRequest{
		Model:    modelName,
		Messages: chatMessages,
	}
	body, err := c.postJSON(ctx, c.apiURLSnapshot(), reqBody)
	if err != nil {
		return "", err
	}

	var parsed chatCompletionsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	return parseChatCompletionText(parsed)
}
