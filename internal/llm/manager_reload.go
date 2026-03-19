package llm

import (
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/ctxasm"
	"github.com/dongwlin/nekomimi/internal/diary"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
)

// ReloadConfig refreshes runtime LLM settings without clearing in-memory history.
func (m *Manager) ReloadConfig(cfg config.LLMConfig) error {
	if m == nil {
		return nil
	}
	apiURL := normalizeAPIURL(cfg.API)
	requestTimeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout()
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

func defaultRequestTimeout() time.Duration {
	return llmclient.DefaultRequestTimeout
}
