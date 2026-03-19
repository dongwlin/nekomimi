package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dongwlin/nekomimi/internal/ctxasm"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
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
	RequestOptions   llmclient.RequestOptions
	Meta             ctxasm.Meta
	ImmersiveContext *ctxasm.ImmersiveContext
}

type pipelineState struct {
	model            string
	systemPrompt     string
	assistantSpeaker string
	assembler        *ctxasm.Assembler
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
		newManagerToolLoopDriver(m, withRequestSource(req.RequestOptions, req.Source)),
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

	reply, err := m.generateWithProvider(ctx, state.model, requestPrompt, messages, withRequestSource(req.RequestOptions, req.Source))
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
		reply, err := m.generateStreamWithProvider(ctx, state.model, requestPrompt, messages, withRequestSource(req.RequestOptions, req.Source), func(delta string) error {
			if delta == "" {
				return nil
			}
			return emit(0, toolloop.StreamMessage{
				Type: toolloop.MessageTypeDelta,
				Delta: &toolloop.DeltaPayload{
					Text: delta,
				},
			})
		})
		if err != nil {
			return "", err
		}
		if err := emit(0, toolloop.StreamMessage{
			Type: toolloop.MessageTypeFinal,
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
		newManagerToolLoopDriver(m, withRequestSource(req.RequestOptions, req.Source)),
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
