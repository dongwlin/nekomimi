package llm

import (
	"context"
	"encoding/json"
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/jsonutil"
	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/dongwlin/nekomimi/internal/llm/toolloop"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

type managerToolLoopDriver struct {
	manager *Manager
	options llmclient.RequestOptions
}

func newManagerToolLoopDriver(manager *Manager, options llmclient.RequestOptions) toolloop.ModelDriver {
	return &managerToolLoopDriver{
		manager: manager,
		options: options,
	}
}

func (d *managerToolLoopDriver) Next(ctx context.Context, req toolloop.RunRequest, trace []toolloop.Message) (toolloop.Message, error) {
	messages := make([]model.Message, 0, len(req.Messages)+1)
	messages = append(messages, req.Messages...)
	messages = append(messages, model.Message{
		Role:    "user",
		Content: buildToolLoopInstruction(req.Tools, trace),
	})

	source := strings.TrimSpace(d.options.Source)
	if source == "" {
		source = "tool_loop_step"
	} else {
		source += "_tool_loop_step"
	}

	reply, err := d.manager.generateWithProvider(ctx, req.ModelName, req.SystemPrompt, messages, withRequestSource(d.options, source))
	if err != nil {
		return toolloop.Message{}, err
	}

	return parseToolLoopFrame(reply)
}

func (d *managerToolLoopDriver) NextStream(ctx context.Context, req toolloop.RunRequest, trace []toolloop.Message, onFrame toolloop.StreamFrameHandler) (toolloop.Message, error) {
	messages := make([]model.Message, 0, len(req.Messages)+1)
	messages = append(messages, req.Messages...)
	messages = append(messages, model.Message{
		Role:    "user",
		Content: buildToolLoopStreamInstruction(req.Tools, trace),
	})

	source := strings.TrimSpace(d.options.Source)
	if source == "" {
		source = "tool_loop_step_stream"
	} else {
		source += "_tool_loop_step_stream"
	}

	parser := toolloop.NewNDJSONParser()
	var terminal *toolloop.Message
	consume := func(items []toolloop.NDJSONItem) error {
		for _, item := range items {
			if item.Frame != nil {
				frame := *item.Frame
				switch frame.Type {
				case toolloop.MessageTypeDelta:
					if frame.Delta == nil || frame.Delta.Text == "" {
						continue
					}
					if onFrame != nil {
						if err := onFrame(frame); err != nil {
							return err
						}
					}
				case toolloop.MessageTypeToolCall, toolloop.MessageTypeFinal, toolloop.MessageTypeError:
					if terminal != nil {
						return toolloop.NewProtocolError("multiple terminal frames in one model step")
					}
					msg, ok := streamFrameTerminalMessage(frame)
					if !ok {
						return toolloop.NewProtocolError("unsupported terminal frame type")
					}
					terminal = &msg
				default:
					return toolloop.NewProtocolError("unsupported stream frame type")
				}
				continue
			}
			if strings.TrimSpace(item.Text) != "" {
				return toolloop.NewProtocolError("unexpected plain-text stream content")
			}
		}
		return nil
	}

	_, err := d.manager.generateStreamWithProvider(ctx, req.ModelName, req.SystemPrompt, messages, withRequestSource(d.options, source), func(delta string) error {
		items, feedErr := parser.Feed(delta)
		if feedErr != nil {
			return feedErr
		}
		return consume(items)
	})
	if err != nil {
		return toolloop.Message{}, err
	}

	remaining, err := parser.Flush()
	if err != nil {
		return toolloop.Message{}, err
	}
	if err := consume(remaining); err != nil {
		return toolloop.Message{}, err
	}

	if terminal != nil {
		return *terminal, nil
	}

	return toolloop.Message{}, toolloop.NewProtocolError("missing terminal frame")
}

func buildToolLoopInstruction(descriptors []tools.Descriptor, trace []toolloop.Message) string {
	toolViews := make([]map[string]any, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entry := map[string]any{
			"name":        strings.TrimSpace(descriptor.Name),
			"description": strings.TrimSpace(descriptor.Description),
			"source":      strings.TrimSpace(descriptor.Source),
		}
		if len(descriptor.InputSchema) > 0 {
			entry["input_schema"] = json.RawMessage(descriptor.InputSchema)
		}
		toolViews = append(toolViews, entry)
	}

	state := map[string]any{
		"available_tools": toolViews,
		"trace":           trace,
	}
	stateJSON, _ := json.Marshal(state)

	var builder strings.Builder
	builder.WriteString("You are a tool-loop controller. Return EXACTLY one JSON object and no markdown.\n")
	builder.WriteString("Allowed types: tool_call, final, error.\n")
	builder.WriteString("When choosing tool_call, output fields: type,tool_call(call_id,name,arguments object).\n")
	builder.WriteString("When choosing final, output fields: type,final(content,stop_reason=final).\n")
	builder.WriteString("When choosing error, output fields: type,error(code,message,retryable).\n")
	builder.WriteString("State JSON:\n")
	builder.Write(stateJSON)
	return builder.String()
}

func buildToolLoopStreamInstruction(descriptors []tools.Descriptor, trace []toolloop.Message) string {
	toolViews := make([]map[string]any, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entry := map[string]any{
			"name":        strings.TrimSpace(descriptor.Name),
			"description": strings.TrimSpace(descriptor.Description),
			"source":      strings.TrimSpace(descriptor.Source),
		}
		if len(descriptor.InputSchema) > 0 {
			entry["input_schema"] = json.RawMessage(descriptor.InputSchema)
		}
		toolViews = append(toolViews, entry)
	}

	state := map[string]any{
		"available_tools": toolViews,
		"trace":           trace,
	}
	stateJSON, _ := json.Marshal(state)

	var builder strings.Builder
	builder.WriteString("You are a tool-loop controller.\n")
	builder.WriteString("Return NDJSON only: one JSON object per line, no markdown, no code fence.\n")
	builder.WriteString("Use type in {delta, tool_call, final, error}.\n")
	builder.WriteString("delta payload shape: {\"type\":\"delta\",\"delta\":{\"text\":\"...\"}}.\n")
	builder.WriteString("tool_call payload shape: {\"type\":\"tool_call\",\"tool_call\":{\"call_id\":\"...\",\"name\":\"...\",\"arguments\":{}}}.\n")
	builder.WriteString("final payload shape: {\"type\":\"final\",\"final\":{\"content\":\"...\",\"stop_reason\":\"final\"}}.\n")
	builder.WriteString("error payload shape: {\"type\":\"error\",\"error\":{\"code\":\"...\",\"message\":\"...\",\"retryable\":false}}.\n")
	builder.WriteString("Tool-call arguments must be a JSON object.\n")
	builder.WriteString("If no tool is needed, emit final frame.\n")
	builder.WriteString("State JSON:\n")
	builder.Write(stateJSON)
	return builder.String()
}

func streamFrameTerminalMessage(frame toolloop.StreamMessage) (toolloop.Message, bool) {
	switch frame.Type {
	case toolloop.MessageTypeToolCall:
		return toolloop.Message{
			Type:     toolloop.MessageTypeToolCall,
			ToolCall: frame.ToolCall,
		}, true
	case toolloop.MessageTypeFinal:
		return toolloop.Message{
			Type:  toolloop.MessageTypeFinal,
			Final: frame.Final,
		}, true
	case toolloop.MessageTypeError:
		return toolloop.Message{
			Type:  toolloop.MessageTypeError,
			Error: frame.Error,
		}, true
	default:
		return toolloop.Message{}, false
	}
}

func parseToolLoopFrame(raw string) (toolloop.Message, error) {
	candidate := jsonutil.ExtractJSONObjectCandidate(raw)
	if candidate == "" {
		return toolloop.Message{}, toolloop.NewProtocolError("expected one JSON object response")
	}
	if !looksLikeToolLoopFrame(candidate) {
		return toolloop.Message{}, toolloop.NewProtocolError("expected protocol object with type and matching payload")
	}

	var frame toolloop.Message
	if err := json.Unmarshal([]byte(candidate), &frame); err != nil {
		return toolloop.Message{}, toolloop.NewProtocolError("invalid JSON object response")
	}
	if strings.TrimSpace(string(frame.Type)) == "" {
		return toolloop.Message{}, toolloop.NewProtocolError("missing type field")
	}
	toolloop.NormalizeModelMessage(&frame)
	return frame, nil
}

func looksLikeToolLoopFrame(candidate string) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
		return false
	}

	switch frameType := strings.TrimSpace(jsonutil.ReadJSONStringField(payload, "type")); frameType {
	case string(toolloop.MessageTypeDelta):
		return jsonutil.HasJSONField(payload, "delta")
	case string(toolloop.MessageTypeToolCall):
		return jsonutil.HasJSONField(payload, "tool_call")
	case string(toolloop.MessageTypeToolResult):
		return jsonutil.HasJSONField(payload, "tool_result")
	case string(toolloop.MessageTypeFinal):
		return jsonutil.HasJSONField(payload, "final")
	case string(toolloop.MessageTypeError):
		return jsonutil.HasJSONField(payload, "error")
	default:
		return false
	}
}
