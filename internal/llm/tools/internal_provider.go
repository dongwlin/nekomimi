package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
)

const (
	InternalProviderName = SourceInternal
	ToolReadChatHistory  = "internal/read_chat_history"
	ToolReadDiary        = "internal/read_diary"
	ToolWriteDiary       = "internal/write_diary"
	defaultReadListLimit = 20
	defaultMaxReadLimit  = 200
	defaultDiaryAuthor   = "assistant"
)

type InternalProviderOptions struct {
	MaxResultChars  int
	DefaultReadList int
	MaxReadLimit    int
}

type internalProvider struct {
	tools           map[string]Callable
	toolNames       []string
	maxResultChars  int
	defaultReadList int
	maxReadLimit    int
}

// NewInternalProvider creates the built-in internal tools provider.
func NewInternalProvider(chatStore chatlog.Store, diaryStore diary.Store, opts InternalProviderOptions) Provider {
	defaultReadList := opts.DefaultReadList
	if defaultReadList <= 0 {
		defaultReadList = defaultReadListLimit
	}

	maxReadLimit := opts.MaxReadLimit
	if maxReadLimit <= 0 {
		maxReadLimit = defaultMaxReadLimit
	}
	if defaultReadList > maxReadLimit {
		defaultReadList = maxReadLimit
	}

	maxResultChars := opts.MaxResultChars
	if maxResultChars <= 0 {
		maxResultChars = DefaultMaxResultChars
	}

	p := &internalProvider{
		tools:           make(map[string]Callable),
		maxResultChars:  maxResultChars,
		defaultReadList: defaultReadList,
		maxReadLimit:    maxReadLimit,
	}

	p.register(newReadChatHistoryTool(chatStore, defaultReadList, maxReadLimit))
	p.register(newReadDiaryTool(diaryStore, defaultReadList, maxReadLimit))
	p.register(newWriteDiaryTool(diaryStore))
	return p
}

func (p *internalProvider) ListTools(ctx context.Context) ([]Descriptor, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	descriptors := make([]Descriptor, 0, len(p.toolNames))
	for _, name := range p.toolNames {
		callable, ok := p.tools[name]
		if !ok {
			continue
		}
		descriptor := callable.Descriptor()
		descriptor.Name = strings.TrimSpace(descriptor.Name)
		if descriptor.Name == "" {
			return nil, fmt.Errorf("internal tool has empty descriptor name")
		}
		if strings.TrimSpace(descriptor.Source) == "" {
			descriptor.Source = SourceInternal
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, nil
}

func (p *internalProvider) CallTool(ctx context.Context, req CallRequest) (CallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return errorResult("", ErrorCodeInvalidArguments, "tool name is required", false), nil
	}

	callable, ok := p.tools[name]
	if !ok {
		return errorResult(name, ErrorCodeNotFound, "tool not found", false), nil
	}

	result, err := callable.Call(ctx, normalizeArguments(req.Arguments))
	if err != nil {
		return mapCallFailure(name, err), nil
	}
	if strings.TrimSpace(result.Name) == "" {
		result.Name = name
	}
	normalizeCallResultError(&result)
	applyResultLimit(&result, p.maxResultChars)
	return result, nil
}

func (p *internalProvider) register(callable Callable) {
	if callable == nil {
		return
	}
	name := strings.TrimSpace(callable.Descriptor().Name)
	if name == "" {
		return
	}
	if _, exists := p.tools[name]; exists {
		return
	}
	p.tools[name] = callable
	p.toolNames = append(p.toolNames, name)
	sort.Strings(p.toolNames)
}

func normalizeArguments(arguments json.RawMessage) json.RawMessage {
	if len(arguments) == 0 {
		return json.RawMessage(`{}`)
	}
	trimmed := strings.TrimSpace(string(arguments))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	return arguments
}

func normalizeCallResultError(result *CallResult) {
	if result == nil {
		return
	}
	if result.Error != nil {
		result.IsError = true
		if strings.TrimSpace(result.Error.Message) == "" {
			result.Error.Message = "tool call failed"
		}
		return
	}
	if result.IsError {
		result.Error = &CallError{
			Code:      ErrorCodeInternal,
			Message:   "tool call failed",
			Retryable: false,
		}
	}
}

func classifyInternalStoreError(name string, err error, invalidCursor error) CallResult {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return mapCallFailure(name, err)
	case invalidCursor != nil && errors.Is(err, invalidCursor):
		return errorResult(name, ErrorCodeInvalidArguments, err.Error(), false)
	default:
		return errorResult(name, ErrorCodeInternal, err.Error(), false)
	}
}
