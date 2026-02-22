package tools

import (
	"context"
	"encoding/json"
)

const (
	SourceInternal = "internal"
	SourceMCP      = "mcp"
)

// ErrorCode is a stable error code set returned by tool providers.
type ErrorCode string

const (
	ErrorCodeInvalidArguments ErrorCode = "invalid_arguments"
	ErrorCodeNotFound         ErrorCode = "not_found"
	ErrorCodeTimeout          ErrorCode = "timeout"
	ErrorCodeUnavailable      ErrorCode = "unavailable"
	ErrorCodeInternal         ErrorCode = "internal"
)

// Descriptor describes one callable tool.
type Descriptor struct {
	Name         string
	Description  string
	Source       string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
}

// CallRequest is a tool invocation request.
type CallRequest struct {
	Name      string
	Arguments json.RawMessage
}

// CallError is an invocation error rendered to the model and diagnostics.
type CallError struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

// CallResult is a normalized invocation result.
type CallResult struct {
	Name       string
	Content    string
	Structured json.RawMessage
	IsError    bool
	Error      *CallError
}

// Callable is the contract for an individual tool implementation.
type Callable interface {
	Descriptor() Descriptor
	Call(ctx context.Context, arguments json.RawMessage) (CallResult, error)
}

// Provider is a collection of tools with a shared discovery/call path.
type Provider interface {
	ListTools(ctx context.Context) ([]Descriptor, error)
	CallTool(ctx context.Context, req CallRequest) (CallResult, error)
}

// Router is the unified tool bus across internal and external providers.
type Router interface {
	Register(providerName string, provider Provider) error
	ListTools(ctx context.Context) ([]Descriptor, error)
	CallTool(ctx context.Context, req CallRequest) (CallResult, error)
}
