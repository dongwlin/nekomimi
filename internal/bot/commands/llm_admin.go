package commands

import (
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func registerLLMHandlers(llmManager *llm.Manager) {
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
}
