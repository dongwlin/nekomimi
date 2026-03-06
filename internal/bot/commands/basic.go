package commands

import (
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/version"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func registerBasicHandlers(cfg *config.Config, llmManager llm.Service, engine ImmersiveEngine) {
	zero.OnFullMatch("ping").Handle(func(ctx *zero.Ctx) {
		sendTracked(ctx, "pong")
	})
	zero.OnCommand("version").Handle(func(ctx *zero.Ctx) {
		sendTracked(ctx, "当前版本: "+version.String())
	})
	zero.OnCommand("reload", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		reloaded, err := config.Load(config.DefaultPath)
		if err != nil {
			sendTracked(ctx, "重载配置失败: "+llm.UserVisibleError(err))
			return
		}
		if err := llmManager.ReloadConfig(reloaded.LLM); err != nil {
			sendTracked(ctx, "重载配置失败: "+llm.UserVisibleError(err))
			return
		}
		if engine != nil {
			engine.ReloadConfig(reloaded.LLM.Immersive, reloaded.NickName)
		}
		if cfg != nil {
			cfg.LLM = reloaded.LLM
			cfg.NickName = append([]string(nil), reloaded.NickName...)
			cfg.CommandPrefix = reloaded.CommandPrefix
			cfg.SuperUsers = append([]int64(nil), reloaded.SuperUsers...)
			cfg.Driver = reloaded.Driver
		}
		sendTracked(ctx, "配置已重载（保留内存会话记录）")
	})
}
