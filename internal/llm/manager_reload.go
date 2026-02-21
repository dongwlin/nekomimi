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
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
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
	basePrompt := composeSystemPrompt(llmprompt.DefaultSystemPrompt, llmprompt.SpeakerSystemPrompt)
	systemPrompt := composeSystemPrompt(basePrompt, cfg.SystemPrompt)
	contextMax := cfg.ContextMax
	if contextMax < 0 {
		contextMax = 0
	}
	runtimeCfg := normalizeRuntimeConfig(cfg, requestTimeout, contextMax)

	m.mu.Lock()
	m.enabled = cfg.Enabled
	m.provider = providerName
	m.model = strings.TrimSpace(cfg.Model)
	m.requestTimeout = requestTimeout
	m.systemPrompt = systemPrompt
	m.basePrompt = basePrompt
	m.defaultModel = strings.TrimSpace(cfg.Model)
	m.defaultPrompt = systemPrompt
	m.defaultAPI = apiURL
	m.defaultProv = providerName
	m.contextMax = contextMax
	m.recentChatLimit = runtimeCfg.recentChatLimit
	m.recentDiaryLimit = runtimeCfg.recentDiaryLimit
	m.toolsEnabled = runtimeCfg.toolsEnabled
	m.toolLoopMaxSteps = runtimeCfg.toolLoopMaxSteps
	m.toolLoopTimeout = runtimeCfg.toolLoopTimeout

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
