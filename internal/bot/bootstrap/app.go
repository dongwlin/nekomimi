package bootstrap

import (
	"time"

	"github.com/dongwlin/nekomimi/internal/bot/commands"
	"github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/bot/repeat"
	"github.com/dongwlin/nekomimi/internal/bot/runtime"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/metrics"
)

// Start wires runtime dependencies and starts the bot.
func Start(cfg *config.Config, llmManager llm.Service, collector *metrics.Collector) {
	engine := immersive.NewEngine(cfg.LLM.Immersive, llmManager, cfg.NickName)
	engine.SetMetricsCollector(collector)
	repeatEngine := repeat.NewEngine(cfg.Repeat, llmManager, engine)
	repeatEngine.SetMetricsCollector(collector)
	commands.Register(cfg, llmManager, engine, repeatEngine, collector)
	if collector != nil {
		collector.SetBotStartedAt(time.Now())
	}
	runtime.Run(cfg)
}
