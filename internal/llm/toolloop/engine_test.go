package toolloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

func TestEngine_Run_ToolCallToFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		args := decodeObjectMap(t, req.Arguments)
		text, _ := args["text"].(string)
		return tools.CallResult{
			Name:    req.Name,
			Content: "echo: " + text,
		}, nil
	})

	driver := &scriptedDriver{
		t: t,
		steps: []driverStep{
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeToolCall,
					ToolCall: &ToolCallPayload{
						CallID:    "call_001",
						Name:      "internal/echo",
						Arguments: mustRawJSON(t, map[string]any{"text": "hello"}),
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 0 {
						t.Fatalf("first model step should see empty trace, got %d messages", len(trace))
					}
				},
			},
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "all done",
						StopReason: StopReasonFinal,
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 2 {
						t.Fatalf("second model step should see tool_call+tool_result, got %d messages", len(trace))
					}
					if trace[0].Type != MessageTypeToolCall {
						t.Fatalf("trace[0] type mismatch: got %q", trace[0].Type)
					}
					if trace[1].Type != MessageTypeToolResult {
						t.Fatalf("trace[1] type mismatch: got %q", trace[1].Type)
					}
					if trace[1].ToolResult == nil {
						t.Fatalf("trace[1] missing tool_result payload")
					}
					if trace[1].ToolResult.Result.IsError {
						t.Fatalf("tool result should be success, got error: %+v", trace[1].ToolResult.Result.Error)
					}
					if trace[1].ToolResult.Result.Content != "echo: hello" {
						t.Fatalf("unexpected tool result content: %q", trace[1].ToolResult.Result.Content)
					}
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	result, err := engine.Run(context.Background(), RunRequest{
		ModelName:    "test-model",
		SystemPrompt: "test",
		Messages: []model.Message{
			{Role: "user", Content: "hi"},
		},
		Config: RunConfig{MaxSteps: 4},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "all done" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
	if len(result.Trace) != 3 {
		t.Fatalf("trace length mismatch: got %d, want 3", len(result.Trace))
	}
	if result.Trace[2].Type != MessageTypeFinal {
		t.Fatalf("final trace type mismatch: got %q", result.Trace[2].Type)
	}
}

func TestEngine_Run_MaxStepsSafetyExit(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{
			Name:    req.Name,
			Content: "ok",
		}, nil
	})
	driver := &repeatToolCallDriver{
		msg: Message{
			Version: ProtocolVersion,
			Type:    MessageTypeToolCall,
			ToolCall: &ToolCallPayload{
				CallID:    "call_loop",
				Name:      "internal/echo",
				Arguments: mustRawJSON(t, map[string]any{}),
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	result, err := engine.Run(context.Background(), RunRequest{
		Config: RunConfig{MaxSteps: 1},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != StopReasonMaxSteps {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonMaxSteps)
	}
	if len(result.Trace) != 3 {
		t.Fatalf("trace length mismatch: got %d, want 3", len(result.Trace))
	}
	last := result.Trace[len(result.Trace)-1]
	if last.Type != MessageTypeFinal || last.Final == nil {
		t.Fatalf("last trace should be final safety message")
	}
	if last.Final.StopReason != StopReasonMaxSteps {
		t.Fatalf("safety final reason mismatch: got %q", last.Final.StopReason)
	}
	if driver.calls != 1 {
		t.Fatalf("driver should be called once, got %d", driver.calls)
	}
}

func TestEngine_Run_InvalidProtocolInjectedThenFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedDriver{
		t: t,
		steps: []driverStep{
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeToolCall,
				},
			},
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "recovered",
						StopReason: StopReasonFinal,
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 1 {
						t.Fatalf("second model step should see one injected error, got %d", len(trace))
					}
					if trace[0].Type != MessageTypeError || trace[0].Error == nil {
						t.Fatalf("expected injected protocol error in trace")
					}
					if trace[0].Error.Code != ErrorCodeInvalidProtocol {
						t.Fatalf("error code mismatch: got %q, want %q", trace[0].Error.Code, ErrorCodeInvalidProtocol)
					}
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	result, err := engine.Run(context.Background(), RunRequest{Config: RunConfig{MaxSteps: 3}})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "recovered" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
	if len(result.Trace) != 2 {
		t.Fatalf("trace length mismatch: got %d, want 2", len(result.Trace))
	}
	if result.Trace[0].Type != MessageTypeError || result.Trace[0].Error == nil {
		t.Fatalf("expected injected protocol error in trace")
	}
	if result.Trace[1].Type != MessageTypeFinal {
		t.Fatalf("expected final message in trace tail, got %q", result.Trace[1].Type)
	}
}

func TestEngine_Run_ModelFinalStopReasonMustBeFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedDriver{
		t: t,
		steps: []driverStep{
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "spoofed timeout",
						StopReason: StopReasonTimeout,
					},
				},
			},
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "recovered",
						StopReason: StopReasonFinal,
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 1 {
						t.Fatalf("second model step should see one injected error, got %d", len(trace))
					}
					if trace[0].Type != MessageTypeError || trace[0].Error == nil {
						t.Fatalf("expected injected protocol error in trace")
					}
					if trace[0].Error.Message != "invalid final.stop_reason" {
						t.Fatalf("unexpected error message: %q", trace[0].Error.Message)
					}
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	result, err := engine.Run(context.Background(), RunRequest{Config: RunConfig{MaxSteps: 3}})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "recovered" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
}

func TestEngine_Run_ProtocolErrorStopsAfterThreeAttempts(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedDriver{
		t: t,
		steps: []driverStep{
			{msg: Message{Type: MessageTypeToolCall}},
			{msg: Message{Type: MessageTypeToolCall}},
			{msg: Message{Type: MessageTypeToolCall}},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	result, err := engine.Run(context.Background(), RunRequest{Config: RunConfig{MaxSteps: 4}})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != StopReasonError {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonError)
	}
	if len(driver.calls) != 3 {
		t.Fatalf("driver should be called three times, got %d", len(driver.calls))
	}
	if len(result.Trace) != 3 {
		t.Fatalf("trace length mismatch: got %d, want 3", len(result.Trace))
	}
	last := result.Trace[len(result.Trace)-1]
	if last.Type != MessageTypeError || last.Error == nil {
		t.Fatalf("expected protocol error tail, got %+v", last)
	}
	if !strings.Contains(last.Error.Message, "protocol retry limit exceeded") {
		t.Fatalf("unexpected final error message: %q", last.Error.Message)
	}
}

func TestEngine_Run_ContextTimeoutTerminatesSafely(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedDriver{
		t: t,
		steps: []driverStep{
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "should not happen",
						StopReason: StopReasonFinal,
					},
				},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	engine := NewEngine(router, driver, EngineOptions{})
	result, err := engine.Run(ctx, RunRequest{Config: RunConfig{MaxSteps: 3}})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != StopReasonTimeout {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonTimeout)
	}
	if len(result.Trace) != 1 {
		t.Fatalf("trace length mismatch: got %d, want 1", len(result.Trace))
	}
	if result.Trace[0].Type != MessageTypeFinal || result.Trace[0].Final == nil {
		t.Fatalf("expected timeout final frame")
	}
	if result.Trace[0].Final.StopReason != StopReasonTimeout {
		t.Fatalf("timeout final reason mismatch: got %q", result.Trace[0].Final.StopReason)
	}
	if len(driver.calls) != 0 {
		t.Fatalf("driver should not be called when ctx already canceled")
	}
}

func TestEngine_Run_ToolErrorInjectionThenFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{
			Name:    req.Name,
			IsError: true,
			Error: &tools.CallError{
				Code:      tools.ErrorCodeInvalidArguments,
				Message:   "missing text",
				Retryable: false,
			},
		}, nil
	})
	driver := &scriptedDriver{
		t: t,
		steps: []driverStep{
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeToolCall,
					ToolCall: &ToolCallPayload{
						CallID:    "call_bad",
						Name:      "internal/echo",
						Arguments: mustRawJSON(t, map[string]any{}),
					},
				},
			},
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "fallback answer",
						StopReason: StopReasonFinal,
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 2 {
						t.Fatalf("expected tool_call+tool_result, got %d", len(trace))
					}
					toolResult := trace[1].ToolResult
					if toolResult == nil {
						t.Fatalf("missing tool_result payload")
					}
					if !toolResult.Result.IsError {
						t.Fatalf("tool error should be injected as tool_result")
					}
					if toolResult.Result.Error == nil || toolResult.Result.Error.Code != tools.ErrorCodeInvalidArguments {
						t.Fatalf("unexpected tool error payload: %+v", toolResult.Result.Error)
					}
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	result, err := engine.Run(context.Background(), RunRequest{Config: RunConfig{MaxSteps: 4}})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "fallback answer" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
}

func TestEngine_Run_ModelDriverErrorInjectedThenFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedDriver{
		t: t,
		steps: []driverStep{
			{
				err: errors.New("model unavailable"),
			},
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "recovered",
						StopReason: StopReasonFinal,
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 1 {
						t.Fatalf("second model step should see one injected error, got %d", len(trace))
					}
					if trace[0].Type != MessageTypeError || trace[0].Error == nil {
						t.Fatalf("expected injected model error in trace")
					}
					if trace[0].Error.Code != ErrorCodeModelResponse {
						t.Fatalf("error code mismatch: got %q, want %q", trace[0].Error.Code, ErrorCodeModelResponse)
					}
					if !strings.Contains(trace[0].Error.Message, "model unavailable") {
						t.Fatalf("error message should include driver error detail, got %q", trace[0].Error.Message)
					}
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	result, err := engine.Run(context.Background(), RunRequest{Config: RunConfig{MaxSteps: 3}})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "recovered" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
	if len(result.Trace) != 2 {
		t.Fatalf("trace length mismatch: got %d, want 2", len(result.Trace))
	}
	if result.Trace[0].Type != MessageTypeError || result.Trace[0].Error == nil {
		t.Fatalf("expected injected model error frame")
	}
	if result.Trace[1].Type != MessageTypeFinal {
		t.Fatalf("expected final frame in trace tail, got %q", result.Trace[1].Type)
	}
}

func TestEngine_RunStream_DeltaAndFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedStreamDriver{
		t: t,
		steps: []streamDriverStep{
			{
				frames: []StreamMessage{
					{
						Version: StreamProtocolVersion,
						Type:    MessageTypeDelta,
						Delta:   &DeltaPayload{Text: "你"},
					},
					{
						Version: StreamProtocolVersion,
						Type:    MessageTypeDelta,
						Delta:   &DeltaPayload{Text: "好"},
					},
				},
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "你好",
						StopReason: StopReasonFinal,
					},
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	events := make([]StreamEvent, 0, 4)
	result, err := engine.RunStream(context.Background(), RunRequest{
		Config: RunConfig{MaxSteps: 3},
	}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("run stream failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "你好" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
	if len(events) != 3 {
		t.Fatalf("event count mismatch: got %d, want 3", len(events))
	}
	if events[0].Frame.Type != MessageTypeDelta || events[1].Frame.Type != MessageTypeDelta || events[2].Frame.Type != MessageTypeFinal {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
}

func TestEngine_RunStream_ToolCallToolResultFinalSequence(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{
			Name:    req.Name,
			Content: "echo: hello",
		}, nil
	})
	driver := &scriptedStreamDriver{
		t: t,
		steps: []streamDriverStep{
			{
				frames: []StreamMessage{
					{
						Version: StreamProtocolVersion,
						Type:    MessageTypeDelta,
						Delta:   &DeltaPayload{Text: "thinking"},
					},
				},
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeToolCall,
					ToolCall: &ToolCallPayload{
						CallID:    "c1",
						Name:      "internal/echo",
						Arguments: mustRawJSON(t, map[string]any{"text": "hello"}),
					},
				},
			},
			{
				frames: []StreamMessage{
					{
						Version: StreamProtocolVersion,
						Type:    MessageTypeDelta,
						Delta:   &DeltaPayload{Text: "done"},
					},
				},
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "done",
						StopReason: StopReasonFinal,
					},
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	events := make([]StreamEvent, 0, 8)
	result, err := engine.RunStream(context.Background(), RunRequest{
		Config: RunConfig{MaxSteps: 4},
	}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("run stream failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if len(events) < 5 {
		t.Fatalf("event count should be >= 5, got %d", len(events))
	}
	if events[0].Frame.Type != MessageTypeDelta {
		t.Fatalf("events[0] should be delta, got %q", events[0].Frame.Type)
	}
	if events[1].Frame.Type != MessageTypeToolCall {
		t.Fatalf("events[1] should be tool_call, got %q", events[1].Frame.Type)
	}
	if events[2].Frame.Type != MessageTypeToolResult {
		t.Fatalf("events[2] should be tool_result, got %q", events[2].Frame.Type)
	}
	if events[3].Frame.Type != MessageTypeDelta {
		t.Fatalf("events[3] should be delta, got %q", events[3].Frame.Type)
	}
	if events[4].Frame.Type != MessageTypeFinal {
		t.Fatalf("events[4] should be final, got %q", events[4].Frame.Type)
	}
}

func TestEngine_RunStream_CallbackError(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedStreamDriver{
		t: t,
		steps: []streamDriverStep{
			{
				frames: []StreamMessage{
					{
						Version: StreamProtocolVersion,
						Type:    MessageTypeDelta,
						Delta:   &DeltaPayload{Text: "x"},
					},
				},
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "x",
						StopReason: StopReasonFinal,
					},
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	_, err := engine.RunStream(context.Background(), RunRequest{Config: RunConfig{MaxSteps: 2}}, func(event StreamEvent) error {
		return errors.New("event failed")
	})
	if err == nil {
		t.Fatal("expected callback error, got nil")
	}
	if !strings.Contains(err.Error(), "event failed") {
		t.Fatalf("callback error detail missing: %v", err)
	}
}

func TestEngine_RunStream_InvalidProtocolInjectedThenFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedStreamDriver{
		t: t,
		steps: []streamDriverStep{
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeToolCall,
				},
			},
			{
				frames: []StreamMessage{
					{
						Version: StreamProtocolVersion,
						Type:    MessageTypeDelta,
						Delta:   &DeltaPayload{Text: "done"},
					},
				},
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "done",
						StopReason: StopReasonFinal,
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 1 {
						t.Fatalf("second model step should see one injected error, got %d", len(trace))
					}
					if trace[0].Type != MessageTypeError || trace[0].Error == nil {
						t.Fatalf("expected injected protocol error in trace")
					}
					if trace[0].Error.Code != ErrorCodeInvalidProtocol {
						t.Fatalf("error code mismatch: got %q, want %q", trace[0].Error.Code, ErrorCodeInvalidProtocol)
					}
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	events := make([]StreamEvent, 0, 4)
	result, err := engine.RunStream(context.Background(), RunRequest{
		Config: RunConfig{MaxSteps: 3},
	}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("run stream failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "done" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
	if len(events) != 3 {
		t.Fatalf("event count mismatch: got %d, want 3", len(events))
	}
	if events[0].Frame.Type != MessageTypeError {
		t.Fatalf("events[0] should be error, got %q", events[0].Frame.Type)
	}
	if events[1].Frame.Type != MessageTypeDelta {
		t.Fatalf("events[1] should be delta, got %q", events[1].Frame.Type)
	}
	if events[2].Frame.Type != MessageTypeFinal {
		t.Fatalf("events[2] should be final, got %q", events[2].Frame.Type)
	}
}

func TestEngine_RunStream_ModelFinalStopReasonMustBeFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedStreamDriver{
		t: t,
		steps: []streamDriverStep{
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "spoofed timeout",
						StopReason: StopReasonTimeout,
					},
				},
			},
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "recovered",
						StopReason: StopReasonFinal,
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 1 {
						t.Fatalf("second model step should see one injected error, got %d", len(trace))
					}
					if trace[0].Type != MessageTypeError || trace[0].Error == nil {
						t.Fatalf("expected injected protocol error in trace")
					}
					if trace[0].Error.Message != "invalid final.stop_reason" {
						t.Fatalf("unexpected error message: %q", trace[0].Error.Message)
					}
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	events := make([]StreamEvent, 0, 3)
	result, err := engine.RunStream(context.Background(), RunRequest{
		Config: RunConfig{MaxSteps: 3},
	}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("run stream failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "recovered" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
	if len(events) != 2 {
		t.Fatalf("event count mismatch: got %d, want 2", len(events))
	}
	if events[0].Frame.Type != MessageTypeError {
		t.Fatalf("events[0] should be error, got %q", events[0].Frame.Type)
	}
	if events[1].Frame.Type != MessageTypeFinal {
		t.Fatalf("events[1] should be final, got %q", events[1].Frame.Type)
	}
}

func TestEngine_RunStream_ProtocolErrorStopsAfterThreeAttempts(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedStreamDriver{
		t: t,
		steps: []streamDriverStep{
			{msg: Message{Type: MessageTypeToolCall}},
			{msg: Message{Type: MessageTypeToolCall}},
			{msg: Message{Type: MessageTypeToolCall}},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	events := make([]StreamEvent, 0, 4)
	result, err := engine.RunStream(context.Background(), RunRequest{
		Config: RunConfig{MaxSteps: 4},
	}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("run stream failed: %v", err)
	}
	if result.StopReason != StopReasonError {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonError)
	}
	if len(events) != 3 {
		t.Fatalf("event count mismatch: got %d, want 3", len(events))
	}
	for i, event := range events {
		if event.Frame.Type != MessageTypeError {
			t.Fatalf("events[%d] should be error, got %q", i, event.Frame.Type)
		}
	}
	if events[2].Frame.Error == nil || !strings.Contains(events[2].Frame.Error.Message, "protocol retry limit exceeded") {
		t.Fatalf("unexpected final error event: %+v", events[2].Frame.Error)
	}
}

func TestEngine_RunStream_ModelDriverErrorInjectedThenFinal(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedStreamDriver{
		t: t,
		steps: []streamDriverStep{
			{
				err: errors.New("stream parser failed"),
			},
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeFinal,
					Final: &FinalPayload{
						Content:    "recovered",
						StopReason: StopReasonFinal,
					},
				},
				assertTrace: func(t *testing.T, trace []Message) {
					if len(trace) != 1 {
						t.Fatalf("second model step should see one injected error, got %d", len(trace))
					}
					if trace[0].Type != MessageTypeError || trace[0].Error == nil {
						t.Fatalf("expected injected model error in trace")
					}
					if trace[0].Error.Code != ErrorCodeModelResponse {
						t.Fatalf("error code mismatch: got %q, want %q", trace[0].Error.Code, ErrorCodeModelResponse)
					}
					if !strings.Contains(trace[0].Error.Message, "stream parser failed") {
						t.Fatalf("error message should include driver error detail, got %q", trace[0].Error.Message)
					}
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	events := make([]StreamEvent, 0, 3)
	result, err := engine.RunStream(context.Background(), RunRequest{
		Config: RunConfig{MaxSteps: 3},
	}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("run stream failed: %v", err)
	}
	if result.StopReason != StopReasonFinal {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonFinal)
	}
	if result.FinalMessage != "recovered" {
		t.Fatalf("final message mismatch: got %q", result.FinalMessage)
	}
	if len(events) != 2 {
		t.Fatalf("event count mismatch: got %d, want 2", len(events))
	}
	if events[0].Frame.Type != MessageTypeError {
		t.Fatalf("events[0] should be error, got %q", events[0].Frame.Type)
	}
	if events[1].Frame.Type != MessageTypeFinal {
		t.Fatalf("events[1] should be final, got %q", events[1].Frame.Type)
	}
}

func TestEngine_RunStream_MaxStepsSafetyExit(t *testing.T) {
	router := buildTestRouter(t, "internal/echo", func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
		return tools.CallResult{Name: req.Name, Content: "ok"}, nil
	})
	driver := &scriptedStreamDriver{
		t: t,
		steps: []streamDriverStep{
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeToolCall,
					ToolCall: &ToolCallPayload{
						CallID:    "c1",
						Name:      "internal/echo",
						Arguments: mustRawJSON(t, map[string]any{}),
					},
				},
			},
			{
				msg: Message{
					Version: ProtocolVersion,
					Type:    MessageTypeToolCall,
					ToolCall: &ToolCallPayload{
						CallID:    "c2",
						Name:      "internal/echo",
						Arguments: mustRawJSON(t, map[string]any{}),
					},
				},
			},
		},
	}

	engine := NewEngine(router, driver, EngineOptions{})
	events := make([]StreamEvent, 0, 4)
	result, err := engine.RunStream(context.Background(), RunRequest{
		Config: RunConfig{MaxSteps: 1},
	}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("run stream failed: %v", err)
	}
	if result.StopReason != StopReasonMaxSteps {
		t.Fatalf("stop reason mismatch: got %q, want %q", result.StopReason, StopReasonMaxSteps)
	}
	if len(events) == 0 || events[len(events)-1].Frame.Type != MessageTypeFinal {
		t.Fatalf("expected safety final event, got %+v", events)
	}
}

type scriptedDriver struct {
	t     *testing.T
	steps []driverStep
	calls []int
}

type driverStep struct {
	msg         Message
	err         error
	assertTrace func(t *testing.T, trace []Message)
}

func (d *scriptedDriver) Next(ctx context.Context, req RunRequest, trace []Message) (Message, error) {
	index := len(d.calls)
	d.calls = append(d.calls, index)
	if index >= len(d.steps) {
		return Message{}, errors.New("unexpected model step")
	}
	step := d.steps[index]
	if step.assertTrace != nil {
		step.assertTrace(d.t, trace)
	}
	return step.msg, step.err
}

type repeatToolCallDriver struct {
	msg   Message
	calls int
}

func (d *repeatToolCallDriver) Next(ctx context.Context, req RunRequest, trace []Message) (Message, error) {
	d.calls++
	return d.msg, nil
}

type streamDriverStep struct {
	frames      []StreamMessage
	msg         Message
	err         error
	assertTrace func(t *testing.T, trace []Message)
}

type scriptedStreamDriver struct {
	t     *testing.T
	steps []streamDriverStep
	calls int
}

func (d *scriptedStreamDriver) Next(ctx context.Context, req RunRequest, trace []Message) (Message, error) {
	if d.calls >= len(d.steps) {
		return Message{}, errors.New("unexpected model step")
	}
	step := d.steps[d.calls]
	d.calls++
	return step.msg, step.err
}

func (d *scriptedStreamDriver) NextStream(ctx context.Context, req RunRequest, trace []Message, onFrame StreamFrameHandler) (Message, error) {
	if d.calls >= len(d.steps) {
		return Message{}, errors.New("unexpected model step")
	}
	step := d.steps[d.calls]
	d.calls++
	if step.assertTrace != nil {
		step.assertTrace(d.t, trace)
	}
	for _, frame := range step.frames {
		if onFrame == nil {
			continue
		}
		if err := onFrame(frame); err != nil {
			return Message{}, err
		}
	}
	return step.msg, step.err
}

func buildTestRouter(t *testing.T, toolName string, handler func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error)) tools.Router {
	t.Helper()
	router := tools.NewRouter()
	provider := &testProvider{
		toolName: toolName,
		handler:  handler,
	}
	if err := router.Register("internal", provider); err != nil {
		t.Fatalf("register provider failed: %v", err)
	}
	return router
}

type testProvider struct {
	toolName string
	handler  func(ctx context.Context, req tools.CallRequest) (tools.CallResult, error)
}

func (p *testProvider) ListTools(ctx context.Context) ([]tools.Descriptor, error) {
	return []tools.Descriptor{
		{
			Name:   p.toolName,
			Source: tools.SourceInternal,
		},
	}, nil
}

func (p *testProvider) CallTool(ctx context.Context, req tools.CallRequest) (tools.CallResult, error) {
	if strings.TrimSpace(req.Name) != p.toolName {
		return tools.CallResult{
			Name:    req.Name,
			IsError: true,
			Error: &tools.CallError{
				Code:      tools.ErrorCodeNotFound,
				Message:   "tool not found",
				Retryable: false,
			},
		}, nil
	}
	if p.handler == nil {
		return tools.CallResult{Name: req.Name}, nil
	}
	return p.handler(ctx, req)
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON failed: %v", err)
	}
	return data
}

func decodeObjectMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	decoded := make(map[string]any)
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode object failed: %v", err)
	}
	return decoded
}
