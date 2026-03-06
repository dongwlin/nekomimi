package commands

import (
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/metrics"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type ImmersiveEngine interface {
	Enqueue(ctx *zero.Ctx, sessionKey, text, speaker string, isPrivate bool)
	RecordTimelineEvent(sessionKey, text, speaker string)
	Clear(sessionKey string)
	RefreshIdentityFromCtx(ctx *zero.Ctx)
	ReloadConfig(cfg config.ImmersiveConfig, nicknames []string)
}

func Register(cfg *config.Config, llmManager llm.Service, engine ImmersiveEngine, collector *metrics.Collector) {
	setMetricsCollector(collector)
	registerInboundMetricsMatchers()
	registerAIHandlers(cfg, llmManager, engine)
	registerLLMHandlers(llmManager)
	registerBasicHandlers(cfg, llmManager, engine)
}
