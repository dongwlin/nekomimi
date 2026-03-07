package commands

import (
	"time"

	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/metrics"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type ImmersiveEngine interface {
	AnalyzeAmbientMessage(ctx *zero.Ctx, text, speaker string, isPrivate bool, at time.Time) immersivepkg.AmbientMessageMeta
	EnqueueAmbient(ctx *zero.Ctx, sessionKey string, meta immersivepkg.AmbientMessageMeta, persistedSeq int64)
	RecordEvent(sessionKey string, event immersivepkg.TimelineEvent)
	RecordTimelineEvent(sessionKey, text, speaker string)
	RecordAssistantDelivered(sessionKey, text, speaker string)
	ShouldYieldToImmersive(sessionKey string, meta immersivepkg.AmbientMessageMeta) bool
	DebugSnapshot(sessionKey string) immersivepkg.DebugSnapshot
	Clear(sessionKey string)
	RefreshIdentityFromCtx(ctx *zero.Ctx)
	ReloadConfig(cfg config.ImmersiveConfig, nicknames []string)
}

type RepeatEngine interface {
	TryRepeat(ctx *zero.Ctx, sessionKey string, meta immersivepkg.AmbientMessageMeta, assistantSpeaker string) bool
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
