package toolloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm/jsonutil"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

const defaultMaxSteps = 8

var (
	ErrRouterRequired = errors.New("tool router is required")
	ErrDriverRequired = errors.New("model driver is required")

	errStreamEventCallback = errors.New("stream event callback failed")
)

// EngineOptions controls default behavior for the loop engine.
type EngineOptions struct {
	DefaultMaxSteps int
}

type loopEngine struct {
	router          tools.Router
	driver          ModelDriver
	defaultMaxSteps int
}

// NewEngine builds a protocol engine that drives model<->tool loop transitions.
func NewEngine(router tools.Router, driver ModelDriver, opts EngineOptions) Engine {
	defaultSteps := opts.DefaultMaxSteps
	if defaultSteps <= 0 {
		defaultSteps = defaultMaxSteps
	}
	return &loopEngine{
		router:          router,
		driver:          driver,
		defaultMaxSteps: defaultSteps,
	}
}

func (e *loopEngine) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if e.router == nil {
		return RunResult{}, ErrRouterRequired
	}
	if e.driver == nil {
		return RunResult{}, ErrDriverRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}

	maxSteps := req.Config.MaxSteps
	if maxSteps <= 0 {
		maxSteps = e.defaultMaxSteps
	}
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}

	trace := make([]Message, 0, maxSteps*2+1)
	for step := 0; step < maxSteps; step++ {
		if isTimeout(ctx.Err()) {
			return finalize(trace, StopReasonTimeout), nil
		}

		next, err := e.driver.Next(ctx, req, copyTrace(trace))
		if err != nil {
			if isTimeout(err) || isTimeout(ctx.Err()) {
				return finalize(trace, StopReasonTimeout), nil
			}
			trace = append(trace, errorMessage(ErrorCodeModelResponse, err.Error(), false))
			continue
		}

		if protocolErr := validateModelMessage(next); protocolErr != nil {
			trace = append(trace, Message{
				Version: ProtocolVersion,
				Type:    MessageTypeError,
				Error:   protocolErr,
			})
			continue
		}

		next.Version = ProtocolVersion
		trace = append(trace, next)

		switch next.Type {
		case MessageTypeToolCall:
			toolResult, callErr := e.executeTool(ctx, *next.ToolCall)
			if callErr != nil {
				trace = append(trace, Message{
					Version: ProtocolVersion,
					Type:    MessageTypeError,
					Error:   callErr,
				})
				return RunResult{
					StopReason: StopReasonError,
					Trace:      trace,
				}, nil
			}
			trace = append(trace, toolResult)
		case MessageTypeFinal:
			return RunResult{
				FinalMessage: next.Final.Content,
				StopReason:   next.Final.StopReason,
				Trace:        trace,
			}, nil
		case MessageTypeError:
			return RunResult{
				StopReason: StopReasonError,
				Trace:      trace,
			}, nil
		default:
			trace = append(trace, errorMessage(
				ErrorCodeInvalidProtocol,
				fmt.Sprintf("unsupported model message type %q", next.Type),
				false,
			))
			return RunResult{
				StopReason: StopReasonError,
				Trace:      trace,
			}, nil
		}
	}

	return finalize(trace, StopReasonMaxSteps), nil
}

func (e *loopEngine) RunStream(ctx context.Context, req RunRequest, onEvent StreamEventHandler) (RunResult, error) {
	if e.router == nil {
		return RunResult{}, ErrRouterRequired
	}
	if e.driver == nil {
		return RunResult{}, ErrDriverRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}

	streamDriver, ok := e.driver.(StreamModelDriver)
	if !ok {
		result, err := e.Run(ctx, req)
		if err != nil {
			return result, err
		}
		if onEvent != nil {
			if strings.TrimSpace(result.FinalMessage) != "" {
				if err := onEvent(StreamEvent{
					Step: 0,
					Frame: StreamMessage{
						Version: StreamProtocolVersion,
						Type:    MessageTypeDelta,
						Delta: &DeltaPayload{
							Text: result.FinalMessage,
						},
					},
				}); err != nil {
					return RunResult{}, fmt.Errorf("%w: %v", errStreamEventCallback, err)
				}
			}
			if err := onEvent(StreamEvent{
				Step: 0,
				Frame: StreamMessage{
					Version: StreamProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    result.FinalMessage,
						StopReason: result.StopReason,
					},
				},
			}); err != nil {
				return RunResult{}, fmt.Errorf("%w: %v", errStreamEventCallback, err)
			}
		}
		return result, nil
	}

	maxSteps := req.Config.MaxSteps
	if maxSteps <= 0 {
		maxSteps = e.defaultMaxSteps
	}
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}

	trace := make([]Message, 0, maxSteps*2+1)
	for step := 0; step < maxSteps; step++ {
		if isTimeout(ctx.Err()) {
			result := finalize(trace, StopReasonTimeout)
			if err := emitSafetyFinalEvent(onEvent, step, StopReasonTimeout); err != nil {
				return RunResult{}, err
			}
			return result, nil
		}

		next, err := streamDriver.NextStream(ctx, req, copyTrace(trace), func(frame StreamMessage) error {
			if frame.Type != MessageTypeDelta {
				return errors.New("stream driver emitted non-delta frame")
			}
			if frame.Delta == nil {
				return errors.New("stream delta payload is required")
			}
			if onEvent == nil {
				return nil
			}
			if err := onEvent(StreamEvent{Step: step, Frame: frame}); err != nil {
				return fmt.Errorf("%w: %v", errStreamEventCallback, err)
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errStreamEventCallback) {
				return RunResult{}, err
			}
			if isTimeout(err) || isTimeout(ctx.Err()) {
				result := finalize(trace, StopReasonTimeout)
				if emitErr := emitSafetyFinalEvent(onEvent, step, StopReasonTimeout); emitErr != nil {
					return RunResult{}, emitErr
				}
				return result, nil
			}
			trace = append(trace, errorMessage(ErrorCodeModelResponse, err.Error(), false))
			errorFrame := messageToStreamFrame(trace[len(trace)-1])
			if emitErr := emitEvent(onEvent, step, errorFrame); emitErr != nil {
				return RunResult{}, emitErr
			}
			continue
		}

		if protocolErr := validateModelMessage(next); protocolErr != nil {
			message := Message{
				Version: ProtocolVersion,
				Type:    MessageTypeError,
				Error:   protocolErr,
			}
			trace = append(trace, message)
			if emitErr := emitEvent(onEvent, step, messageToStreamFrame(message)); emitErr != nil {
				return RunResult{}, emitErr
			}
			continue
		}

		next.Version = ProtocolVersion
		trace = append(trace, next)
		if emitErr := emitEvent(onEvent, step, messageToStreamFrame(next)); emitErr != nil {
			return RunResult{}, emitErr
		}

		switch next.Type {
		case MessageTypeToolCall:
			toolResult, callErr := e.executeTool(ctx, *next.ToolCall)
			if callErr != nil {
				message := Message{
					Version: ProtocolVersion,
					Type:    MessageTypeError,
					Error:   callErr,
				}
				trace = append(trace, message)
				if emitErr := emitEvent(onEvent, step, messageToStreamFrame(message)); emitErr != nil {
					return RunResult{}, emitErr
				}
				return RunResult{
					StopReason: StopReasonError,
					Trace:      trace,
				}, nil
			}
			trace = append(trace, toolResult)
			if emitErr := emitEvent(onEvent, step, messageToStreamFrame(toolResult)); emitErr != nil {
				return RunResult{}, emitErr
			}
		case MessageTypeFinal:
			return RunResult{
				FinalMessage: next.Final.Content,
				StopReason:   next.Final.StopReason,
				Trace:        trace,
			}, nil
		case MessageTypeError:
			return RunResult{
				StopReason: StopReasonError,
				Trace:      trace,
			}, nil
		default:
			message := errorMessage(
				ErrorCodeInvalidProtocol,
				fmt.Sprintf("unsupported model message type %q", next.Type),
				false,
			)
			trace = append(trace, message)
			if emitErr := emitEvent(onEvent, step, messageToStreamFrame(message)); emitErr != nil {
				return RunResult{}, emitErr
			}
			return RunResult{
				StopReason: StopReasonError,
				Trace:      trace,
			}, nil
		}
	}

	result := finalize(trace, StopReasonMaxSteps)
	if err := emitSafetyFinalEvent(onEvent, maxSteps, StopReasonMaxSteps); err != nil {
		return RunResult{}, err
	}
	return result, nil
}

func (e *loopEngine) executeTool(ctx context.Context, call ToolCallPayload) (Message, *ErrorPayload) {
	name := strings.TrimSpace(call.Name)
	result, err := e.router.CallTool(ctx, tools.CallRequest{
		Name:      name,
		Arguments: jsonutil.CloneRawMessage(call.Arguments),
	})
	if err != nil {
		if isTimeout(err) || isTimeout(ctx.Err()) {
			return Message{}, &ErrorPayload{
				Code:      ErrorCodeTimeout,
				Message:   "tool call timeout",
				Retryable: true,
			}
		}
		return Message{}, &ErrorPayload{
			Code:      ErrorCodeToolCallFailed,
			Message:   strings.TrimSpace(err.Error()),
			Retryable: false,
		}
	}
	if strings.TrimSpace(result.Name) == "" {
		result.Name = name
	}
	if result.IsError && result.Error == nil {
		result.Error = &tools.CallError{
			Code:      tools.ErrorCodeInternal,
			Message:   "tool call failed",
			Retryable: false,
		}
	}

	return Message{
		Version: ProtocolVersion,
		Type:    MessageTypeToolResult,
		ToolResult: &ToolResultPayload{
			CallID: strings.TrimSpace(call.CallID),
			Name:   name,
			Result: result,
		},
	}, nil
}

func validateModelMessage(msg Message) *ErrorPayload {
	if strings.TrimSpace(msg.Version) != ProtocolVersion {
		return &ErrorPayload{
			Code:      ErrorCodeInvalidProtocol,
			Message:   fmt.Sprintf("version must be %q", ProtocolVersion),
			Retryable: false,
		}
	}

	switch msg.Type {
	case MessageTypeToolCall:
		if msg.ToolCall == nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "missing tool_call payload",
				Retryable: false,
			}
		}
		if msg.ToolResult != nil || msg.Final != nil || msg.Error != nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "tool_call must not include other payloads",
				Retryable: false,
			}
		}
		if strings.TrimSpace(msg.ToolCall.CallID) == "" {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "tool_call.call_id is required",
				Retryable: false,
			}
		}
		if strings.TrimSpace(msg.ToolCall.Name) == "" {
			return &ErrorPayload{
				Code:      ErrorCodeUnknownTool,
				Message:   "tool_call.name is required",
				Retryable: false,
			}
		}
		if !isJSONObject(msg.ToolCall.Arguments) {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidArgs,
				Message:   "tool_call.arguments must be a JSON object",
				Retryable: false,
			}
		}
		return nil
	case MessageTypeFinal:
		if msg.Final == nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "missing final payload",
				Retryable: false,
			}
		}
		if msg.ToolCall != nil || msg.ToolResult != nil || msg.Error != nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "final must not include other payloads",
				Retryable: false,
			}
		}
		if !validStopReason(msg.Final.StopReason) {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "invalid final.stop_reason",
				Retryable: false,
			}
		}
		return nil
	case MessageTypeError:
		if msg.Error == nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "missing error payload",
				Retryable: false,
			}
		}
		if msg.ToolCall != nil || msg.ToolResult != nil || msg.Final != nil {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "error must not include other payloads",
				Retryable: false,
			}
		}
		if !validErrorCode(msg.Error.Code) {
			return &ErrorPayload{
				Code:      ErrorCodeInvalidProtocol,
				Message:   "invalid error.code",
				Retryable: false,
			}
		}
		if strings.TrimSpace(msg.Error.Message) == "" {
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
			Message:   fmt.Sprintf("unknown message type %q", msg.Type),
			Retryable: false,
		}
	}
}

func errorMessage(code ErrorCode, message string, retryable bool) Message {
	text := strings.TrimSpace(message)
	if text == "" {
		text = "loop error"
	}
	return Message{
		Version: ProtocolVersion,
		Type:    MessageTypeError,
		Error: &ErrorPayload{
			Code:      code,
			Message:   text,
			Retryable: retryable,
		},
	}
}

func finalize(trace []Message, reason StopReason) RunResult {
	resultTrace := append(trace, Message{
		Version: ProtocolVersion,
		Type:    MessageTypeFinal,
		Final: &FinalPayload{
			Content:    "",
			StopReason: reason,
		},
	})
	return RunResult{
		FinalMessage: "",
		StopReason:   reason,
		Trace:        resultTrace,
	}
}

func validStopReason(reason StopReason) bool {
	switch reason {
	case StopReasonFinal, StopReasonMaxSteps, StopReasonTimeout, StopReasonError:
		return true
	default:
		return false
	}
}

func validErrorCode(code ErrorCode) bool {
	switch code {
	case ErrorCodeInvalidProtocol,
		ErrorCodeUnknownTool,
		ErrorCodeInvalidArgs,
		ErrorCodeToolCallFailed,
		ErrorCodeMaxSteps,
		ErrorCodeTimeout,
		ErrorCodeModelResponse,
		ErrorCodeInternal:
		return true
	default:
		return false
	}
}

func isJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false
	}
	_, ok := decoded.(map[string]any)
	return ok
}

func copyTrace(trace []Message) []Message {
	if len(trace) == 0 {
		return nil
	}
	cloned := make([]Message, len(trace))
	copy(cloned, trace)
	return cloned
}

func isTimeout(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func emitSafetyFinalEvent(onEvent StreamEventHandler, step int, reason StopReason) error {
	if onEvent == nil {
		return nil
	}
	return onEvent(StreamEvent{
		Step: step,
		Frame: StreamMessage{
			Version: StreamProtocolVersion,
			Type:    MessageTypeFinal,
			Final: &FinalPayload{
				Content:    "",
				StopReason: reason,
			},
		},
	})
}

func emitEvent(onEvent StreamEventHandler, step int, frame StreamMessage) error {
	if onEvent == nil {
		return nil
	}
	if err := onEvent(StreamEvent{
		Step:  step,
		Frame: frame,
	}); err != nil {
		return fmt.Errorf("%w: %v", errStreamEventCallback, err)
	}
	return nil
}
