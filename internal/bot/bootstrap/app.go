package bootstrap

import (
	"github.com/dongwlin/nekomimi/internal/bot/commands"
	"github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/bot/runtime"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
)

// Start wires runtime dependencies and starts the bot.
func Start(cfg *config.Config, llmManager *llm.Manager) {
	engine := immersive.NewEngine(cfg.LLM.Immersive, llmManager, cfg.NickName)
	commands.Register(cfg, llmManager, engine)
	runtime.Run(cfg)
}
