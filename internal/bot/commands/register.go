package commands

import (
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type ImmersiveEngine interface {
	Enqueue(ctx *zero.Ctx, sessionKey, text, speaker string, isPrivate bool)
	Clear(sessionKey string)
	RefreshIdentityFromCtx(ctx *zero.Ctx)
}

func Register(cfg *config.Config, llmManager *llm.Manager, engine ImmersiveEngine) {
	registerAIHandlers(cfg, llmManager, engine)
	registerLLMHandlers(llmManager)
	registerBasicHandlers()
}
