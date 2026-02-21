package toolloop

import (
	"fmt"
	"strings"
)

func validateModelStreamFrame(frame StreamMessage) *ErrorPayload {
	if strings.TrimSpace(frame.Version) != StreamProtocolVersion {
		return &ErrorPayload{
			Code:      ErrorCodeInvalidProtocol,
			Message:   fmt.Sprintf("version must be %q", StreamProtocolVersion),
			Retryable: false,
		}
	}

	switch frame.Type {
	case MessageTypeDelta:
		if frame.Delta == nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "missing delta payload",
				Retryable: false,
			}
		}
		if frame.ToolCall != nil || frame.ToolResult != nil || frame.Final != nil || frame.Error != nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "delta must not include other payloads",
				Retryable: false,
			}
		}
		return nil
	case MessageTypeToolCall:
		if frame.ToolCall == nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "missing tool_call payload",
				Retryable: false,
			}
		}
		if frame.Delta != nil || frame.ToolResult != nil || frame.Final != nil || frame.Error != nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "tool_call must not include other payloads",
				Retryable: false,
			}
		}
		if strings.TrimSpace(frame.ToolCall.CallID) == "" {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "tool_call.call_id is required",
				Retryable: false,
			}
		}
		if strings.TrimSpace(frame.ToolCall.Name) == "" {
			return &ErrorPayload{
				Code:      ErrorCodeUnknownTool,
				Message:   "tool_call.name is required",
				Retryable: false,
			}
		}
		if !isJSONObject(frame.ToolCall.Arguments) {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidArgs,
				Message:   "tool_call.arguments must be a JSON object",
				Retryable: false,
			}
		}
		return nil
	case MessageTypeFinal:
		if frame.Final == nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "missing final payload",
				Retryable: false,
			}
		}
		if frame.Delta != nil || frame.ToolCall != nil || frame.ToolResult != nil || frame.Error != nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "final must not include other payloads",
				Retryable: false,
			}
		}
		if !validStopReason(frame.Final.StopReason) {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "invalid final.stop_reason",
				Retryable: false,
			}
		}
		return nil
	case MessageTypeError:
		if frame.Error == nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "missing error payload",
				Retryable: false,
			}
		}
		if frame.Delta != nil || frame.ToolCall != nil || frame.ToolResult != nil || frame.Final != nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "error must not include other payloads",
				Retryable: false,
			}
		}
		if !validErrorCode(frame.Error.Code) {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "invalid error.code",
				Retryable: false,
			}
		}
		if strings.TrimSpace(frame.Error.Message) == "" {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "error.message is required",
				Retryable: false,
			}
		}
		return nil
	case MessageTypeToolResult:
		return &ErrorPayload{
			Code:      ErrorCodeInvalidProtocol,
			Message:   "model output type tool_result is not allowed",
			Retryable: false,
		}
	default:
		return &ErrorPayload{
			Code:      ErrorCodeInvalidProtocol,
			Message:   fmt.Sprintf("unknown message type %q", frame.Type),
			Retryable: false,
		}
	}
}

func streamFrameToMessage(frame StreamMessage) (Message, bool) {
	switch frame.Type {
	case MessageTypeToolCall:
		return Message{
			Version:  ProtocolVersion,
			Type:     MessageTypeToolCall,
			ToolCall: cloneToolCallPayload(frame.ToolCall),
		}, true
	case MessageTypeFinal:
		return Message{
			Version: ProtocolVersion,
			Type:    MessageTypeFinal,
			Final:   cloneFinalPayload(frame.Final),
		}, true
	case MessageTypeError:
		return Message{
			Version: ProtocolVersion,
			Type:    MessageTypeError,
			Error:   cloneErrorPayload(frame.Error),
		}, true
	default:
		return Message{}, false
	}
}

func messageToStreamFrame(msg Message) StreamMessage {
	return StreamMessage{
		Version:    StreamProtocolVersion,
		Type:       msg.Type,
		ToolCall:   cloneToolCallPayload(msg.ToolCall),
		ToolResult: cloneToolResultPayload(msg.ToolResult),
		Final:      cloneFinalPayload(msg.Final),
		Error:      cloneErrorPayload(msg.Error),
	}
}

func cloneToolCallPayload(value *ToolCallPayload) *ToolCallPayload {
	if value == nil {
		return nil
	}
	return &ToolCallPayload{
		CallID:    strings.TrimSpace(value.CallID),
		Name:      strings.TrimSpace(value.Name),
		Arguments: cloneRawJSON(value.Arguments),
	}
}

func cloneToolResultPayload(value *ToolResultPayload) *ToolResultPayload {
	if value == nil {
		return nil
	}
	return &ToolResultPayload{
		CallID: strings.TrimSpace(value.CallID),
		Name:   strings.TrimSpace(value.Name),
		Result: value.Result,
	}
}

func cloneFinalPayload(value *FinalPayload) *FinalPayload {
	if value == nil {
		return nil
	}
	return &FinalPayload{
		Content:    value.Content,
		StopReason: value.StopReason,
	}
}

func cloneErrorPayload(value *ErrorPayload) *ErrorPayload {
	if value == nil {
		return nil
	}
	return &ErrorPayload{
		Code:      value.Code,
		Message:   value.Message,
		Retryable: value.Retryable,
	}
}
