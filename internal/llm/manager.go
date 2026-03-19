package llm

import (
	"strings"
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/ctxasm"
	"github.com/dongwlin/nekomimi/internal/diary"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

type Manager struct {
	mu               sync.RWMutex
	current          currentConfig
	defaults         defaultConfig
	client           *llmclient.Client
	chatStore        chatlog.Store
	diaryStore       diary.Store
	contextAssembler *ctxasm.Assembler
	toolRouter       tools.Router
	sessions         *sessionState
}

type currentConfig struct {
	enabled          bool
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
	model  string
	prompt string
	apiURL string
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
	apiURL := strings.TrimSpace(cfg.API)
	if apiURL == "" {
		apiURL = llmclient.DefaultAnthropicAPI
	}
	contextMax := cfg.ContextMax
	if contextMax < 0 {
		contextMax = 0
	}
	requestTimeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if requestTimeout <= 0 {
		requestTimeout = llmclient.DefaultRequestTimeout
	}

	client := llmclient.New(apiURL, cfg.Key)
	client.SetThinkingConfig(thinkingConfigFromConfig(cfg))
	client.SetOutputConfig(outputConfigFromConfig(cfg))
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
			model:  strings.TrimSpace(cfg.Model),
			prompt: systemPrompt,
			apiURL: apiURL,
		},
		client:           client,
		chatStore:        cs,
		diaryStore:       ds,
		contextAssembler: ctxasm.New(cs, ds, runtimeCfg.assemblyOptions),
		toolRouter:       buildToolRouter(cs, ds, runtimeCfg),
		sessions:         newSessionState(),
	}
}
