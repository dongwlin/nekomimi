package bot

import (
	"github.com/dongwlin/nekomimi/internal/bot/buffer"
	bothandlers "github.com/dongwlin/nekomimi/internal/bot/handlers"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
)

func RegisterHandlers(cfg *config.Config, llmManager *llm.Manager) {
	buf := buffer.NewImmersiveBuffer(cfg.LLM.Immersive, llmManager, cfg.NickName)
	bothandlers.Register(cfg, llmManager, buf)
}
