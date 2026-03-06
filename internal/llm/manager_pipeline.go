package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
	"github.com/dongwlin/nekomimi/internal/llm/model"
	"github.com/dongwlin/nekomimi/internal/llm/toolloop"
	"github.com/dongwlin/nekomimi/internal/llm/tools"
)

type pipelineRequest struct {
	UserInput        string
	SessionKey       string
	Speaker          string
	ExtraPrompt      string
	Source           string
	AppendTurn       bool
	DisableTools     bool
	Meta             contextassemble.Meta
	ImmersiveContext *contextassemble.ImmersiveContext
}

type pipelineState struct {
	provider         string
	model            string
	systemPrompt     string
	assistantSpeaker string
	assembler        *contextassemble.Assembler
	router           tools.Router
	toolsEnabled     bool
	toolLoopMaxStep  int
	toolLoopTimeout  time.Duration
}

func (m *Manager) replyWithPipeline(ctx context.Context, req pipelineRequest) (string, error) {
	if m == nil {
		return "", errors.New("manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	state := m.snapshotPipelineState()
	if strings.TrimSpace(state.model) == "" {
		return "", errors.New("model is not configured")
	}

	eventTime := time.Now()
	userContent := formatUserContentAt(req.UserInput, req.Speaker, eventTime)
	if req.AppendTurn && strings.TrimSpace(userContent) == "" {
		return "", errors.New("input is empty")
	}
	if !req.AppendTurn && strings.TrimSpace(userContent) == "" && strings.TrimSpace(req.SessionKey) == "" {
		return "", errors.New("input is empty")
	}

	var replyCutoffSeq int64
	if req.AppendTurn {
		var ok bool
		replyCutoffSeq, ok = m.AppendUserEventAt(req.SessionKey, req.UserInput, req.Speaker, eventTime)
		if !ok {
			return "", errors.New("append user event failed")
		}
	}

	requestPrompt := composeSystemPrompt(state.systemPrompt, req.ExtraPrompt)
	meta := buildPipelineMeta(req.SessionKey, state.assistantSpeaker, req.Meta)
	messages, compressed, err := m.buildPipelineMessages(ctx, state.assembler, req.SessionKey, meta, userContent, req.ImmersiveContext)
	if err != nil {
		return "", err
	}
	if compressed {
		m.sessions.incrementContextTrimCount(req.SessionKey)
	}

	var toolDescriptors []tools.Descriptor
	if !req.DisableTools && state.toolsEnabled && state.router != nil {
		toolDescriptors, err = state.router.ListTools(ctx)
		if err != nil {
			return "", fmt.Errorf("list tools failed: %w", err)
		}
	}

	runCtx := ctx
	cancel := func() {}
	if state.toolLoopTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, state.toolLoopTimeout)
	}
	defer cancel()

	engine := toolloop.NewEngine(
		state.router,
		newManagerToolLoopDriver(m, state.provider, req.Source),
		toolloop.EngineOptions{DefaultMaxSteps: state.toolLoopMaxStep},
	)
	result, err := engine.Run(runCtx, toolloop.RunRequest{
		ModelName:    state.model,
		SystemPrompt: requestPrompt,
		Messages:     messages,
		Tools:        toolDescriptors,
		Config: toolloop.RunConfig{
			MaxSteps: state.toolLoopMaxStep,
		},
	})
	if err != nil {
		return "", err
	}

	reply, err := finalizeToolLoopResult(result)
	if err != nil {
		return "", err
	}
	if req.AppendTurn {
		_ = m.AppendAssistantEvent(req.SessionKey, reply, replyCutoffSeq)
	}
	return reply, nil
}

func (m *Manager) decideIntentWithPipeline(ctx context.Context, req pipelineRequest) (llmintent.ControlIntent, error) {
	if m == nil {
		return llmintent.ControlIntent{}, errors.New("manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	state := m.snapshotPipelineState()
	if strings.TrimSpace(state.model) == "" {
		return llmintent.ControlIntent{}, errors.New("model is not configured")
	}

	eventTime := time.Now()
	userContent := formatUserContentAt(req.UserInput, req.Speaker, eventTime)
	if strings.TrimSpace(userContent) == "" && strings.TrimSpace(req.SessionKey) == "" {
		return llmintent.ControlIntent{}, errors.New("input is empty")
	}

	requestPrompt := composeSystemPrompt(state.systemPrompt, req.ExtraPrompt)
	meta := buildPipelineMeta(req.SessionKey, state.assistantSpeaker, req.Meta)
	messages, compressed, err := m.buildPipelineMessages(ctx, state.assembler, req.SessionKey, meta, userContent, req.ImmersiveContext)
	if err != nil {
		return llmintent.ControlIntent{}, err
	}
	if compressed {
		m.sessions.incrementContextTrimCount(req.SessionKey)
	}

	reply, err := m.generateWithProvider(ctx, state.provider, state.model, requestPrompt, messages, llmclient.RequestOptions{
		Source: req.Source,
	})
	if err != nil {
		return llmintent.ControlIntent{}, err
	}

	decision, err := llmintent.Parse(reply)
	if err != nil {
		return llmintent.ControlIntent{}, err
	}
	return decision, nil
}

func (m *Manager) replyStreamWithPipeline(ctx context.Context, req pipelineRequest, onEvent StreamEventHandler) (string, error) {
	if m == nil {
		return "", errors.New("manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	state := m.snapshotPipelineState()
	if strings.TrimSpace(state.model) == "" {
		return "", errors.New("model is not configured")
	}

	eventTime := time.Now()
	userContent := formatUserContentAt(req.UserInput, req.Speaker, eventTime)
	if req.AppendTurn && strings.TrimSpace(userContent) == "" {
		return "", errors.New("input is empty")
	}
	if !req.AppendTurn && strings.TrimSpace(userContent) == "" && strings.TrimSpace(req.SessionKey) == "" {
		return "", errors.New("input is empty")
	}

	var replyCutoffSeq int64
	if req.AppendTurn {
		var ok bool
		replyCutoffSeq, ok = m.AppendUserEventAt(req.SessionKey, req.UserInput, req.Speaker, eventTime)
		if !ok {
			return "", errors.New("append user event failed")
		}
	}

	requestPrompt := composeSystemPrompt(state.systemPrompt, req.ExtraPrompt)
	meta := buildPipelineMeta(req.SessionKey, state.assistantSpeaker, req.Meta)
	messages, compressed, err := m.buildPipelineMessages(ctx, state.assembler, req.SessionKey, meta, userContent, req.ImmersiveContext)
	if err != nil {
		return "", err
	}
	if compressed {
		m.sessions.incrementContextTrimCount(req.SessionKey)
	}

	var seq int64
	emit := func(step int, message toolloop.StreamMessage) error {
		if onEvent == nil {
			return nil
		}
		current := atomic.AddInt64(&seq, 1)
		return onEvent(mapToolLoopStreamEvent(current, step, message))
	}

	if req.DisableTools || !state.toolsEnabled || state.router == nil {
		reply, err := m.generateStreamWithProvider(ctx, state.provider, state.model, requestPrompt, messages, llmclient.RequestOptions{
			Source: req.Source,
		}, func(delta string) error {
			if delta == "" {
				return nil
			}
			return emit(0, toolloop.StreamMessage{
				Version: toolloop.StreamProtocolVersion,
				Type:    toolloop.MessageTypeDelta,
				Delta: &toolloop.DeltaPayload{
					Text: delta,
				},
			})
		})
		if err != nil {
			return "", err
		}
		if err := emit(0, toolloop.StreamMessage{
			Version: toolloop.StreamProtocolVersion,
			Type:    toolloop.MessageTypeFinal,
			Final: &toolloop.FinalPayload{
				Content:    strings.TrimSpace(reply),
				StopReason: toolloop.StopReasonFinal,
			},
		}); err != nil {
			return "", err
		}
		if req.AppendTurn {
			_ = m.AppendAssistantEvent(req.SessionKey, reply, replyCutoffSeq)
		}
		return reply, nil
	}

	toolDescriptors, err := state.router.ListTools(ctx)
	if err != nil {
		return "", fmt.Errorf("list tools failed: %w", err)
	}

	runCtx := ctx
	cancel := func() {}
	if state.toolLoopTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, state.toolLoopTimeout)
	}
	defer cancel()

	engine := toolloop.NewEngine(
		state.router,
		newManagerToolLoopDriver(m, state.provider, req.Source),
		toolloop.EngineOptions{DefaultMaxSteps: state.toolLoopMaxStep},
	)
	result, err := engine.RunStream(runCtx, toolloop.RunRequest{
		ModelName:    state.model,
		SystemPrompt: requestPrompt,
		Messages:     messages,
		Tools:        toolDescriptors,
		Config: toolloop.RunConfig{
			MaxSteps: state.toolLoopMaxStep,
		},
	}, func(event toolloop.StreamEvent) error {
		return emit(event.Step, event.Frame)
	})
	if err != nil {
		return "", err
	}

	reply, err := finalizeToolLoopResult(result)
	if err != nil {
		return "", err
	}
	if req.AppendTurn {
		_ = m.AppendAssistantEvent(req.SessionKey, reply, replyCutoffSeq)
	}
	return reply, nil
}

func (m *Manager) snapshotPipelineState() pipelineState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return pipelineState{
		provider:         m.current.provider,
		model:            m.current.model,
		systemPrompt:     m.current.systemPrompt,
		assistantSpeaker: m.current.assistantSpeaker,
		assembler:        m.contextAssembler,
		router:           m.toolRouter,
		toolsEnabled:     m.current.toolsEnabled,
		toolLoopMaxStep:  m.current.toolLoopMaxSteps,
		toolLoopTimeout:  m.current.toolLoopTimeout,
	}
}

func (m *Manager) buildPipelineMessages(ctx context.Context, assembler *contextassemble.Assembler, sessionKey string, meta contextassemble.Meta, fallbackContent string, immersiveCtx *contextassemble.ImmersiveContext) ([]model.Message, bool, error) {
	session := strings.TrimSpace(sessionKey)
	if assembler == nil || session == "" {
		content := strings.TrimSpace(fallbackContent)
		if content == "" {
			return nil, false, errors.New("input is empty")
		}
		return []model.Message{{Role: "user", Content: content}}, false, nil
	}

	assembled, err := assembler.Assemble(ctx, contextassemble.Request{
		SessionKey: session,
		Meta:       meta,
	})
	if err != nil {
		return nil, false, fmt.Errorf("assemble context: %w", err)
	}

	compressed := false
	for _, block := range assembled.Blocks {
		if block.Truncated {
			compressed = true
			break
		}
	}

	blocks := assembled.Blocks
	if immersiveCtx != nil {
		blocks = append(blocks, contextassemble.Block{
			Name:    contextassemble.BlockImmersiveSignals,
			Content: contextassemble.FormatImmersiveContext(immersiveCtx),
		})
	}

	content := renderAssembledBlocks(blocks)
	if strings.TrimSpace(content) == "" {
		content = strings.TrimSpace(fallbackContent)
	}
	if strings.TrimSpace(content) == "" {
		return nil, false, errors.New("input is empty")
	}
	return []model.Message{{Role: "user", Content: content}}, compressed, nil
}

func renderAssembledBlocks(blocks []contextassemble.Block) string {
	if len(blocks) == 0 {
		return ""
	}

	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		header := "[" + strings.TrimSpace(block.Name) + "]"
		if block.Truncated {
			header += " (truncated)"
		}
		content := strings.TrimSpace(block.Content)
		if content == "" {
			content = "(empty)"
		}
		parts = append(parts, header+"\n"+content)
	}
	return strings.Join(parts, "\n\n")
}

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

func buildPipelineMeta(sessionKey, assistantSpeaker string, override contextassemble.Meta) contextassemble.Meta {
	meta := contextassemble.Meta{
		Now:               time.Now().Format("2006-01-02 15:04:05"),
		AssistantIdentity: strings.TrimSpace(assistantSpeaker),
		BotConfigNames:    extractBotConfigNames(assistantSpeaker),
		SessionType:       inferSessionType(sessionKey),
	}

	if trimmed := strings.TrimSpace(override.Now); trimmed != "" {
		meta.Now = trimmed
	}
	if trimmed := strings.TrimSpace(override.AssistantIdentity); trimmed != "" {
		meta.AssistantIdentity = trimmed
	}
	if cleaned := cleanStringSlice(override.BotConfigNames); len(cleaned) > 0 {
		meta.BotConfigNames = cleaned
	}
	if trimmed := strings.TrimSpace(strings.ToLower(override.SessionType)); trimmed != "" {
		meta.SessionType = trimmed
	}
	return meta
}

func inferSessionType(sessionKey string) string {
	session := strings.TrimSpace(strings.ToLower(sessionKey))
	if session == "" {
		return "unknown"
	}
	if strings.HasPrefix(session, "group:") {
		return "group"
	}
	if strings.HasPrefix(session, "private:") {
		return "private"
	}
	if parts := strings.SplitN(session, ":", 2); len(parts) > 1 && strings.TrimSpace(parts[0]) != "" {
		return strings.TrimSpace(parts[0])
	}
	return "unknown"
}

func extractBotConfigNames(assistantSpeaker string) []string {
	trimmed := strings.TrimSpace(assistantSpeaker)
	if trimmed == "" {
		return nil
	}
	for _, part := range strings.Split(trimmed, ";") {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(strings.ToLower(key)) != "name" {
			continue
		}
		if name := strings.TrimSpace(value); name != "" {
			return []string{name}
		}
	}
	if strings.Contains(trimmed, "=") {
		return nil
	}
	return []string{trimmed}
}

func cleanStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
