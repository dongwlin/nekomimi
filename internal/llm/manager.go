package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/history"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
	"github.com/dongwlin/nekomimi/internal/llm/provider"
)

type Manager struct {
	mu                  sync.RWMutex
	enabled             bool
	provider            string
	model               string
	requestTimeout      time.Duration
	systemPrompt        string
	basePrompt          string
	defaultModel        string
	defaultPrompt       string
	defaultAPI          string
	defaultProv         string
	client              *llmclient.Client
	providers           *provider.Factory
	historyStore        history.Store
	historyMax          int
	contextMax          int
	immersive           map[string]bool
	judgeEnabled        bool
	judgeModel          string
	judgePrompt         string
	judgeTimeout        time.Duration
	judgeReasoning      string
	speakJudgeEnabled   bool
	speakJudgeModel     string
	speakJudgePrompt    string
	speakJudgeTimeout   time.Duration
	speakJudgeReasoning string
	speakJudgeFailOpen  bool
	postJudgeEnabled    bool
	postJudgeModel      string
	postJudgePrompt     string
	postJudgeTimeout    time.Duration
	postJudgeReasoning  string
	sessionStats        map[string]*sessionUsageStats
}

type sessionUsageStats struct {
	startedAt            time.Time
	historyCompressCount int
	contextCompressCount int
}

func normalizeAssistantReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low", "medium", "high", "none":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		// 助手默认不继承全局推理强度，未配置或无效时按 none 处理。
		return "none"
	}
}

func NewManager(cfg config.LLMConfig) *Manager {
	basePrompt := composeSystemPrompt(llmprompt.DefaultSystemPrompt, llmprompt.SpeakerSystemPrompt)
	systemPrompt := composeSystemPrompt(basePrompt, cfg.SystemPrompt)
	providerName := normalizeProvider(cfg.Provider)
	apiURL := normalizeAPIURL(providerName, cfg.API)
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
	speakJudgeTimeout := time.Duration(cfg.Immersive.SpeakGate.TimeoutMS) * time.Millisecond
	if speakJudgeTimeout <= 0 {
		speakJudgeTimeout = 1200 * time.Millisecond
	}
	postJudgePrompt := strings.TrimSpace(cfg.Immersive.PostCooldownJudge.Prompt)
	if postJudgePrompt == "" {
		postJudgePrompt = llmprompt.PostCooldownJudgePrompt
	}
	postJudgeModel := strings.TrimSpace(cfg.Immersive.PostCooldownJudge.Model)
	postJudgeReasoning := normalizeAssistantReasoningEffort(cfg.Immersive.PostCooldownJudge.ReasoningEffort)
	postJudgeTimeout := time.Duration(cfg.Immersive.PostCooldownJudge.TimeoutMS) * time.Millisecond
	if postJudgeTimeout <= 0 {
		postJudgeTimeout = 1200 * time.Millisecond
	}
	requestTimeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if requestTimeout <= 0 {
		requestTimeout = llmclient.DefaultRequestTimeout
	}
	client := llmclient.New(apiURL, cfg.Key)
	client.SetReasoningEffort(cfg.ReasoningEffort)
	client.SetShowReasoning(cfg.ShowReasoning)
	return &Manager{
		enabled:             cfg.Enabled,
		provider:            providerName,
		model:               strings.TrimSpace(cfg.Model),
		requestTimeout:      requestTimeout,
		systemPrompt:        systemPrompt,
		basePrompt:          basePrompt,
		defaultModel:        strings.TrimSpace(cfg.Model),
		defaultPrompt:       systemPrompt,
		defaultAPI:          apiURL,
		defaultProv:         providerName,
		client:              client,
		providers:           provider.NewFactory(client),
		historyStore:        history.NewMemoryStore(historyMax),
		historyMax:          historyMax,
		contextMax:          contextMax,
		judgeEnabled:        cfg.Immersive.MentionJudge.Enabled,
		judgeModel:          judgeModel,
		judgePrompt:         judgePrompt,
		judgeTimeout:        judgeTimeout,
		judgeReasoning:      judgeReasoning,
		speakJudgeEnabled:   cfg.Immersive.SpeakGate.Enabled,
		speakJudgeModel:     speakJudgeModel,
		speakJudgePrompt:    speakJudgePrompt,
		speakJudgeTimeout:   speakJudgeTimeout,
		speakJudgeReasoning: speakJudgeReasoning,
		speakJudgeFailOpen:  cfg.Immersive.SpeakGate.FailOpen,
		postJudgeEnabled:    cfg.Immersive.PostCooldownJudge.Enabled,
		postJudgeModel:      postJudgeModel,
		postJudgePrompt:     postJudgePrompt,
		postJudgeTimeout:    postJudgeTimeout,
		postJudgeReasoning:  postJudgeReasoning,
		sessionStats:        make(map[string]*sessionUsageStats),
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
	messages = m.compressMessages(ctx, provider, model, systemPrompt, sessionKey, messages)
	reply, err := m.generateWithProvider(ctx, provider, model, systemPrompt, messages, llmclient.RequestOptions{
		Source: "main_reply",
	})
	if err != nil {
		return "", err
	}
	m.appendHistory(sessionKey, userContent, reply)
	return reply, nil
}

func (m *Manager) ReplyStream(ctx context.Context, userInput, sessionKey, speaker string, onDelta func(delta string) error) (string, error) {
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
	messages = m.compressMessages(ctx, provider, model, systemPrompt, sessionKey, messages)
	reply, err := m.generateStreamWithProvider(ctx, provider, model, systemPrompt, messages, llmclient.RequestOptions{
		Source: "main_reply_stream",
	}, onDelta)
	if err != nil {
		return "", err
	}
	m.appendHistory(sessionKey, userContent, reply)
	return reply, nil
}

func (m *Manager) generateWithProvider(ctx context.Context, providerName, model, systemPrompt string, messages []Message, options llmclient.RequestOptions) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = llmclient.WithRequestOptions(ctx, options)
	timeout := m.requestTimeout
	if timeout <= 0 {
		timeout = llmclient.DefaultRequestTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	providerClient := m.providers.From(providerName)
	return providerClient.Generate(reqCtx, model, systemPrompt, messages)
}

func (m *Manager) generateStreamWithProvider(ctx context.Context, providerName, model, systemPrompt string, messages []Message, options llmclient.RequestOptions, onDelta func(delta string) error) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = llmclient.WithRequestOptions(ctx, options)
	timeout := m.requestTimeout
	if timeout <= 0 {
		timeout = llmclient.DefaultRequestTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	providerClient := m.providers.From(providerName)
	return providerClient.GenerateStream(reqCtx, model, systemPrompt, messages, onDelta)
}
