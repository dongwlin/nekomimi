package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

var errNoopClientUnsupported = errors.New("mcp transport client is not configured")

type noopClient struct{}

// NewNoopClient returns a placeholder MCP client for manager wiring.
// It exposes no remote tools and fails direct calls with a stable error.
func NewNoopClient() Client {
	return noopClient{}
}

func (noopClient) ListTools(ctx context.Context, server ServerConfig) ([]RemoteTool, error) {
	return nil, nil
}

func (noopClient) CallTool(ctx context.Context, server ServerConfig, toolName string, arguments json.RawMessage) (json.RawMessage, error) {
	return nil, errNoopClientUnsupported
}
