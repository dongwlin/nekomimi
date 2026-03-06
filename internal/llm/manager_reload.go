package llm

import (
	"errors"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
)

// ReloadConfig refreshes runtime LLM settings without clearing in-memory history.
func (m *Manager) ReloadConfig(cfg config.LLMConfig) error {
	if m == nil {
		return nil
	}
	providerName := normalizeProvider(cfg.Provider)
	if providerName == llmProviderGemini {
		return errors.New("gemini is not implemented")
	}
	apiURL := normalizeAPIURL(providerName, cfg.API)
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
		provider:         providerName,
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
		provider: providerName,
		model:    strings.TrimSpace(cfg.Model),
		prompt:   systemPrompt,
		apiURL:   apiURL,
	}

	if m.chatStore == nil {
		m.chatStore = chatlog.NewMemoryStore()
	}
	if m.diaryStore == nil {
		m.diaryStore = diary.NewMemoryStore()
	}
	m.contextAssembler = contextassemble.New(m.chatStore, m.diaryStore, runtimeCfg.assemblyOptions)
	m.toolRouter = buildToolRouter(m.chatStore, m.diaryStore, runtimeCfg)
	m.mu.Unlock()

	m.client.SetAPIURL(apiURL)
	m.client.SetAPIKey(cfg.Key)
	m.client.SetReasoningEffort(cfg.ReasoningEffort)
	m.client.SetThinkingType(cfg.ThinkingType)
	m.client.SetShowReasoning(cfg.ShowReasoning)
	return nil
}

func defaultRequestTimeout() time.Duration {
	return llmclient.DefaultRequestTimeout
}
