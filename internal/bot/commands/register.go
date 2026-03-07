package commands

import (
	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/metrics"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type ImmersiveEngine interface {
	Enqueue(ctx *zero.Ctx, sessionKey, text, speaker string, isPrivate bool)
	RecordEvent(sessionKey string, event immersivepkg.TimelineEvent)
	RecordTimelineEvent(sessionKey, text, speaker string)
	DebugSnapshot(sessionKey string) immersivepkg.DebugSnapshot
	Clear(sessionKey string)
	RefreshIdentityFromCtx(ctx *zero.Ctx)
	ReloadConfig(cfg config.ImmersiveConfig, nicknames []string)
}

type RepeatEngine interface {
	Enqueue(ctx *zero.Ctx, sessionKey, text, speaker, assistantSpeaker string, isPrivate bool) bool
	Clear(sessionKey string)
	ReloadConfig(cfg config.RepeatConfig)
	SetEnabled(sessionKey string, enabled bool)
	IsEnabled(sessionKey string) bool
}

func Register(cfg *config.Config, llmManager llm.Service, engine ImmersiveEngine, repeatEngine RepeatEngine, collector *metrics.Collector) {
	setMetricsCollector(collector)
	registerInboundMetricsMatchers()
	registerAIHandlers(cfg, llmManager, engine, repeatEngine)
	registerRepeatHandlers(repeatEngine)
	registerLLMHandlers(llmManager)
	registerBasicHandlers(cfg, llmManager, engine, repeatEngine)
}
