package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

const (
	ProviderName            = tools.SourceMCP
	defaultCallTimeout      = 10 * time.Second
	defaultMaxPayloadBytes  = 256 * 1024
	toolNamePrefix          = "mcp/"
	contentFieldName        = "content"
	textFieldName           = "text"
	payloadLimitErrorFormat = "payload exceeds %d bytes limit"
)

type AdapterOptions struct {
	DefaultTimeout  time.Duration
	MaxPayloadBytes int
}

type adapter struct {
	registry        Registry
	client          Client
	defaultTimeout  time.Duration
	maxPayloadBytes int
}

var _ Adapter = (*adapter)(nil)
var _ tools.Provider = (*adapter)(nil)

// NewAdapter creates an MCP adapter that can be registered into tools.Router.
func NewAdapter(reg Registry, client Client, opts AdapterOptions) Adapter {
	timeout := opts.DefaultTimeout
	if timeout <= 0 {
		timeout = defaultCallTimeout
	}
	maxPayloadBytes := opts.MaxPayloadBytes
	if maxPayloadBytes <= 0 {
		maxPayloadBytes = defaultMaxPayloadBytes
	}
	return &adapter{
		registry:        reg,
		client:          client,
		defaultTimeout:  timeout,
		maxPayloadBytes: maxPayloadBytes,
	}
}

func (a *adapter) ListTools(ctx context.Context) ([]tools.Descriptor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.registry == nil {
		return nil, nil
	}
	if a.client == nil {
		return nil, errors.New("mcp client is unavailable")
	}

	servers := a.registry.ListServers()
	if len(servers) == 0 {
		return nil, nil
	}

	result := make([]tools.Descriptor, 0, 8)
	seen := make(map[string]struct{})
	for _, server := range servers {
		serverName := strings.TrimSpace(server.Name)
		if serverName == "" {
			continue
		}
		allow := newAllowSet(server.AllowTools)

		callCtx, cancel := withServerTimeout(ctx, a.resolveTimeout(server.Timeout))
		remoteTools, err := a.client.ListTools(callCtx, server)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("list tools from server %q: %w", serverName, err)
		}

		for _, remoteTool := range remoteTools {
			toolName := strings.TrimSpace(remoteTool.Name)
			if toolName == "" {
				continue
			}
			if !allow.allowed(toolName) {
				continue
			}

			fullName := composeToolName(serverName, toolName)
			if _, exists := seen[fullName]; exists {
				return nil, fmt.Errorf("duplicate mcp tool name %q", fullName)
			}
			seen[fullName] = struct{}{}

			result = append(result, tools.Descriptor{
				Name:        fullName,
				Description: strings.TrimSpace(remoteTool.Description),
				Source:      tools.SourceMCP,
				InputSchema: cloneRawMessage(remoteTool.InputSchema),
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (a *adapter) CallTool(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.registry == nil || a.client == nil {
		return callError(req.Name, tools.ErrorCodeUnavailable, "mcp adapter is unavailable", true), nil
	}

	fullName := strings.TrimSpace(req.Name)
	if fullName == "" {
		return callError("", tools.ErrorCodeInvalidArguments, "tool name is required", false), nil
	}

	serverName, toolName, ok := parseToolName(fullName)
	if !ok {
		return callError(fullName, tools.ErrorCodeNotFound, "tool not found", false), nil
	}

	server, found := a.registry.GetServer(serverName)
	if !found {
		return callError(fullName, tools.ErrorCodeNotFound, "tool not found", false), nil
	}
	if !newAllowSet(server.AllowTools).allowed(toolName) {
		return callError(fullName, tools.ErrorCodeNotFound, "tool not found", false), nil
	}

	arguments := normalizeArguments(req.Arguments)
	maxPayloadBytes := a.resolveMaxPayload(server.MaxPayloadBytes)
	if exceeded(arguments, maxPayloadBytes) {
		return callError(fullName, tools.ErrorCodeInvalidArguments, fmt.Sprintf(payloadLimitErrorFormat, maxPayloadBytes), false), nil
	}

	callCtx, cancel := withServerTimeout(ctx, a.resolveTimeout(server.Timeout))
	defer cancel()

	rawResult, err := a.client.CallTool(callCtx, server, toolName, arguments)
	if err != nil {
		return mapCallFailure(fullName, err), nil
	}
	if len(rawResult) == 0 {
		rawResult = json.RawMessage(`{}`)
	}
	if !json.Valid(rawResult) {
		return callError(fullName, tools.ErrorCodeInternal, "invalid MCP response payload", false), nil
	}
	if exceeded(rawResult, maxPayloadBytes) {
		return callError(fullName, tools.ErrorCodeInternal, fmt.Sprintf(payloadLimitErrorFormat, maxPayloadBytes), false), nil
	}

	return tools.CallResult{
		Name:       fullName,
		Content:    toPromptContent(rawResult),
		Structured: compactJSON(rawResult),
	}, nil
}

func (a *adapter) resolveTimeout(serverTimeout time.Duration) time.Duration {
	if serverTimeout > 0 {
		return serverTimeout
	}
	return a.defaultTimeout
}

func (a *adapter) resolveMaxPayload(serverLimit int) int {
	if serverLimit > 0 {
		return serverLimit
	}
	if a.maxPayloadBytes > 0 {
		return a.maxPayloadBytes
	}
	return 0
}

type allowSet map[string]struct{}

func newAllowSet(values []string) allowSet {
	if len(values) == 0 {
		return nil
	}
	result := make(allowSet, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		result[name] = struct{}{}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (s allowSet) allowed(toolName string) bool {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return false
	}
	if len(s) == 0 {
		return true
	}
	_, ok := s[trimmed]
	return ok
}

func composeToolName(serverName, toolName string) string {
	return toolNamePrefix + strings.TrimSpace(serverName) + "/" + strings.TrimSpace(toolName)
}

func parseToolName(name string) (serverName string, toolName string, ok bool) {
	trimmed := strings.TrimSpace(name)
	if !strings.HasPrefix(trimmed, toolNamePrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(trimmed, toolNamePrefix)
	serverName, toolName, found := strings.Cut(rest, "/")
	if !found {
		return "", "", false
	}
	serverName = strings.TrimSpace(serverName)
	toolName = strings.TrimSpace(toolName)
	if serverName == "" || toolName == "" {
		return "", "", false
	}
	return serverName, toolName, true
}

func normalizeArguments(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return json.RawMessage(`{}`)
	}
	return raw
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func compactJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return cloneRawMessage(raw)
	}
	return append(json.RawMessage(nil), buf.Bytes()...)
}

func toPromptContent(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}

	var object map[string]any
	if err := json.Unmarshal(raw, &object); err == nil && object != nil {
		if text, ok := lookupStringField(object, contentFieldName); ok {
			return text
		}
		if text, ok := lookupStringField(object, textFieldName); ok {
			return text
		}
	}

	var textValue string
	if err := json.Unmarshal(raw, &textValue); err == nil {
		return strings.TrimSpace(textValue)
	}
	return trimmed
}

func lookupStringField(object map[string]any, key string) (string, bool) {
	value, ok := object[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func withServerTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func exceeded(raw json.RawMessage, maxBytes int) bool {
	return maxBytes > 0 && len(raw) > maxBytes
}

func callError(name string, code tools.ErrorCode, message string, retryable bool) tools.CallResult {
	return tools.CallResult{
		Name:    strings.TrimSpace(name),
		IsError: true,
		Error: &tools.CallError{
			Code:      code,
			Message:   strings.TrimSpace(message),
			Retryable: retryable,
		},
	}
}

func mapCallFailure(name string, err error) tools.CallResult {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return callError(name, tools.ErrorCodeTimeout, "tool call timeout", true)
	case errors.Is(err, context.Canceled):
		return callError(name, tools.ErrorCodeUnavailable, "tool call canceled", true)
	case isDisconnectError(err):
		return callError(name, tools.ErrorCodeUnavailable, "mcp server disconnected", true)
	default:
		return callError(name, tools.ErrorCodeInternal, err.Error(), false)
	}
}

func isDisconnectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "disconnected")
}
