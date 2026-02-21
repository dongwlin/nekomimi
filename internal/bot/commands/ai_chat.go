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
	pokeReactions := newPokeTracker(cfg.LLM.Immersive.PokeReaction)

	zero.OnCommand("ai").Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		llmManager.SetAssistantSpeaker(assistantLabel(ctx))
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
		engine.RefreshIdentityFromCtx(ctx)
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.Send("用法: /chat on|off|status")
			return
		}
		action, rest := parseActionArgs(args)
		if strings.TrimSpace(rest) != "" {
			ctx.Send("用法: /chat on|off|status")
			return
		}
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

	zero.OnCommand("chaton", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		if !llmManager.IsEnabled() {
			ctx.Send("LLM 未开启，可使用 /llm on 开启")
			return
		}
		args := strings.TrimSpace(ctx.State["args"].(string))
		targetSession, targetLabel, err := parseTargetSessionKey(args)
		if err != nil {
			ctx.Send("用法: /chaton group <群号> | /chaton private <QQ号>\n示例: /chaton group 123456")
			return
		}
		llmManager.SetImmersive(targetSession, true)
		// 静默开启：若命令在目标会话内执行，不回任何开启文案。
		if targetSession == sessionKey(ctx) {
			return
		}
		ctx.Send("已静默开启沉浸模式: " + targetLabel)
	})

	zero.OnCommand("chatoff", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		args := strings.TrimSpace(ctx.State["args"].(string))
		targetSession, targetLabel, err := parseTargetSessionKey(args)
		if err != nil {
			ctx.Send("用法: /chatoff group <群号> | /chatoff private <QQ号>\n示例: /chatoff group 123456")
			return
		}
		llmManager.SetImmersive(targetSession, false)
		engine.Clear(targetSession)
		// 静默关闭：若命令在目标会话内执行，不回任何关闭文案。
		if targetSession == sessionKey(ctx) {
			return
		}
		ctx.Send("已静默关闭沉浸模式: " + targetLabel)
	})

	zero.OnMessage().Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
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

	zero.On("notice/notify/poke").Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		llmManager.SetAssistantSpeaker(assistantLabel(ctx))
		if ctx == nil || ctx.Event == nil || !ctx.Event.IsToMe {
			return
		}
		if !llmManager.IsEnabled() || !llmManager.IsImmersive(sessionKey(ctx)) {
			return
		}
		if ctx.Event.UserID == 0 || ctx.Event.UserID == ctx.Event.SelfID {
			return
		}
		session := sessionKey(ctx)
		actorLabel, actorName := pokeActorInfo(ctx)
		pokeCount, moodTier := pokeReactions.Observe(session, time.Now())

		engine.RecordTimelineEvent(session, actorName+"戳了你一下。", actorLabel)

		ctx.SendPoke(ctx.Event.GroupID, ctx.Event.UserID)
		engine.RecordTimelineEvent(session, "你回戳了对方。", "assistant")

		reply, err := llmManager.Reply(
			context.Background(),
			buildPokeReplyPrompt(pokeCount, moodTier),
			session,
			speakerLabel(ctx),
		)
		if err != nil {
			fallback := pokeFallbackReplies(moodTier)
			fallbackReply := fallback[rand.Intn(len(fallback))]
			sendPokeContinuousReply(ctx, cfg.LLM.Immersive.ContinuousSpeech, fallbackReply)
			engine.RecordTimelineEvent(session, fallbackReply, "assistant")
			return
		}
		reply = strings.TrimSpace(reply)
		if reply == "" {
			fallback := pokeFallbackReplies(moodTier)
			reply = fallback[0]
		}
		sendPokeContinuousReply(ctx, cfg.LLM.Immersive.ContinuousSpeech, reply)
		engine.RecordTimelineEvent(session, reply, "assistant")
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
			"当前会话上下文（新链路）:\n会话开始: %s\n估算 tokens: %d\ntoken 上限: 未设置（context_max <= 0）\n占比: 未启用\nrecent_chat: %d/%d\nrecent_diary: %d/%d\n组装字符数: %d\n截断块数: %d\n累计截断轮次: %d",
			sessionStart,
			usage.UsedTokens,
			usage.RecentChatCount,
			usage.RecentChatLimit,
			usage.RecentDiaryCount,
			usage.RecentDiaryLimit,
			usage.AssembledChars,
			usage.TruncatedBlockCount,
			usage.ContextTrimCount,
		))
		return
	}
	ctx.Send(fmt.Sprintf(
		"当前会话上下文（新链路）:\n会话开始: %s\n估算 tokens: %d/%d\n占比: %.1f%%\nrecent_chat: %d/%d\nrecent_diary: %d/%d\n组装字符数: %d\n截断块数: %d\n累计截断轮次: %d",
		sessionStart,
		usage.UsedTokens,
		usage.MaxTokens,
		usage.UsagePercent,
		usage.RecentChatCount,
		usage.RecentChatLimit,
		usage.RecentDiaryCount,
		usage.RecentDiaryLimit,
		usage.AssembledChars,
		usage.TruncatedBlockCount,
		usage.ContextTrimCount,
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

func parseTargetSessionKey(raw string) (sessionKeyValue string, label string, err error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) != 2 {
		return "", "", fmt.Errorf("invalid args")
	}
	targetType := strings.ToLower(strings.TrimSpace(fields[0]))
	targetID := strings.TrimSpace(fields[1])
	if targetID == "" || !isDigits(targetID) {
		return "", "", fmt.Errorf("invalid target id")
	}
	switch targetType {
	case "group", "grp", "g":
		return "group:" + targetID, "群聊(" + targetID + ")", nil
	case "private", "pri", "p", "user", "u":
		return "private:" + targetID, "私聊(" + targetID + ")", nil
	default:
		return "", "", fmt.Errorf("invalid target type")
	}
}

func isDigits(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return value != ""
}

func sendPokeContinuousReply(ctx *zero.Ctx, continuous config.ContinuousSpeechConfig, reply string) {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return
	}
	if !continuous.Enabled {
		ctx.Send(trimmed)
		return
	}
	chunks := splitPokeReplyChunks(trimmed, continuous.MinChunkChars, continuous.MaxChunkChars)
	if len(chunks) == 0 {
		ctx.Send(trimmed)
		return
	}
	for idx, chunk := range chunks {
		if idx > 0 {
			time.Sleep(nextPokeContinuousSpeechDelay(continuous))
		}
		ctx.Send(chunk)
	}
}

func splitPokeReplyChunks(text string, minChars, maxChars int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if minChars <= 0 {
		minChars = 12
	}
	if maxChars <= 0 {
		maxChars = 80
	}
	if maxChars < minChars {
		maxChars = minChars
	}
	chunks := make([]string, 0, 4)
	startByte := 0
	startRune := 0
	lastBoundaryByte := -1
	runeCount := 0
	for idx, r := range trimmed {
		runeCount++
		if isPokeChunkBoundary(r) {
			lastBoundaryByte = idx + len(string(r))
		}
		segmentRunes := runeCount - startRune
		if segmentRunes < minChars && segmentRunes < maxChars {
			continue
		}
		cutByte := -1
		if lastBoundaryByte > startByte && segmentRunes >= minChars {
			cutByte = lastBoundaryByte
		}
		if segmentRunes >= maxChars && cutByte < 0 {
			cutByte = idx + len(string(r))
		}
		if cutByte < 0 {
			continue
		}
		chunk := strings.TrimSpace(trimmed[startByte:cutByte])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		startByte = cutByte
		startRune = runeCount
		lastBoundaryByte = -1
	}
	rest := strings.TrimSpace(trimmed[startByte:])
	if rest != "" {
		chunks = append(chunks, rest)
	}
	return chunks
}

func isPokeChunkBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?', '.', ';', '；', '\n':
		return true
	default:
		return false
	}
}

func nextPokeContinuousSpeechDelay(cfg config.ContinuousSpeechConfig) time.Duration {
	minMS := cfg.MinIntervalMS
	maxMS := cfg.MaxIntervalMS
	if minMS <= 0 {
		minMS = 300
	}
	if maxMS <= 0 {
		maxMS = 900
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	if maxMS == minMS {
		return time.Duration(minMS) * time.Millisecond
	}
	return time.Duration(minMS+rand.Intn(maxMS-minMS+1)) * time.Millisecond
}
