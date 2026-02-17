package runtime

import (
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/driver"
)

func Run(cfg *config.Config) {
	log.Info().
		Strs("nicknames", cfg.NickName).
		Str("command_prefix", cfg.CommandPrefix).
		Int("super_users", len(cfg.SuperUsers)).
		Str("ws_url", cfg.Driver.WebSocket.URL).
		Msg("bot starting")
	zero.RunAndBlock(&zero.Config{
		NickName:      cfg.NickName,
		CommandPrefix: cfg.CommandPrefix,
		SuperUsers:    cfg.SuperUsers,
		Driver: []zero.Driver{
			driver.NewWebSocketClient(cfg.Driver.WebSocket.URL, cfg.Driver.WebSocket.Token),
		},
	}, nil)
}
