package llm

import (
	"errors"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/history"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
)

// ReloadConfig refreshes runtime LLM settings without clearing in-memory history.
func (m *Manager) ReloadConfig(cfg config.LLMConfig) error {
	if m == nil {
		return nil
	}
	providerName := normalizeProvider(cfg.Provider)
	if providerName == llmProviderGemini {
		return errors.New("gemini 尚未接入")
	}
	apiURL := normalizeAPIURL(providerName, cfg.API)
	requestTimeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout()
	}
	basePrompt := composeSystemPrompt(llmprompt.DefaultSystemPrompt, llmprompt.SpeakerSystemPrompt)
	systemPrompt := composeSystemPrompt(basePrompt, cfg.SystemPrompt)
	historyMax := cfg.HistoryMax
	contextMax := cfg.ContextMax
	if contextMax < 0 {
		contextMax = 0
	}
	judgePrompt := strings.TrimSpace(cfg.Immersive.MentionJudge.Prompt)
	if judgePrompt == "" {
		judgePrompt = llmprompt.MentionJudgePrompt
	}
	judgeModel := strings.TrimSpace(cfg.Immersive.MentionJudge.Model)
	judgeReasoning := normalizeAssistantReasoningEffort(cfg.Immersive.MentionJudge.ReasoningEffort)
	judgeThinking := normalizeAssistantThinkingType(cfg.Immersive.MentionJudge.ThinkingType)
	judgeTimeout := time.Duration(cfg.Immersive.MentionJudge.TimeoutMS) * time.Millisecond
	if judgeTimeout <= 0 {
		judgeTimeout = 1200 * time.Millisecond
	}
	speakJudgePrompt := strings.TrimSpace(cfg.Immersive.SpeakGate.Prompt)
	if speakJudgePrompt == "" {
		speakJudgePrompt = llmprompt.SpeakGateJudgePrompt
	}
	speakJudgeModel := strings.TrimSpace(cfg.Immersive.SpeakGate.Model)
	speakJudgeReasoning := normalizeAssistantReasoningEffort(cfg.Immersive.SpeakGate.ReasoningEffort)
	speakJudgeThinking := normalizeAssistantThinkingType(cfg.Immersive.SpeakGate.ThinkingType)
	speakJudgeTimeout := time.Duration(cfg.Immersive.SpeakGate.TimeoutMS) * time.Millisecond
	if speakJudgeTimeout <= 0 {
		speakJudgeTimeout = 1200 * time.Millisecond
	}

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
	m.historyMax = historyMax
	m.contextMax = contextMax
	m.judgeEnabled = cfg.Immersive.MentionJudge.Enabled
	m.judgeModel = judgeModel
	m.judgePrompt = judgePrompt
	m.judgeTimeout = judgeTimeout
	m.judgeReasoning = judgeReasoning
	m.judgeThinking = judgeThinking
	m.speakJudgeEnabled = cfg.Immersive.SpeakGate.Enabled
	m.speakJudgeModel = speakJudgeModel
	m.speakJudgePrompt = speakJudgePrompt
	m.speakJudgeTimeout = speakJudgeTimeout
	m.speakJudgeReasoning = speakJudgeReasoning
	m.speakJudgeThinking = speakJudgeThinking
	m.mu.Unlock()

	m.client.SetAPIURL(apiURL)
	m.client.SetAPIKey(cfg.Key)
	m.client.SetReasoningEffort(cfg.ReasoningEffort)
	m.client.SetThinkingType(cfg.ThinkingType)
	m.client.SetShowReasoning(cfg.ShowReasoning)
	if store, ok := m.historyStore.(*history.MemoryStore); ok {
		store.SetMaxRounds(historyMax)
	}
	return nil
}

func defaultRequestTimeout() time.Duration {
	return llmclient.DefaultRequestTimeout
}
