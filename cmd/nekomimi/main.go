package main

import (
	"log"

	"github.com/dongwlin/nekomimi/internal/bot"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
)

func main() {
	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	llmManager := llm.NewManager(cfg.LLM)
	bot.RegisterHandlers(cfg, llmManager)
	bot.Run(cfg)
}
