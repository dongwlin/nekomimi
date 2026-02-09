package llm

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/dongwlin/nekomimi/internal/config"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
)

type Manager struct {
	mu            sync.RWMutex
	enabled       bool
	provider      string
	model         string
	systemPrompt  string
	basePrompt    string
	defaultModel  string
	defaultPrompt string
	defaultAPI    string
	defaultProv   string
	client        *llmclient.Client
	historyMax    int
	contextMax    int
	history       map[string][]Message
	immersive     map[string]bool
}

func NewManager(cfg config.LLMConfig) *Manager {
	basePrompt := composeSystemPrompt(llmprompt.DefaultSystemPrompt, llmprompt.SpeakerSystemPrompt)
	systemPrompt := composeSystemPrompt(basePrompt, cfg.SystemPrompt)
	provider := normalizeProvider(cfg.Provider)
	apiURL := normalizeAPIURL(provider, cfg.API)
	historyMax := cfg.HistoryMax
	contextMax := cfg.ContextMax
	if contextMax < 0 {
		contextMax = 0
	}
	return &Manager{
		enabled:       cfg.Enabled,
		provider:      provider,
		model:         strings.TrimSpace(cfg.Model),
		systemPrompt:  systemPrompt,
		basePrompt:    basePrompt,
		defaultModel:  strings.TrimSpace(cfg.Model),
		defaultPrompt: systemPrompt,
		defaultAPI:    apiURL,
		defaultProv:   provider,
		client:        llmclient.New(apiURL, cfg.Key),
		historyMax:    historyMax,
		contextMax:    contextMax,
		history:       make(map[string][]Message),
	}
}

func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

func (m *Manager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = enabled
}

func (m *Manager) SetProvider(provider string) error {
	normalized := normalizeProvider(provider)
	if normalized == llmProviderGemini {
		return errors.New("gemini 尚未接入")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = normalized
	m.client.SetAPIURL(normalizeAPIURL(normalized, m.client.APIURL()))
	return nil
}

func (m *Manager) SetModel(model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = strings.TrimSpace(model)
}

func (m *Manager) SetSystemPrompt(prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemPrompt = composeSystemPrompt(m.basePrompt, prompt)
}

func (m *Manager) SetImmersive(sessionKey string, enabled bool) {
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

func (m *Manager) IsImmersive(sessionKey string) bool {
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

func (m *Manager) ResetDefaults() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = m.defaultProv
	m.model = m.defaultModel
	m.systemPrompt = m.defaultPrompt
	m.client.SetAPIURL(m.defaultAPI)
}

func (m *Manager) Status() (enabled bool, provider string, model string, systemPrompt string, apiURL string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled, m.provider, m.model, m.systemPrompt, m.client.APIURL()
}

func (m *Manager) Reply(ctx context.Context, userInput, sessionKey, speaker string) (string, error) {
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
	userContent := formatUserContent(userInput, speaker)
	messages := append(history, Message{Role: "user", Content: userContent})
	messages = m.compressMessages(ctx, provider, model, systemPrompt, messages)
	reqCtx, cancel := context.WithTimeout(ctx, llmclient.DefaultRequestTimeout)
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
	m.appendHistory(sessionKey, userContent, reply)
	return reply, nil
}
