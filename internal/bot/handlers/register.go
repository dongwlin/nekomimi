package handlers

import (
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type ImmersiveBuffer interface {
	Enqueue(ctx *zero.Ctx, sessionKey, text, speaker string, isPrivate bool)
	Clear(sessionKey string)
}

func Register(cfg *config.Config, llmManager *llm.Manager, buffer ImmersiveBuffer) {
	registerAIHandlers(cfg, llmManager, buffer)
	registerLLMHandlers(llmManager)
	registerBasicHandlers()
}
