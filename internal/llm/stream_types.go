package llm

import "github.com/dongwlin/nekomimi/internal/llm/toolloop"

type StreamEventType string

const (
	StreamEventDelta      StreamEventType = "delta"
	StreamEventToolCall   StreamEventType = "tool_call"
	StreamEventToolResult StreamEventType = "tool_result"
	StreamEventFinal      StreamEventType = "final"
	StreamEventError      StreamEventType = "error"
)

type StreamEvent struct {
	Seq        int64
	Step       int
	Type       StreamEventType
	Delta      string
	ToolCall   *toolloop.ToolCallPayload
	ToolResult *toolloop.ToolResultPayload
	Final      *toolloop.FinalPayload
	Error      *toolloop.ErrorPayload
}

type StreamEventHandler func(event StreamEvent) error
