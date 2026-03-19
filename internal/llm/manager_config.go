package llm

import (
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/ctxasm"
	"github.com/dongwlin/nekomimi/internal/diary"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
)

func composeConfiguredSystemPrompt(configPrompt string) (basePrompt string, systemPrompt string) {
	basePrompt = strings.TrimSpace(llmprompt.SpeakerSystemPrompt)
	customPrompt := strings.TrimSpace(configPrompt)
	if customPrompt == "" {
		return basePrompt, composeSystemPrompt(basePrompt, llmprompt.DefaultSystemPrompt)
	}
	return basePrompt, composeSystemPrompt(basePrompt, customPrompt)
}

func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.enabled
}

func (m *Manager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current.enabled = enabled
}

func (m *Manager) SetModel(model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current.model = strings.TrimSpace(model)
}

func (m *Manager) SetSystemPrompt(prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current.systemPrompt = composeSystemPrompt(m.current.basePrompt, prompt)
}

func (m *Manager) SetAssistantSpeaker(speaker string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	trimmed := strings.TrimSpace(speaker)
	if trimmed == "" {
		trimmed = "name=assistant"
	}
	m.current.assistantSpeaker = trimmed
}

func (m *Manager) SetImmersive(sessionKey string, enabled bool) {
	m.sessions.SetImmersive(sessionKey, enabled)
}

func (m *Manager) IsImmersive(sessionKey string) bool {
	return m.sessions.IsImmersive(sessionKey)
}

func (m *Manager) ResetDefaults() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current.model = m.defaults.model
	m.current.systemPrompt = m.defaults.prompt
	m.client.SetAPIURL(m.defaults.apiURL)
}

func (m *Manager) Status() (enabled bool, model string, systemPrompt string, apiURL string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.enabled, m.current.model, m.current.systemPrompt, m.client.APIURL()
}

// ReloadConfig refreshes runtime LLM settings without clearing in-memory history.
func (m *Manager) ReloadConfig(cfg config.LLMConfig) error {
	if m == nil {
		return nil
	}
	apiURL := strings.TrimSpace(cfg.API)
	if apiURL == "" {
		apiURL = llmclient.DefaultAnthropicAPI
	}
	requestTimeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if requestTimeout <= 0 {
		requestTimeout = llmclient.DefaultRequestTimeout
	}
	basePrompt, systemPrompt := composeConfiguredSystemPrompt(cfg.SystemPrompt)
	contextMax := cfg.ContextMax
	if contextMax < 0 {
		contextMax = 0
	}
	runtimeCfg := normalizeRuntimeConfig(cfg, requestTimeout, contextMax)

	m.mu.Lock()
	m.current = currentConfig{
		enabled:          cfg.Enabled,
		model:            strings.TrimSpace(cfg.Model),
		requestTimeout:   requestTimeout,
		systemPrompt:     systemPrompt,
		basePrompt:       basePrompt,
		assistantSpeaker: m.current.assistantSpeaker,
		contextMax:       contextMax,
		recentChatLimit:  runtimeCfg.recentChatLimit,
		recentDiaryLimit: runtimeCfg.recentDiaryLimit,
		toolsEnabled:     runtimeCfg.toolsEnabled,
		toolLoopMaxSteps: runtimeCfg.toolLoopMaxSteps,
		toolLoopTimeout:  runtimeCfg.toolLoopTimeout,
	}
	m.defaults = defaultConfig{
		model:  strings.TrimSpace(cfg.Model),
		prompt: systemPrompt,
		apiURL: apiURL,
	}

	if m.chatStore == nil {
		m.chatStore = chatlog.NewMemoryStore()
	}
	if m.diaryStore == nil {
		m.diaryStore = diary.NewMemoryStore()
	}
	m.contextAssembler = ctxasm.New(m.chatStore, m.diaryStore, runtimeCfg.assemblyOptions)
	m.toolRouter = buildToolRouter(m.chatStore, m.diaryStore, runtimeCfg)
	m.mu.Unlock()

	m.client.SetAPIURL(apiURL)
	m.client.SetAPIKey(cfg.Key)
	m.client.SetThinkingConfig(thinkingConfigFromConfig(cfg))
	m.client.SetOutputConfig(outputConfigFromConfig(cfg))
	m.client.SetShowReasoning(cfg.ShowReasoning)
	return nil
}

func thinkingConfigFromConfig(cfg config.LLMConfig) llmclient.ThinkingConfig {
	return llmclient.ThinkingConfig{
		Type:         cfg.Thinking.Type,
		BudgetTokens: int64(cfg.Thinking.BudgetTokens),
	}
}

func outputConfigFromConfig(cfg config.LLMConfig) llmclient.OutputConfig {
	return llmclient.OutputConfig{
		Effort: cfg.OutputConfig.Effort,
	}
}
