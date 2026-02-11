package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func registerAIHandlers(cfg *config.Config, llmManager *llm.Manager, buffer ImmersiveBuffer) {
	zero.OnCommand("ai").Handle(func(ctx *zero.Ctx) {
		prompt := strings.TrimSpace(ctx.State["args"].(string))
		if prompt == "" {
			ctx.Send("用法: /ai 你的问题")
			return
		}
		if !llmManager.IsEnabled() {
			ctx.Send("LLM未开启，可使用 /llm on 开启")
			return
		}
		reply, err := llmManager.Reply(context.Background(), prompt, sessionKey(ctx), speakerLabel(ctx))
		if err != nil {
			ctx.Send("LLM调用失败: " + llm.UserVisibleError(err))
			return
		}
		ctx.Send(reply)
	})

	zero.OnCommand("ctx", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		sendContextUsage(ctx, llmManager)
	})

	zero.OnCommand("chat", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.Send("用法: /chat on|off|status")
			return
		}
		action, _ := parseActionArgs(args)
		switch action {
		case "on":
			if !llmManager.IsEnabled() {
				ctx.Send("LLM未开启，可使用 /llm on 开启")
				return
			}
			llmManager.SetImmersive(sessionKey(ctx), true)
			ctx.Send("沉浸模式已开启，直接发消息即可对话（/chat off 关闭）")
		case "off":
			llmManager.SetImmersive(sessionKey(ctx), false)
			buffer.Clear(sessionKey(ctx))
			ctx.Send("沉浸模式已关闭")
		case "status":
			status := "关闭"
			if llmManager.IsImmersive(sessionKey(ctx)) {
				status = "开启"
			}
			ctx.Send("沉浸模式状态: " + status)
		default:
			ctx.Send("用法: /chat on|off|status")
		}
	})

	zero.OnMessage().Handle(func(ctx *zero.Ctx) {
		if !llmManager.IsEnabled() || !llmManager.IsImmersive(sessionKey(ctx)) {
			return
		}
		text := strings.TrimSpace(ctx.ExtractPlainText())
		if text == "" {
			return
		}
		if strings.HasPrefix(text, cfg.CommandPrefix) {
			return
		}
		isPrivate := ctx.Event != nil && ctx.Event.DetailType == "private"
		buffer.Enqueue(ctx, sessionKey(ctx), text, speakerLabel(ctx), isPrivate)
	})
}

func sendContextUsage(ctx *zero.Ctx, llmManager *llm.Manager) {
	if !llmManager.IsEnabled() {
		ctx.Send("LLM未开启，可使用 /llm on 开启")
		return
	}
	usage := llmManager.SessionContextUsage(sessionKey(ctx))
	sessionStart := "未开始"
	if !usage.SessionStartedAt.IsZero() {
		sessionStart = usage.SessionStartedAt.Format("2006-01-02 15:04:05")
	}
	if usage.MaxTokens <= 0 {
		ctx.Send(fmt.Sprintf(
			"当前会话上下文估算:\n会话开始: %s\n已使用: %d tokens\n上限: 未设置（context_max <= 0）\n占比: 未启用\n历史消息: %d 条\n压缩次数: %d（历史: %d, 上下文: %d）",
			sessionStart,
			usage.UsedTokens,
			usage.MessageCount,
			usage.TotalCompressCount,
			usage.HistoryCompressCount,
			usage.ContextCompressCount,
		))
		return
	}
	ctx.Send(fmt.Sprintf(
		"当前会话上下文估算:\n会话开始: %s\n已使用: %d/%d tokens\n占比: %.1f%%\n历史消息: %d 条\n压缩次数: %d（历史: %d, 上下文: %d）",
		sessionStart,
		usage.UsedTokens,
		usage.MaxTokens,
		usage.UsagePercent,
		usage.MessageCount,
		usage.TotalCompressCount,
		usage.HistoryCompressCount,
		usage.ContextCompressCount,
	))
}
