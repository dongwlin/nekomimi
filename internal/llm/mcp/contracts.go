package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

const ToolNameFormat = "mcp/<server>/<tool>"

// ServerConfig defines one MCP server contract.
type ServerConfig struct {
	Name            string
	Transport       string
	URL             string
	Command         string
	Args            []string
	Headers         map[string]string
	AllowTools      []string
	Timeout         time.Duration
	MaxPayloadBytes int
}

// RemoteTool is the MCP-native tool descriptor.
type RemoteTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Client is the wire-level MCP contract.
type Client interface {
	ListTools(ctx context.Context, server ServerConfig) ([]RemoteTool, error)
	CallTool(ctx context.Context, server ServerConfig, toolName string, arguments json.RawMessage) (json.RawMessage, error)
}

// Registry stores and resolves configured MCP servers.
type Registry interface {
	SetServers(servers []ServerConfig)
	ListServers() []ServerConfig
	GetServer(name string) (ServerConfig, bool)
}

// Adapter bridges MCP servers into the unified tool router.
type Adapter interface {
	ListTools(ctx context.Context) ([]tools.Descriptor, error)
	CallTool(ctx context.Context, req tools.CallRequest) (tools.CallResult, error)
}
