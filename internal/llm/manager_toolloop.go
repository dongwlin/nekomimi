package llm

import (
	"errors"
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm/toolloop"
)

func finalizeToolLoopResult(result toolloop.RunResult) (string, error) {
	reply := strings.TrimSpace(result.FinalMessage)
	switch result.StopReason {
	case toolloop.StopReasonFinal:
		if reply == "" {
			return "", errors.New("model returned empty content")
		}
		return reply, nil
	case toolloop.StopReasonTimeout:
		return "", errors.New("request timed out")
	case toolloop.StopReasonMaxSteps:
		return "", errors.New("tool call exceeded max steps")
	case toolloop.StopReasonError:
		if msg := protocolErrorMessage(result.Trace); msg != "" {
			return "", errors.New(msg)
		}
		return "", errors.New("invalid tool-loop protocol response")
	default:
		if reply != "" {
			return reply, nil
		}
		return "", errors.New("model request failed")
	}
}

func protocolErrorMessage(trace []toolloop.Message) string {
	for i := len(trace) - 1; i >= 0; i-- {
		entry := trace[i]
		if entry.Type != toolloop.MessageTypeError || entry.Error == nil {
			continue
		}
		if text := strings.TrimSpace(entry.Error.Message); text != "" {
			return text
		}
	}
	return ""
}

func mapToolLoopStreamEvent(seq int64, step int, message toolloop.StreamMessage) StreamEvent {
	event := StreamEvent{
		Seq:  seq,
		Step: step,
	}
	switch message.Type {
	case toolloop.MessageTypeDelta:
		event.Type = StreamEventDelta
		if message.Delta != nil {
			event.Delta = message.Delta.Text
		}
	case toolloop.MessageTypeToolCall:
		event.Type = StreamEventToolCall
		event.ToolCall = message.ToolCall
	case toolloop.MessageTypeToolResult:
		event.Type = StreamEventToolResult
		event.ToolResult = message.ToolResult
	case toolloop.MessageTypeFinal:
		event.Type = StreamEventFinal
		event.Final = message.Final
	case toolloop.MessageTypeError:
		event.Type = StreamEventError
		event.Error = message.Error
	default:
		event.Type = StreamEventError
		event.Error = &toolloop.ErrorPayload{
			Code:      toolloop.ErrorCodeInvalidProtocol,
			Message:   "unsupported stream event type",
			Retryable: false,
		}
	}
	return event
}
