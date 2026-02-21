package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
	"github.com/dongwlin/nekomimi/internal/llm/provider"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
	"github.com/rs/zerolog/log"
)

type Manager struct {
	mu               sync.RWMutex
	enabled          bool
	provider         string
	model            string
	requestTimeout   time.Duration
	systemPrompt     string
	basePrompt       string
	defaultModel     string
	defaultPrompt    string
	defaultAPI       string
	defaultProv      string
	client           *llmclient.Client
	providers        *provider.Factory
	chatStore        chatlog.Store
	diaryStore       diary.Store
	contextAssembler *contextassemble.Assembler
	toolRouter       tools.Router
	contextMax       int
	recentChatLimit  int
	recentDiaryLimit int
	toolsEnabled     bool
	toolLoopMaxSteps int
	toolLoopTimeout  time.Duration
	immersive        map[string]bool
	sessionStats     map[string]*sessionUsageStats
}

type sessionUsageStats struct {
	startedAt        time.Time
	contextTrimCount int
}

func NewManager(cfg config.LLMConfig) *Manager {
	basePrompt := composeSystemPrompt(llmprompt.DefaultSystemPrompt, llmprompt.SpeakerSystemPrompt)
	systemPrompt := composeSystemPrompt(basePrompt, cfg.SystemPrompt)
	providerName := normalizeProvider(cfg.Provider)
	apiURL := normalizeAPIURL(providerName, cfg.API)
	contextMax := cfg.ContextMax
	if contextMax < 0 {
		contextMax = 0
	}
	requestTimeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if requestTimeout <= 0 {
		requestTimeout = llmclient.DefaultRequestTimeout
	}

	client := llmclient.New(apiURL, cfg.Key)
	client.SetReasoningEffort(cfg.ReasoningEffort)
	client.SetThinkingType(cfg.ThinkingType)
	client.SetShowReasoning(cfg.ShowReasoning)

	chatStore := chatlog.NewMemoryStore()
	diaryStore := diary.NewMemoryStore()
	runtimeCfg := normalizeRuntimeConfig(cfg, requestTimeout, contextMax)

	return &Manager{
		enabled:          cfg.Enabled,
		provider:         providerName,
		model:            strings.TrimSpace(cfg.Model),
		requestTimeout:   requestTimeout,
		systemPrompt:     systemPrompt,
		basePrompt:       basePrompt,
		defaultModel:     strings.TrimSpace(cfg.Model),
		defaultPrompt:    systemPrompt,
		defaultAPI:       apiURL,
		defaultProv:      providerName,
		client:           client,
		providers:        provider.NewFactory(client),
		chatStore:        chatStore,
		diaryStore:       diaryStore,
		contextAssembler: contextassemble.New(chatStore, diaryStore, runtimeCfg.assemblyOptions),
		toolRouter:       buildToolRouter(chatStore, diaryStore, runtimeCfg),
		contextMax:       contextMax,
		recentChatLimit:  runtimeCfg.recentChatLimit,
		recentDiaryLimit: runtimeCfg.recentDiaryLimit,
		toolsEnabled:     runtimeCfg.toolsEnabled,
		toolLoopMaxSteps: runtimeCfg.toolLoopMaxSteps,
		toolLoopTimeout:  runtimeCfg.toolLoopTimeout,
		sessionStats:     make(map[string]*sessionUsageStats),
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
		return errors.New("gemini is not implemented")
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
	startedAt := time.Now()
	reply, err := m.replyWithPipeline(ctx, pipelineRequest{
		UserInput:   userInput,
		SessionKey:  sessionKey,
		Speaker:     speaker,
		ExtraPrompt: "",
		Source:      "main_reply",
		AppendTurn:  true,
	})
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "main_reply").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm assistant reply failed")
		return "", err
	}
	log.Info().
		Str("request_source", "main_reply").
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm assistant reply completed")
	return reply, nil
}

func (m *Manager) ReplyStream(ctx context.Context, userInput, sessionKey, speaker string, onEvent StreamEventHandler) (string, error) {
	startedAt := time.Now()
	reply, err := m.replyStreamWithPipeline(ctx, pipelineRequest{
		UserInput:   userInput,
		SessionKey:  sessionKey,
		Speaker:     speaker,
		ExtraPrompt: "",
		Source:      "main_reply_stream",
		AppendTurn:  true,
	}, onEvent)
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "main_reply_stream").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm assistant streaming reply failed")
		return "", err
	}
	log.Info().
		Str("request_source", "main_reply_stream").
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm assistant streaming reply completed")
	return reply, nil
}

func (m *Manager) ReplyStreamWithExtraPrompt(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler) (string, error) {
	startedAt := time.Now()
	reply, err := m.replyStreamWithPipeline(ctx, pipelineRequest{
		UserInput:   userInput,
		SessionKey:  sessionKey,
		Speaker:     speaker,
		ExtraPrompt: extraPrompt,
		Source:      "immersive_control_reply_stream",
		AppendTurn:  false,
	}, onEvent)
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "immersive_control_reply_stream").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm assistant streaming reply failed")
		return "", err
	}
	log.Info().
		Str("request_source", "immersive_control_reply_stream").
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm assistant streaming reply completed")
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
