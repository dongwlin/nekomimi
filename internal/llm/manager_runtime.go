package llm

import (
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/chatlog"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/ctxasm"
	"github.com/dongwlin/nekomimi/internal/diary"
	"github.com/dongwlin/nekomimi/internal/llm/mcp"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
	"github.com/rs/zerolog/log"
)

const defaultToolLoopMaxSteps = 8

type runtimeConfig struct {
	assemblyOptions   ctxasm.Options
	recentChatLimit   int
	recentDiaryLimit  int
	toolsEnabled      bool
	toolLoopMaxSteps  int
	toolLoopTimeout   time.Duration
	internalToolOpts  tools.InternalProviderOptions
	mcpEnabled        bool
	mcpServers        []mcp.ServerConfig
	mcpAdapterOptions mcp.AdapterOptions
}

func normalizeRuntimeConfig(cfg config.LLMConfig, requestTimeout time.Duration, contextMax int) runtimeConfig {
	recentChatLimit := normalizeRecentChatLimit(cfg)
	recentDiaryLimit := normalizeRecentDiaryLimit(cfg)

	toolLoopMaxSteps := cfg.ToolLoop.MaxSteps
	if toolLoopMaxSteps <= 0 {
		toolLoopMaxSteps = defaultToolLoopMaxSteps
	}
	toolLoopTimeout := time.Duration(cfg.ToolLoop.TimeoutMS) * time.Millisecond
	if toolLoopTimeout <= 0 {
		toolLoopTimeout = requestTimeout
	}

	mcpDefaultTimeout := time.Duration(cfg.MCP.DefaultTimeoutMS) * time.Millisecond
	if mcpDefaultTimeout < 0 {
		mcpDefaultTimeout = 0
	}

	return runtimeConfig{
		assemblyOptions: ctxasm.Options{
			RecentChatLimit:  recentChatLimit,
			RecentDiaryLimit: recentDiaryLimit,
			MaxChars:         contextMax,
		},
		recentChatLimit:  recentChatLimit,
		recentDiaryLimit: recentDiaryLimit,
		toolsEnabled:     cfg.Tools.Enabled,
		toolLoopMaxSteps: toolLoopMaxSteps,
		toolLoopTimeout:  toolLoopTimeout,
		internalToolOpts: tools.InternalProviderOptions{
			MaxResultChars: cfg.Tools.MaxResultChars,
		},
		mcpEnabled: cfg.MCP.Enabled,
		mcpServers: normalizeMCPServers(cfg.MCP.Servers),
		mcpAdapterOptions: mcp.AdapterOptions{
			DefaultTimeout:  mcpDefaultTimeout,
			MaxPayloadBytes: cfg.MCP.MaxPayloadBytes,
		},
	}
}

func normalizeRecentChatLimit(cfg config.LLMConfig) int {
	if cfg.ContextAssembly.RecentChatLimit > 0 {
		return cfg.ContextAssembly.RecentChatLimit
	}
	return ctxasm.DefaultRecentChatLimit
}

func normalizeRecentDiaryLimit(cfg config.LLMConfig) int {
	if cfg.ContextAssembly.RecentDiaryLimit > 0 {
		return cfg.ContextAssembly.RecentDiaryLimit
	}
	return ctxasm.DefaultRecentDiaryLimit
}

func normalizeMCPServers(servers []config.MCPServerConfig) []mcp.ServerConfig {
	if len(servers) == 0 {
		return nil
	}

	result := make([]mcp.ServerConfig, 0, len(servers))
	for _, server := range servers {
		timeout := time.Duration(server.TimeoutMS) * time.Millisecond
		if timeout < 0 {
			timeout = 0
		}
		result = append(result, mcp.ServerConfig{
			Name:            strings.TrimSpace(server.Name),
			Transport:       strings.TrimSpace(server.Transport),
			URL:             strings.TrimSpace(server.URL),
			Command:         strings.TrimSpace(server.Command),
			Args:            append([]string(nil), server.Args...),
			Headers:         cloneStringMap(server.Headers),
			AllowTools:      append([]string(nil), server.AllowTools...),
			Timeout:         timeout,
			MaxPayloadBytes: 0,
		})
	}
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for k, v := range values {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		copied[key] = strings.TrimSpace(v)
	}
	if len(copied) == 0 {
		return nil
	}
	return copied
}

func buildToolRouter(chatStore chatlog.Store, diaryStore diary.Store, cfg runtimeConfig) tools.Router {
	router := tools.NewRouter()
	if !cfg.toolsEnabled {
		return router
	}

	internalProvider := tools.NewInternalProvider(chatStore, diaryStore, cfg.internalToolOpts)
	if err := router.Register(tools.InternalProviderName, internalProvider); err != nil {
		log.Warn().Err(err).Msg("register internal tools provider failed")
	}

	if cfg.mcpEnabled {
		registry := mcp.NewRegistry()
		registry.SetServers(cfg.mcpServers)
		adapter := mcp.NewAdapter(registry, mcp.NewNoopClient(), cfg.mcpAdapterOptions)
		if err := router.Register(mcp.ProviderName, adapter); err != nil {
			log.Warn().Err(err).Msg("register mcp tools provider failed")
		}
	}

	return router
}
