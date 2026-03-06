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
	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
	"github.com/dongwlin/nekomimi/internal/llm/provider"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
	"github.com/rs/zerolog/log"
)

type Manager struct {
	mu               sync.RWMutex
	current          currentConfig
	defaults         defaultConfig
	client           *llmclient.Client
	providers        *provider.Factory
	chatStore        chatlog.Store
	diaryStore       diary.Store
	contextAssembler *contextassemble.Assembler
	toolRouter       tools.Router
	sessions         *sessionState
}

type currentConfig struct {
	enabled          bool
	provider         string
	model            string
	requestTimeout   time.Duration
	systemPrompt     string
	assistantSpeaker string
	basePrompt       string
	contextMax       int
	recentChatLimit  int
	recentDiaryLimit int
	toolsEnabled     bool
	toolLoopMaxSteps int
	toolLoopTimeout  time.Duration
}

type defaultConfig struct {
	provider string
	model    string
	prompt   string
	apiURL   string
}

type sessionUsageStats struct {
	startedAt        time.Time
	contextTrimCount int
	causalSeq        int64
}

// ManagerDeps holds optional dependencies for the Manager. When a field is nil,
// the Manager falls back to an in-memory default implementation.
type ManagerDeps struct {
	ChatStore  chatlog.Store
	DiaryStore diary.Store
}

func NewManager(cfg config.LLMConfig, deps ManagerDeps) *Manager {
	basePrompt, systemPrompt := composeConfiguredSystemPrompt(cfg.SystemPrompt)
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

	cs := deps.ChatStore
	if cs == nil {
		cs = chatlog.NewMemoryStore()
	}
	ds := deps.DiaryStore
	if ds == nil {
		ds = diary.NewMemoryStore()
	}
	runtimeCfg := normalizeRuntimeConfig(cfg, requestTimeout, contextMax)

	return &Manager{
		current: currentConfig{
			enabled:          cfg.Enabled,
			provider:         providerName,
			model:            strings.TrimSpace(cfg.Model),
			requestTimeout:   requestTimeout,
			systemPrompt:     systemPrompt,
			assistantSpeaker: "name=assistant",
			basePrompt:       basePrompt,
			contextMax:       contextMax,
			recentChatLimit:  runtimeCfg.recentChatLimit,
			recentDiaryLimit: runtimeCfg.recentDiaryLimit,
			toolsEnabled:     runtimeCfg.toolsEnabled,
			toolLoopMaxSteps: runtimeCfg.toolLoopMaxSteps,
			toolLoopTimeout:  runtimeCfg.toolLoopTimeout,
		},
		defaults: defaultConfig{
			provider: providerName,
			model:    strings.TrimSpace(cfg.Model),
			prompt:   systemPrompt,
			apiURL:   apiURL,
		},
		client:           client,
		providers:        provider.NewFactory(client),
		chatStore:        cs,
		diaryStore:       ds,
		contextAssembler: contextassemble.New(cs, ds, runtimeCfg.assemblyOptions),
		toolRouter:       buildToolRouter(cs, ds, runtimeCfg),
		sessions:         newSessionState(),
	}
}

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

func (m *Manager) SetProvider(provider string) error {
	normalized := normalizeProvider(provider)
	if normalized == llmProviderGemini {
		return errors.New("gemini is not implemented")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current.provider = normalized
	m.client.SetAPIURL(normalizeAPIURL(normalized, m.client.APIURL()))
	return nil
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
	m.current.provider = m.defaults.provider
	m.current.model = m.defaults.model
	m.current.systemPrompt = m.defaults.prompt
	m.client.SetAPIURL(m.defaults.apiURL)
}

func (m *Manager) Status() (enabled bool, provider string, model string, systemPrompt string, apiURL string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.enabled, m.current.provider, m.current.model, m.current.systemPrompt, m.client.APIURL()
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
	return m.replyStreamWithExtraPrompt(ctx, userInput, sessionKey, speaker, extraPrompt, onEvent, true, nil)
}

func (m *Manager) ReplyStreamWithExtraPromptAllowTools(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler, immersiveCtx *contextassemble.ImmersiveContext) (string, error) {
	return m.replyStreamWithExtraPrompt(ctx, userInput, sessionKey, speaker, extraPrompt, onEvent, false, immersiveCtx)
}

func (m *Manager) DecideImmersiveIntent(ctx context.Context, userInput, sessionKey, speaker string, immersiveCtx *contextassemble.ImmersiveContext) (llmintent.ControlIntent, error) {
	startedAt := time.Now()
	intent, err := m.decideIntentWithPipeline(ctx, pipelineRequest{
		UserInput:        userInput,
		SessionKey:       sessionKey,
		Speaker:          speaker,
		ExtraPrompt:      llmprompt.ImmersiveControlPrompt,
		Source:           "immersive_control_intent",
		AppendTurn:       false,
		ImmersiveContext: immersiveCtx,
	})
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "immersive_control_intent").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm immersive control intent failed")
		return llmintent.ControlIntent{}, err
	}

	log.Info().
		Str("request_source", "immersive_control_intent").
		Str("intent_action", string(intent.Action)).
		Int("intent_wait_ms", intent.WaitMS).
		Str("intent_reason", intent.Reason).
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm immersive control intent completed")
	return intent, nil
}

func (m *Manager) replyStreamWithExtraPrompt(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler, disableTools bool, immersiveCtx *contextassemble.ImmersiveContext) (string, error) {
	startedAt := time.Now()
	reply, err := m.replyStreamWithPipeline(ctx, pipelineRequest{
		UserInput:        userInput,
		SessionKey:       sessionKey,
		Speaker:          speaker,
		ExtraPrompt:      extraPrompt,
		Source:           "extra_prompt_reply_stream",
		AppendTurn:       false,
		DisableTools:     disableTools,
		ImmersiveContext: immersiveCtx,
	}, onEvent)
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "extra_prompt_reply_stream").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm assistant streaming reply failed")
		return "", err
	}
	log.Info().
		Str("request_source", "extra_prompt_reply_stream").
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm assistant streaming reply completed")
	return reply, nil
}

func (m *Manager) generateWithProvider(ctx context.Context, providerName, model, systemPrompt string, messages []Message, options llmclient.RequestOptions) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = llmclient.WithRequestOptions(ctx, options)
	timeout := m.current.requestTimeout
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
	timeout := m.current.requestTimeout
	if timeout <= 0 {
		timeout = llmclient.DefaultRequestTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	providerClient := m.providers.From(providerName)
	return providerClient.GenerateStream(reqCtx, model, systemPrompt, messages, onDelta)
}
