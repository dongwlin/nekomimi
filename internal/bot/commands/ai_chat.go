package commands

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func registerAIHandlers(cfg *config.Config, llmManager *llm.Manager, engine ImmersiveEngine) {
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
				ctx.Send("LLM 未开启，可使用 /llm on 开启")
				return
			}
			llmManager.SetImmersive(sessionKey(ctx), true)
			sendTimeAwareRandomMessage(
				ctx,
				[]string{
					"好啦，我来陪你聊天啦～",
					"开好咯，直接发消息就能跟我聊",
					"这边已接通，随时戳我我都在",
					"聊天通道打开啦，来找我玩喵",
					"OK 已就位，随时可以开唠",
				},
				[]string{
					"我在呢，晚上也能陪你聊天",
					"睡前频道已接通，有话慢慢跟我说",
					"我上线了，夜里也会好好听你讲",
					"晚上也能找我聊天，想说什么就发给我",
					"今晚值班中，来陪你聊一会儿吧",
				},
			)
		case "off":
			llmManager.SetImmersive(sessionKey(ctx), false)
			engine.Clear(sessionKey(ctx))
			sendTimeAwareRandomMessage(
				ctx,
				[]string{
					"那我先退下啦，有事再喊我就来",
					"收到，这边先静音待机一会儿",
					"我先不插话啦，想聊再叫我喵",
					"好哒，先关上，有需要随时再开",
					"那我先安静待机，在旁边等你",
				},
				[]string{
					"那我先静音啦，想聊天随时再叫我",
					"收到，今晚先不打扰你休息",
					"我先下麦啦，要聊随时喊我回来",
					"先收声一下，晚点想聊也能再开",
					"先关到这儿，你有话随时可以来找我",
				},
			)
		case "status":
			if llmManager.IsImmersive(sessionKey(ctx)) {
				sendTimeAwareRandomMessage(
					ctx,
					[]string{
						"我在这儿呢，发来就能聊天",
						"这边是开着的，随时可以跟我说话",
						"在线在线，你一说话我就接喵",
						"可以直接聊，我已经准备好了",
						"随时能唠，我正看着你的消息框",
					},
					[]string{
						"我在线，夜里发消息我也能看到",
						"现在也能聊，我这边看得到你的每一句",
						"还没睡呢，你发来我就认真回你",
						"可以放心说，我这边一直是开着的",
						"我在夜间值守，有什么想说的都可以告诉我",
					},
				)
				return
			}
			sendTimeAwareRandomMessage(
				ctx,
				[]string{
					"我这会儿还没接入聊天，要聊的话开一下就好",
					"现在是待机状态，想聊我就立刻上线陪你",
					"这边暂时没开，点一下就能继续跟我聊喵",
					"我先在旁边候着，你开了我就马上来陪你",
					"现在还在休息中，叫我上班我就马上到",
				},
				[]string{
					"我现在还没开晚间陪聊，开一下就能陪你说话",
					"这会儿是待机中，你叫我我就上线陪你",
					"暂时关着呢，想聊的话一键打开就行",
					"我先在旁边候着，要聊就轻轻唤我一下",
					"夜里先休息中，开一下我就回来陪你聊天",
				},
			)
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
		engine.Enqueue(ctx, sessionKey(ctx), text, speakerLabel(ctx), isPrivate)
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

func sendRandomMessage(ctx *zero.Ctx, messages []string) {
	if len(messages) == 0 {
		return
	}
	ctx.Send(messages[rand.Intn(len(messages))])
}

func sendTimeAwareRandomMessage(ctx *zero.Ctx, dayMessages []string, nightMessages []string) {
	hour := time.Now().Hour()
	if hour >= 23 || hour < 6 {
		if len(nightMessages) > 0 {
			sendRandomMessage(ctx, nightMessages)
			return
		}
	}
	sendRandomMessage(ctx, dayMessages)
}
