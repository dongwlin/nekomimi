package main

import (
	"log"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/driver"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	zero.OnFullMatch("ping").
		Handle(func(ctx *zero.Ctx) {
			ctx.Send("pong")
		})

	zero.RunAndBlock(&zero.Config{
		NickName:      cfg.NickName,
		CommandPrefix: cfg.CommandPrefix,
		SuperUsers:    cfg.SuperUsers,
		Driver: []zero.Driver{
			driver.NewWebSocketClient(cfg.Driver.WebSocket.URL, cfg.Driver.WebSocket.Token),
		},
	}, nil)
}
