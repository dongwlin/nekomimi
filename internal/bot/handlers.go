package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/version"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func RegisterHandlers(cfg *config.Config, llmManager *llm.Manager) {
	buffer := NewImmersiveBuffer(cfg.LLM.Immersive, llmManager, cfg.NickName)
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

	sendContextUsage := func(ctx *zero.Ctx) {
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
	zero.OnCommand("ctx", zero.SuperUserPermission).Handle(sendContextUsage)

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

	zero.OnCommand("llm", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.Send("用法: /llm on|off|status|provider <responses|openai|gemini>|model <name>|prompt <text>|reset|clear")
			return
		}
		action, rest := parseActionArgs(args)
		switch action {
		case "on":
			llmManager.SetEnabled(true)
			ctx.Send("LLM已开启")
		case "off":
			llmManager.SetEnabled(false)
			ctx.Send("LLM已关闭")
		case "status":
			enabled, provider, model, systemPrompt, apiURL := llmManager.Status()
			status := "关闭"
			if enabled {
				status = "开启"
			}
			if strings.TrimSpace(provider) == "" {
				provider = "(未设置)"
			}
			if strings.TrimSpace(model) == "" {
				model = "(未设置)"
			}
			if strings.TrimSpace(systemPrompt) == "" {
				systemPrompt = "(未设置)"
			}
			ctx.Send("LLM状态: " + status + "\n提供方: " + provider + "\n模型: " + model + "\nAPI: " + apiURL + "\n系统提示词: " + systemPrompt)
		case "provider":
			if strings.TrimSpace(rest) == "" {
				ctx.Send("用法: /llm provider <responses|openai|gemini>")
				return
			}
			if err := llmManager.SetProvider(rest); err != nil {
				ctx.Send("更新提供方失败: " + llm.UserVisibleError(err))
				return
			}
			ctx.Send("已更新提供方: " + rest)
		case "model":
			if strings.TrimSpace(rest) == "" {
				ctx.Send("用法: /llm model <name>")
				return
			}
			llmManager.SetModel(rest)
			ctx.Send("已更新模型: " + rest)
		case "prompt":
			if strings.TrimSpace(rest) == "" {
				ctx.Send("用法: /llm prompt <text>")
				return
			}
			llmManager.SetSystemPrompt(rest)
			ctx.Send("已更新系统提示词")
		case "reset":
			llmManager.ResetDefaults()
			ctx.Send("LLM配置已重置为配置文件默认值")
		case "clear":
			llmManager.ClearHistory(sessionKey(ctx))
			ctx.Send("已清空当前会话的对话历史")
		default:
			ctx.Send("用法: /llm on|off|status|provider <responses|openai|gemini>|model <name>|prompt <text>|reset|clear")
		}
	})

	zero.OnFullMatch("ping").Handle(func(ctx *zero.Ctx) {
		ctx.Send("pong")
	})
	zero.OnCommand("version").Handle(func(ctx *zero.Ctx) {
		ctx.Send("当前版本: " + version.String())
	})
}

func parseActionArgs(args string) (string, string) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", ""
	}
	fields := strings.Fields(args)
	action := strings.ToLower(fields[0])
	rest := strings.TrimSpace(args[len(fields[0]):])
	return action, rest
}

func sessionKey(ctx *zero.Ctx) string {
	if ctx == nil || ctx.Event == nil {
		return "global"
	}
	if ctx.Event.DetailType == "guild" {
		userID := strings.TrimSpace(ctx.Event.TinyID)
		if userID == "" {
			userID = fmt.Sprintf("%d", ctx.Event.UserID)
		}
		return "guild:" + ctx.Event.GuildID + ":" + ctx.Event.ChannelID
	}
	if ctx.Event.DetailType == "private" {
		return fmt.Sprintf("private:%d", ctx.Event.UserID)
	}
	return fmt.Sprintf("group:%d", ctx.Event.GroupID)
}

func speakerLabel(ctx *zero.Ctx) string {
	if ctx == nil || ctx.Event == nil {
		return ""
	}
	if ctx.Event.DetailType == "private" {
		return ""
	}
	name, speakerID := speakerNameAndID(ctx)
	if strings.TrimSpace(name) == "" {
		if strings.TrimSpace(speakerID) == "" {
			return "user"
		}
		return "id=" + speakerID
	}
	if strings.TrimSpace(speakerID) == "" {
		return "name=" + name
	}
	return "name=" + name + ";id=" + speakerID
}

func speakerNameAndID(ctx *zero.Ctx) (string, string) {
	if ctx == nil || ctx.Event == nil {
		return "", ""
	}
	var name string
	var speakerID string
	if ctx.Event.Sender != nil {
		name = strings.TrimSpace(ctx.Event.Sender.AnonymousName)
		if name == "" {
			name = strings.TrimSpace(ctx.Event.Sender.Card)
		}
		if name == "" {
			name = strings.TrimSpace(ctx.Event.Sender.NickName)
		}
		if ctx.Event.Sender.TinyID != "" {
			speakerID = strings.TrimSpace(ctx.Event.Sender.TinyID)
		} else if ctx.Event.Sender.ID != 0 {
			speakerID = fmt.Sprintf("%d", ctx.Event.Sender.ID)
		}
	}
	if speakerID == "" {
		speakerID = strings.TrimSpace(ctx.Event.TinyID)
	}
	if speakerID == "" && ctx.Event.UserID != 0 {
		speakerID = fmt.Sprintf("%d", ctx.Event.UserID)
	}
	return name, speakerID
}
