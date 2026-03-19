package llm

import (
	"context"
	"time"

	"github.com/dongwlin/nekomimi/internal/ctxasm"
	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
	"github.com/rs/zerolog/log"
)

func (m *Manager) Reply(ctx context.Context, userInput, sessionKey, speaker string) (string, error) {
	startedAt := time.Now()
	reply, err := m.replyWithPipeline(ctx, pipelineRequest{
		UserInput:   userInput,
		SessionKey:  sessionKey,
		Speaker:     speaker,
		ExtraPrompt: "",
		Source:      "main_reply",
		AppendTurn:  true,
	})
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "main_reply").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm assistant reply failed")
		return "", err
	}
	log.Info().
		Str("request_source", "main_reply").
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm assistant reply completed")
	return reply, nil
}

func (m *Manager) ReplyStream(ctx context.Context, userInput, sessionKey, speaker string, onEvent StreamEventHandler) (string, error) {
	startedAt := time.Now()
	reply, err := m.replyStreamWithPipeline(ctx, pipelineRequest{
		UserInput:   userInput,
		SessionKey:  sessionKey,
		Speaker:     speaker,
		ExtraPrompt: "",
		Source:      "main_reply_stream",
		AppendTurn:  true,
	}, onEvent)
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "main_reply_stream").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm assistant streaming reply failed")
		return "", err
	}
	log.Info().
		Str("request_source", "main_reply_stream").
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm assistant streaming reply completed")
	return reply, nil
}

func (m *Manager) ReplyStreamWithExtraPrompt(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler) (string, error) {
	return m.replyStreamWithExtraPrompt(ctx, userInput, sessionKey, speaker, extraPrompt, onEvent, true, nil, llmclient.RequestOptions{})
}

func (m *Manager) ReplyStreamWithExtraPromptAllowTools(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler, immersiveCtx *ctxasm.ImmersiveContext) (string, error) {
	return m.replyStreamWithExtraPrompt(ctx, userInput, sessionKey, speaker, extraPrompt, onEvent, false, immersiveCtx, immersiveRequestOptions())
}

func (m *Manager) DecideImmersiveIntent(ctx context.Context, userInput, sessionKey, speaker string, immersiveCtx *ctxasm.ImmersiveContext) (llmintent.ControlIntent, error) {
	startedAt := time.Now()
	intent, err := m.decideIntentWithPipeline(ctx, pipelineRequest{
		UserInput:        userInput,
		SessionKey:       sessionKey,
		Speaker:          speaker,
		ExtraPrompt:      llmprompt.ImmersiveControlPrompt,
		Source:           "immersive_control_intent",
		AppendTurn:       false,
		RequestOptions:   immersiveRequestOptions(),
		ImmersiveContext: immersiveCtx,
	})
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "immersive_control_intent").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm immersive control intent failed")
		return llmintent.ControlIntent{}, err
	}

	log.Info().
		Str("request_source", "immersive_control_intent").
		Str("intent_action", string(intent.Action)).
		Int("intent_wait_ms", intent.WaitMS).
		Str("intent_reason", intent.Reason).
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm immersive control intent completed")
	return intent, nil
}

func (m *Manager) replyStreamWithExtraPrompt(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler, disableTools bool, immersiveCtx *ctxasm.ImmersiveContext, options llmclient.RequestOptions) (string, error) {
	startedAt := time.Now()
	reply, err := m.replyStreamWithPipeline(ctx, pipelineRequest{
		UserInput:        userInput,
		SessionKey:       sessionKey,
		Speaker:          speaker,
		ExtraPrompt:      extraPrompt,
		Source:           "extra_prompt_reply_stream",
		AppendTurn:       false,
		DisableTools:     disableTools,
		RequestOptions:   options,
		ImmersiveContext: immersiveCtx,
	}, onEvent)
	if err != nil {
		log.Warn().
			Err(err).
			Str("request_source", "extra_prompt_reply_stream").
			Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
			Msg("llm assistant streaming reply failed")
		return "", err
	}
	log.Info().
		Str("request_source", "extra_prompt_reply_stream").
		Int64("elapsed_ms", time.Since(startedAt).Milliseconds()).
		Msg("llm assistant streaming reply completed")
	return reply, nil
}

func immersiveRequestOptions() llmclient.RequestOptions {
	return llmclient.RequestOptions{
		Thinking: &llmclient.ThinkingConfig{
			Type: "disabled",
		},
	}
}
