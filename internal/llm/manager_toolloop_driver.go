package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/dongwlin/nekomimi/internal/llm/toolloop"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

type managerToolLoopDriver struct {
	manager  *Manager
	provider string
	source   string
}

func newManagerToolLoopDriver(manager *Manager, provider, source string) toolloop.ModelDriver {
	return &managerToolLoopDriver{
		manager:  manager,
		provider: strings.TrimSpace(provider),
		source:   strings.TrimSpace(source),
	}
}

func (d *managerToolLoopDriver) Next(ctx context.Context, req toolloop.RunRequest, trace []toolloop.Message) (toolloop.Message, error) {
	messages := make([]model.Message, 0, len(req.Messages)+1)
	messages = append(messages, req.Messages...)
	messages = append(messages, model.Message{
		Role:    "user",
		Content: buildToolLoopInstruction(req.Tools, trace),
	})

	source := strings.TrimSpace(d.source)
	if source == "" {
		source = "tool_loop_step"
	} else {
		source += "_tool_loop_step"
	}

	reply, err := d.manager.generateWithProvider(ctx, d.provider, req.ModelName, req.SystemPrompt, messages, llmclient.RequestOptions{
		Source: source,
	})
	if err != nil {
		return toolloop.Message{}, err
	}

	if frame, ok := parseToolLoopFrame(reply); ok {
		return frame, nil
	}

	return toolloop.Message{
		Version: toolloop.ProtocolVersion,
		Type:    toolloop.MessageTypeFinal,
		Final: &toolloop.FinalPayload{
			Content:    strings.TrimSpace(reply),
			StopReason: toolloop.StopReasonFinal,
		},
	}, nil
}

func (d *managerToolLoopDriver) NextStream(ctx context.Context, req toolloop.RunRequest, trace []toolloop.Message, onFrame toolloop.StreamFrameHandler) (toolloop.Message, error) {
	messages := make([]model.Message, 0, len(req.Messages)+1)
	messages = append(messages, req.Messages...)
	messages = append(messages, model.Message{
		Role:    "user",
		Content: buildToolLoopStreamInstruction(req.Tools, trace),
	})

	source := strings.TrimSpace(d.source)
	if source == "" {
		source = "tool_loop_step_stream"
	} else {
		source += "_tool_loop_step_stream"
	}

	parser := toolloop.NewNDJSONParser()
	var terminal *toolloop.Message
	var deltaBuilder strings.Builder
	fallbackLineCount := 0
	consume := func(items []toolloop.NDJSONItem) error {
		for _, item := range items {
			if item.Frame != nil {
				frame := *item.Frame
				switch frame.Type {
				case toolloop.MessageTypeDelta:
					if frame.Delta == nil || frame.Delta.Text == "" {
						continue
					}
					deltaBuilder.WriteString(frame.Delta.Text)
					if onFrame != nil {
						if err := onFrame(frame); err != nil {
							return err
						}
					}
				case toolloop.MessageTypeToolCall, toolloop.MessageTypeFinal, toolloop.MessageTypeError:
					if terminal != nil {
						return errors.New("invalid stream frame: multiple terminal frames in one model step")
					}
					msg, ok := streamFrameTerminalMessage(frame)
					if !ok {
						return errors.New("invalid stream frame: unsupported terminal type")
					}
					terminal = &msg
				default:
					return errors.New("invalid stream frame: unsupported frame type")
				}
				continue
			}

			if item.Text == "" {
				continue
			}
			deltaText := item.Text
			if fallbackLineCount > 0 {
				deltaText = "\n" + deltaText
			}
			deltaBuilder.WriteString(deltaText)
			fallbackLineCount++
			if onFrame != nil {
				if err := onFrame(toolloop.StreamMessage{
					Version: toolloop.StreamProtocolVersion,
					Type:    toolloop.MessageTypeDelta,
					Delta: &toolloop.DeltaPayload{
						Text: deltaText,
					},
				}); err != nil {
					return err
				}
			}
		}
		return nil
	}

	_, err := d.manager.generateStreamWithProvider(ctx, d.provider, req.ModelName, req.SystemPrompt, messages, llmclient.RequestOptions{
		Source: source,
	}, func(delta string) error {
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

	fallback := deltaBuilder.String()
	if strings.TrimSpace(fallback) != "" {
		return toolloop.Message{
			Version: toolloop.ProtocolVersion,
			Type:    toolloop.MessageTypeFinal,
			Final: &toolloop.FinalPayload{
				Content:    fallback,
				StopReason: toolloop.StopReasonFinal,
			},
		}, nil
	}
	return toolloop.Message{}, errors.New("invalid stream frame: missing terminal frame")
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
		"protocol_version": toolloop.ProtocolVersion,
		"available_tools":  toolViews,
		"trace":            trace,
	}
	stateJSON, _ := json.Marshal(state)

	var builder strings.Builder
	builder.WriteString("You are a tool-loop controller. Return EXACTLY one JSON object and no markdown.\n")
	builder.WriteString("Allowed types: tool_call, final, error.\n")
	builder.WriteString("When choosing tool_call, output fields: version,type,tool_call(call_id,name,arguments object).\n")
	builder.WriteString("When choosing final, output fields: version,type,final(content,stop_reason=final).\n")
	builder.WriteString("When choosing error, output fields: version,type,error(code,message,retryable).\n")
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
		"protocol_version": toolloop.StreamProtocolVersion,
		"available_tools":  toolViews,
		"trace":            trace,
	}
	stateJSON, _ := json.Marshal(state)

	var builder strings.Builder
	builder.WriteString("You are a tool-loop controller.\n")
	builder.WriteString("Return NDJSON only: one JSON object per line, no markdown, no code fence.\n")
	builder.WriteString("Use version=\"v2\" and type in {delta, tool_call, final, error}.\n")
	builder.WriteString("delta payload shape: {\"delta\":{\"text\":\"...\"}}.\n")
	builder.WriteString("tool_call payload shape: {\"tool_call\":{\"call_id\":\"...\",\"name\":\"...\",\"arguments\":{}}}.\n")
	builder.WriteString("final payload shape: {\"final\":{\"content\":\"...\",\"stop_reason\":\"final\"}}.\n")
	builder.WriteString("error payload shape: {\"error\":{\"code\":\"...\",\"message\":\"...\",\"retryable\":false}}.\n")
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
			Version:  toolloop.ProtocolVersion,
			Type:     toolloop.MessageTypeToolCall,
			ToolCall: frame.ToolCall,
		}, true
	case toolloop.MessageTypeFinal:
		return toolloop.Message{
			Version: toolloop.ProtocolVersion,
			Type:    toolloop.MessageTypeFinal,
			Final:   frame.Final,
		}, true
	case toolloop.MessageTypeError:
		return toolloop.Message{
			Version: toolloop.ProtocolVersion,
			Type:    toolloop.MessageTypeError,
			Error:   frame.Error,
		}, true
	default:
		return toolloop.Message{}, false
	}
}

func parseToolLoopFrame(raw string) (toolloop.Message, bool) {
	candidate := extractJSONObjectCandidate(raw)
	if candidate == "" {
		return toolloop.Message{}, false
	}

	var frame toolloop.Message
	if err := json.Unmarshal([]byte(candidate), &frame); err != nil {
		return toolloop.Message{}, false
	}
	if strings.TrimSpace(string(frame.Type)) == "" {
		return toolloop.Message{}, false
	}
	if strings.TrimSpace(frame.Version) == "" {
		frame.Version = toolloop.ProtocolVersion
	}
	if frame.Type == toolloop.MessageTypeFinal && frame.Final != nil && frame.Final.StopReason == "" {
		frame.Final.StopReason = toolloop.StopReasonFinal
	}
	if frame.Type == toolloop.MessageTypeToolCall && frame.ToolCall != nil && len(frame.ToolCall.Arguments) == 0 {
		frame.ToolCall.Arguments = json.RawMessage(`{}`)
	}
	return frame, true
}

func extractJSONObjectCandidate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = stripCodeFence(trimmed)
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}

	first := strings.IndexByte(trimmed, '{')
	last := strings.LastIndexByte(trimmed, '}')
	if first >= 0 && last > first {
		candidate := strings.TrimSpace(trimmed[first : last+1])
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}
	return ""
}

func stripCodeFence(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}
	if strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			lines = lines[:i]
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
