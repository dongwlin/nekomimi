package main

import (
	"fmt"
	"os"
	"time"

	"github.com/dongwlin/nekomimi/internal/bot/bootstrap"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/httpapi"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/dongwlin/nekomimi/internal/version"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	processStartedAt := time.Now()

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

	llmManager := llm.NewManager(cfg.LLM)
	log.Info().
		Msg("llm manager initialized")

	var collector *metrics.Collector
	if cfg.API.Enabled {
		var err error
		collector, err = metrics.NewCollector(httpapi.AuthSQLitePath)
		if err != nil {
			log.Fatal().
				Err(err).
				Msg("initialize metrics collector failed")
		}
		defer func() {
			if closeErr := collector.Close(); closeErr != nil {
				log.Error().Err(closeErr).Msg("close metrics collector failed")
			}
		}()
		collector.SetProcessStartedAt(processStartedAt)

		go func() {
			log.Info().
				Str("listen", cfg.API.Listen).
				Msg("http api starting")
			if err := httpapi.Run(cfg.API, httpapi.RunOptions{
				Metrics: collector,
				LLMStatusProvider: func() metrics.LLMStatus {
					enabled, provider, model, _, _ := llmManager.Status()
					return metrics.LLMStatus{
						Enabled:  enabled,
						Provider: provider,
						Model:    model,
					}
				},
			}); err != nil {
				log.Fatal().
					Err(err).
					Msg("http api stopped")
			}
		}()
	}

	bootstrap.Start(cfg, llmManager, collector)
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
