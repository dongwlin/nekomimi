package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// DefaultMaxResultChars keeps tool output bounded when no explicit config is wired yet.
const DefaultMaxResultChars = 4000

var (
	ErrProviderNameRequired = errors.New("provider name is required")
	ErrProviderNil          = errors.New("provider is required")
	ErrProviderExists       = errors.New("provider already registered")
)

type busRouter struct {
	mu        sync.RWMutex
	providers map[string]Provider
	order     []string
}

// NewRouter creates the unified tool router.
func NewRouter() Router {
	return &busRouter{
		providers: make(map[string]Provider),
	}
}

func (r *busRouter) Register(providerName string, provider Provider) error {
	name := normalizeName(providerName)
	if name == "" {
		return ErrProviderNameRequired
	}
	if provider == nil {
		return ErrProviderNil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("%w: %s", ErrProviderExists, name)
	}

	r.providers[name] = provider
	r.order = append(r.order, name)
	return nil
}

func (r *busRouter) ListTools(ctx context.Context) ([]Descriptor, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	providers := r.snapshotProviders()
	if len(providers) == 0 {
		return nil, nil
	}

	result := make([]Descriptor, 0, 8)
	seen := make(map[string]string)
	for _, item := range providers {
		descriptors, err := item.provider.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tools from provider %q: %w", item.name, err)
		}

		for _, descriptor := range descriptors {
			toolName := strings.TrimSpace(descriptor.Name)
			if toolName == "" {
				return nil, fmt.Errorf("provider %q returned tool with empty name", item.name)
			}
			if owner, ok := seen[toolName]; ok {
				return nil, fmt.Errorf("duplicate tool name %q from providers %q and %q", toolName, owner, item.name)
			}
			seen[toolName] = item.name

			normalized := descriptor
			normalized.Name = toolName
			if strings.TrimSpace(normalized.Source) == "" {
				normalized.Source = item.name
			}
			result = append(result, normalized)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (r *busRouter) CallTool(ctx context.Context, req CallRequest) (CallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errorResult("", ErrorCodeInvalidArguments, "tool name is required", false), nil
	}
	req.Name = name

	resolved, ok := r.resolveProviderForTool(ctx, name)
	if !ok {
		return errorResult(name, ErrorCodeNotFound, "tool not found", false), nil
	}

	result, err := resolved.CallTool(ctx, req)
	if err != nil {
		return mapCallFailure(name, err), nil
	}
	if strings.TrimSpace(result.Name) == "" {
		result.Name = name
	}
	if result.IsError && result.Error == nil {
		result.Error = &CallError{
			Code:      ErrorCodeInternal,
			Message:   "tool call failed",
			Retryable: false,
		}
	}
	return result, nil
}

func (r *busRouter) resolveProviderForTool(ctx context.Context, toolName string) (Provider, bool) {
	r.mu.RLock()
	if provider, ok := r.providers[firstPathSegment(toolName)]; ok {
		r.mu.RUnlock()
		return provider, true
	}
	r.mu.RUnlock()

	// Fallback for tools that do not follow "<provider>/<name>" naming.
	providers := r.snapshotProviders()
	for _, item := range providers {
		descriptors, err := item.provider.ListTools(ctx)
		if err != nil {
			continue
		}
		for _, descriptor := range descriptors {
			if strings.TrimSpace(descriptor.Name) == toolName {
				return item.provider, true
			}
		}
	}
	return nil, false
}

type providerItem struct {
	name     string
	provider Provider
}

func (r *busRouter) snapshotProviders() []providerItem {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]providerItem, 0, len(r.order))
	for _, name := range r.order {
		provider, ok := r.providers[name]
		if !ok {
			continue
		}
		items = append(items, providerItem{name: name, provider: provider})
	}
	return items
}

func firstPathSegment(value string) string {
	if idx := strings.IndexByte(value, '/'); idx > 0 {
		return normalizeName(value[:idx])
	}
	return normalizeName(value)
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func mapCallFailure(name string, err error) CallResult {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errorResult(name, ErrorCodeTimeout, "tool call timeout", true)
	case errors.Is(err, context.Canceled):
		return errorResult(name, ErrorCodeUnavailable, "tool call canceled", true)
	default:
		return errorResult(name, ErrorCodeInternal, err.Error(), false)
	}
}

func errorResult(name string, code ErrorCode, message string, retryable bool) CallResult {
	return CallResult{
		Name:    name,
		IsError: true,
		Error: &CallError{
			Code:      code,
			Message:   strings.TrimSpace(message),
			Retryable: retryable,
		},
	}
}
