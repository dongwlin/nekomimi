package toolloop

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

const ProtocolVersion = "v1"
const StreamProtocolVersion = "v2"

// MessageType is the stable protocol message kind.
type MessageType string

const (
	MessageTypeDelta      MessageType = "delta"
	MessageTypeToolCall   MessageType = "tool_call"
	MessageTypeToolResult MessageType = "tool_result"
	MessageTypeFinal      MessageType = "final"
	MessageTypeError      MessageType = "error"
)

type DeltaPayload struct {
	Text string `json:"text"`
}

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

func (e *ErrorPayload) Error() string {
	if e == nil {
		return ""
	}
	if text := strings.TrimSpace(e.Message); text != "" {
		return text
	}
	if code := strings.TrimSpace(string(e.Code)); code != "" {
		return code
	}
	return "protocol error"
}

func NewProtocolError(message string) *ErrorPayload {
	text := strings.TrimSpace(message)
	if text == "" {
		text = "invalid protocol response"
	}
	return &ErrorPayload{
		Code:      ErrorCodeInvalidProtocol,
		Message:   text,
		Retryable: false,
	}
}

// Message is the protocol frame shape frozen in package-0.
type Message struct {
	Version    string             `json:"version,omitempty"`
	Type       MessageType        `json:"type"`
	ToolCall   *ToolCallPayload   `json:"tool_call,omitempty"`
	ToolResult *ToolResultPayload `json:"tool_result,omitempty"`
	Final      *FinalPayload      `json:"final,omitempty"`
	Error      *ErrorPayload      `json:"error,omitempty"`
}

// StreamMessage is the v2 streaming frame shape.
type StreamMessage struct {
	Version    string             `json:"version,omitempty"`
	Type       MessageType        `json:"type"`
	Delta      *DeltaPayload      `json:"delta,omitempty"`
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

type StreamFrameHandler func(frame StreamMessage) error

// StreamModelDriver is the optional streaming participant for v2 protocol.
type StreamModelDriver interface {
	NextStream(ctx context.Context, req RunRequest, trace []Message, onFrame StreamFrameHandler) (Message, error)
}

type StreamEvent struct {
	Step  int
	Frame StreamMessage
}

type StreamEventHandler func(event StreamEvent) error

// Engine drives tool-loop state transitions until termination.
type Engine interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
	RunStream(ctx context.Context, req RunRequest, onEvent StreamEventHandler) (RunResult, error)
}
