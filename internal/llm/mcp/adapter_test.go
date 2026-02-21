package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

func TestAdapter_RouterIntegration_ListAndCall(t *testing.T) {
	registry := NewRegistry()
	registry.SetServers([]ServerConfig{
		{
			Name:       "alpha",
			AllowTools: []string{"echo"},
		},
		{
			Name: "beta",
		},
	})

	client := &stubClient{
		listFn: func(ctx context.Context, server ServerConfig) ([]RemoteTool, error) {
			switch server.Name {
			case "alpha":
				return []RemoteTool{
					{Name: "echo", Description: "echo input"},
					{Name: "hidden", Description: "must be filtered by allowlist"},
				}, nil
			case "beta":
				return []RemoteTool{
					{Name: "ping", Description: "health ping"},
				}, nil
			default:
				return nil, errors.New("unexpected server")
			}
		},
		callFn: func(ctx context.Context, server ServerConfig, toolName string, arguments json.RawMessage) (json.RawMessage, error) {
			if server.Name != "alpha" || toolName != "echo" {
				return nil, errors.New("unexpected call target")
			}
			var args map[string]any
			if err := json.Unmarshal(arguments, &args); err != nil {
				return nil, err
			}
			if args["text"] != "hello" {
				return nil, errors.New("unexpected call arguments")
			}
			return mustRawJSON(t, map[string]any{
				"content": "echo: hello",
				"ok":      true,
			}), nil
		},
	}

	adapter := NewAdapter(registry, client, AdapterOptions{})
	router := tools.NewRouter()
	if err := router.Register(ProviderName, adapter); err != nil {
		t.Fatalf("register mcp adapter failed: %v", err)
	}

	list, err := router.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools failed: %v", err)
	}
	assertToolNames(t, list, []string{
		"mcp/alpha/echo",
		"mcp/beta/ping",
	})

	result, err := router.CallTool(context.Background(), tools.CallRequest{
		Name:      "mcp/alpha/echo",
		Arguments: mustRawJSON(t, map[string]any{"text": "hello"}),
	})
	if err != nil {
		t.Fatalf("call tool failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("call should succeed, got error: %+v", result.Error)
	}
	if result.Content != "echo: hello" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
	structured := decodeObjectMap(t, result.Structured)
	if okValue, _ := structured["ok"].(bool); !okValue {
		t.Fatalf("structured payload missing ok=true: %#v", structured)
	}
}

func TestAdapter_ServerAllowlistAndPayloadLimit(t *testing.T) {
	registry := NewRegistry()
	registry.SetServers([]ServerConfig{
		{
			Name:            "safe",
			AllowTools:      []string{"echo"},
			MaxPayloadBytes: 32,
		},
	})

	callCount := 0
	client := &stubClient{
		listFn: func(ctx context.Context, server ServerConfig) ([]RemoteTool, error) {
			return []RemoteTool{{Name: "echo"}, {Name: "hidden"}}, nil
		},
		callFn: func(ctx context.Context, server ServerConfig, toolName string, arguments json.RawMessage) (json.RawMessage, error) {
			callCount++
			if toolName != "echo" {
				return nil, errors.New("unexpected tool")
			}
			return mustRawJSON(t, map[string]any{
				"content": strings.Repeat("x", 64),
			}), nil
		},
	}

	adapter := NewAdapter(registry, client, AdapterOptions{})

	notFoundServer, err := adapter.CallTool(context.Background(), tools.CallRequest{
		Name:      "mcp/unknown/echo",
		Arguments: mustRawJSON(t, map[string]any{"text": "x"}),
	})
	if err != nil {
		t.Fatalf("call with unknown server failed: %v", err)
	}
	assertErrorCode(t, notFoundServer, tools.ErrorCodeNotFound)

	disallowedTool, err := adapter.CallTool(context.Background(), tools.CallRequest{
		Name:      "mcp/safe/hidden",
		Arguments: mustRawJSON(t, map[string]any{"text": "x"}),
	})
	if err != nil {
		t.Fatalf("call with disallowed tool failed: %v", err)
	}
	assertErrorCode(t, disallowedTool, tools.ErrorCodeNotFound)

	tooLargeArgs, err := adapter.CallTool(context.Background(), tools.CallRequest{
		Name:      "mcp/safe/echo",
		Arguments: mustRawJSON(t, map[string]any{"text": strings.Repeat("a", 128)}),
	})
	if err != nil {
		t.Fatalf("call with oversized args failed: %v", err)
	}
	assertErrorCode(t, tooLargeArgs, tools.ErrorCodeInvalidArguments)
	if callCount != 0 {
		t.Fatalf("client should not be called when args exceed payload limit")
	}

	tooLargeResponse, err := adapter.CallTool(context.Background(), tools.CallRequest{
		Name:      "mcp/safe/echo",
		Arguments: mustRawJSON(t, map[string]any{"text": "ok"}),
	})
	if err != nil {
		t.Fatalf("call with oversized response failed: %v", err)
	}
	assertErrorCode(t, tooLargeResponse, tools.ErrorCodeInternal)
	if callCount != 1 {
		t.Fatalf("client call count mismatch: got %d, want %d", callCount, 1)
	}
}

func TestAdapter_TimeoutAndDisconnectHandling(t *testing.T) {
	registry := NewRegistry()
	registry.SetServers([]ServerConfig{
		{
			Name:       "svc",
			AllowTools: []string{"echo"},
		},
	})

	t.Run("default timeout", func(t *testing.T) {
		defaultTimeout := 50 * time.Millisecond
		var timeoutBudget time.Duration
		client := &stubClient{
			callFn: func(ctx context.Context, server ServerConfig, toolName string, arguments json.RawMessage) (json.RawMessage, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					return nil, errors.New("missing call deadline")
				}
				timeoutBudget = time.Until(deadline)
				return nil, context.DeadlineExceeded
			},
		}

		adapter := NewAdapter(registry, client, AdapterOptions{
			DefaultTimeout: defaultTimeout,
		})

		result, err := adapter.CallTool(context.Background(), tools.CallRequest{
			Name:      "mcp/svc/echo",
			Arguments: mustRawJSON(t, map[string]any{"text": "hi"}),
		})
		if err != nil {
			t.Fatalf("call tool failed: %v", err)
		}
		assertErrorCode(t, result, tools.ErrorCodeTimeout)
		if timeoutBudget <= 0 {
			t.Fatalf("timeout budget should be positive, got %v", timeoutBudget)
		}
		if timeoutBudget > defaultTimeout+100*time.Millisecond {
			t.Fatalf("timeout budget should use adapter default timeout, got %v", timeoutBudget)
		}
	})

	t.Run("disconnect error", func(t *testing.T) {
		client := &stubClient{
			callFn: func(ctx context.Context, server ServerConfig, toolName string, arguments json.RawMessage) (json.RawMessage, error) {
				return nil, io.EOF
			},
		}
		adapter := NewAdapter(registry, client, AdapterOptions{})

		result, err := adapter.CallTool(context.Background(), tools.CallRequest{
			Name:      "mcp/svc/echo",
			Arguments: mustRawJSON(t, map[string]any{"text": "hi"}),
		})
		if err != nil {
			t.Fatalf("call tool failed: %v", err)
		}
		assertErrorCode(t, result, tools.ErrorCodeUnavailable)
		if result.Error == nil || !result.Error.Retryable {
			t.Fatalf("disconnect should be retryable: %+v", result.Error)
		}
	})
}

type stubClient struct {
	listFn func(ctx context.Context, server ServerConfig) ([]RemoteTool, error)
	callFn func(ctx context.Context, server ServerConfig, toolName string, arguments json.RawMessage) (json.RawMessage, error)
}

func (s *stubClient) ListTools(ctx context.Context, server ServerConfig) ([]RemoteTool, error) {
	if s.listFn == nil {
		return nil, nil
	}
	return s.listFn(ctx, server)
}

func (s *stubClient) CallTool(ctx context.Context, server ServerConfig, toolName string, arguments json.RawMessage) (json.RawMessage, error) {
	if s.callFn == nil {
		return mustRawJSON(nil, map[string]any{}), nil
	}
	return s.callFn(ctx, server, toolName, arguments)
}

func assertToolNames(t *testing.T, got []tools.Descriptor, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tool count mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("tool[%d] mismatch: got %q, want %q", i, got[i].Name, want[i])
		}
	}
}

func assertErrorCode(t *testing.T, result tools.CallResult, want tools.ErrorCode) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected error result, got success")
	}
	if result.Error == nil {
		t.Fatalf("expected error payload")
	}
	if result.Error.Code != want {
		t.Fatalf("error code mismatch: got %q, want %q", result.Error.Code, want)
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	if value == nil {
		return json.RawMessage(`{}`)
	}
	data, err := json.Marshal(value)
	if err != nil {
		if t != nil {
			t.Fatalf("marshal JSON failed: %v", err)
		}
		return json.RawMessage(`{}`)
	}
	return data
}

func decodeObjectMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	result := make(map[string]any)
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode object failed: %v", err)
	}
	return result
}
