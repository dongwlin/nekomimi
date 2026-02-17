package commands

import (
	"github.com/dongwlin/nekomimi/internal/version"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func registerBasicHandlers() {
	zero.OnFullMatch("ping").Handle(func(ctx *zero.Ctx) {
		ctx.Send("pong")
	})
	zero.OnCommand("version").Handle(func(ctx *zero.Ctx) {
		ctx.Send("当前版本: " + version.String())
	})
}
