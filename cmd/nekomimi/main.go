package main

import (
	"fmt"
	"os"
	"time"

	"github.com/dongwlin/nekomimi/internal/bot/bootstrap"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/httpapi"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/version"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	if shouldPrintVersion(os.Args[1:]) {
		fmt.Println(version.String())
		return
	}

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
		Bool("api_enabled", cfg.API.Enabled).
		Msg("config loaded")

	if cfg.API.Enabled {
		go func() {
			log.Info().
				Str("listen", cfg.API.Listen).
				Msg("http api starting")
			if err := httpapi.Run(cfg.API); err != nil {
				log.Fatal().
					Err(err).
					Msg("http api stopped")
			}
		}()
	}

	llmManager := llm.NewManager(cfg.LLM)
	log.Info().
		Msg("llm manager initialized")
	bootstrap.Start(cfg, llmManager)
}

func shouldPrintVersion(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "version", "--version", "-v":
		return true
	default:
		return false
	}
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
