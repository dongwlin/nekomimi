package commands

import (
	"strings"

	zero "github.com/wdvxdr1123/ZeroBot"
)

func registerRepeatHandlers(repeatEngine RepeatEngine) {
	if repeatEngine == nil {
		return
	}

	zero.OnCommand("repeat", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			sendTracked(ctx, repeatUsageText())
			return
		}

		action, rest := parseActionArgs(args)
		switch action {
		case "on":
			if strings.TrimSpace(rest) != "" {
				sendTracked(ctx, repeatUsageText())
				return
			}
			repeatEngine.SetEnabled(sessionKey(ctx), true)
			sendTracked(ctx, "复读模式已开启")
		case "off":
			if strings.TrimSpace(rest) != "" {
				sendTracked(ctx, repeatUsageText())
				return
			}
			repeatEngine.SetEnabled(sessionKey(ctx), false)
			repeatEngine.Clear(sessionKey(ctx))
			sendTracked(ctx, "复读模式已关闭")
		case "status":
			if strings.TrimSpace(rest) != "" {
				sendTracked(ctx, repeatUsageText())
				return
			}
			if repeatEngine.IsEnabled(sessionKey(ctx)) {
				sendTracked(ctx, "复读模式当前是开启状态")
				return
			}
			sendTracked(ctx, "复读模式当前是关闭状态")
		default:
			sendTracked(ctx, repeatUsageText())
		}
	})

	zero.OnCommand("repeaton", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		targetSession, targetLabel, err := parseTargetSessionKey(args)
		if err != nil {
			sendTracked(ctx, "用法: /repeaton group <群号> | /repeaton private <QQ号>\n示例: /repeaton group 123456")
			return
		}
		repeatEngine.SetEnabled(targetSession, true)
		if targetSession == sessionKey(ctx) {
			return
		}
		sendTracked(ctx, "已静默开启复读模式: "+targetLabel)
	})

	zero.OnCommand("repeatoff", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		targetSession, targetLabel, err := parseTargetSessionKey(args)
		if err != nil {
			sendTracked(ctx, "用法: /repeatoff group <群号> | /repeatoff private <QQ号>\n示例: /repeatoff group 123456")
			return
		}
		repeatEngine.SetEnabled(targetSession, false)
		repeatEngine.Clear(targetSession)
		if targetSession == sessionKey(ctx) {
			return
		}
		sendTracked(ctx, "已静默关闭复读模式: "+targetLabel)
	})
}

func repeatUsageText() string {
	return "用法: /repeat on|off|status"
}
