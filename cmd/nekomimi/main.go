package main

import (
	"os"
	"time"

	"github.com/dongwlin/nekomimi/internal/bot"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	initLogger()
	cfg, err := config.Load(config.DefaultPath)
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("load config failed")
	}
	log.Info().
		Bool("llm_enabled", cfg.LLM.Enabled).
		Str("provider", cfg.LLM.Provider).
		Str("model", cfg.LLM.Model).
		Msg("config loaded")

	llmManager := llm.NewManager(cfg.LLM)
	log.Info().
		Msg("llm manager initialized")
	bot.RegisterHandlers(cfg, llmManager)
	log.Info().
		Msg("handlers registered")
	bot.Run(cfg)
}

func initLogger() {
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "2006-01-02 15:04:05.000",
	}
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = zerolog.New(consoleWriter).With().Timestamp().Logger()
}
