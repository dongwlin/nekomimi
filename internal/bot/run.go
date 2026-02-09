package bot

import (
	"github.com/dongwlin/nekomimi/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/driver"
)

func Run(cfg *config.Config) {
	zero.RunAndBlock(&zero.Config{
		NickName:      cfg.NickName,
		CommandPrefix: cfg.CommandPrefix,
		SuperUsers:    cfg.SuperUsers,
		Driver: []zero.Driver{
			driver.NewWebSocketClient(cfg.Driver.WebSocket.URL, cfg.Driver.WebSocket.Token),
		},
	}, nil)
}
