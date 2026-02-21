package toolloop

import (
	"context"
	"encoding/json"

	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

const ProtocolVersion = "v1"

// MessageType is the stable protocol message kind.
type MessageType string

const (
	MessageTypeToolCall   MessageType = "tool_call"
	MessageTypeToolResult MessageType = "tool_result"
	MessageTypeFinal      MessageType = "final"
	MessageTypeError      MessageType = "error"
)

// ErrorCode is the stable protocol error code set.
type ErrorCode string

const (
	ErrorCodeInvalidProtocol ErrorCode = "invalid_protocol"
	ErrorCodeUnknownTool     ErrorCode = "unknown_tool"
	ErrorCodeInvalidArgs     ErrorCode = "invalid_arguments"
	ErrorCodeToolCallFailed  ErrorCode = "tool_call_failed"
	ErrorCodeMaxSteps        ErrorCode = "max_steps_exceeded"
	ErrorCodeTimeout         ErrorCode = "timeout"
	ErrorCodeModelResponse   ErrorCode = "model_response_error"
	ErrorCodeInternal        ErrorCode = "internal_error"
)

// StopReason is the stable termination reason set.
type StopReason string

const (
	StopReasonFinal    StopReason = "final"
	StopReasonMaxSteps StopReason = "max_steps"
	StopReasonTimeout  StopReason = "timeout"
	StopReasonError    StopReason = "error"
)

// ToolCallPayload requests one tool execution.
type ToolCallPayload struct {
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResultPayload sends one tool execution result back to the model.
type ToolResultPayload struct {
	CallID string           `json:"call_id"`
	Name   string           `json:"name"`
	Result tools.CallResult `json:"result"`
}

// FinalPayload marks terminal assistant output.
type FinalPayload struct {
	Content    string     `json:"content"`
	StopReason StopReason `json:"stop_reason"`
}

// ErrorPayload marks terminal protocol/engine failure.
type ErrorPayload struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

// Message is the protocol frame shape frozen in package-0.
type Message struct {
	Version    string             `json:"version"`
	Type       MessageType        `json:"type"`
	ToolCall   *ToolCallPayload   `json:"tool_call,omitempty"`
	ToolResult *ToolResultPayload `json:"tool_result,omitempty"`
	Final      *FinalPayload      `json:"final,omitempty"`
	Error      *ErrorPayload      `json:"error,omitempty"`
}

// RunConfig controls loop safety limits.
type RunConfig struct {
	MaxSteps int
}

// RunRequest is the loop input contract.
type RunRequest struct {
	ModelName    string
	SystemPrompt string
	Messages     []model.Message
	Tools        []tools.Descriptor
	Config       RunConfig
}

// RunResult is the loop terminal contract.
type RunResult struct {
	FinalMessage string
	StopReason   StopReason
	Trace        []Message
}

// ModelDriver is the model-facing protocol participant.
type ModelDriver interface {
	Next(ctx context.Context, req RunRequest, trace []Message) (Message, error)
}

// Engine drives tool-loop state transitions until termination.
type Engine interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}
