package bot

import (
	"github.com/dongwlin/nekomimi/internal/config"
	bothandlers "github.com/dongwlin/nekomimi/internal/bot/handlers"
	"github.com/dongwlin/nekomimi/internal/llm"
)

func RegisterHandlers(cfg *config.Config, llmManager *llm.Manager) {
	buffer := NewImmersiveBuffer(cfg.LLM.Immersive, llmManager, cfg.NickName)
	bothandlers.Register(cfg, llmManager, buffer)
}
