package commands

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func registerAIHandlers(cfg *config.Config, llmManager llm.Service, engine ImmersiveEngine, repeatEngine RepeatEngine) {
	pokeReactions := newPokeTracker(cfg.LLM.Immersive.PokeReaction)

	zero.OnCommand("ai").Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		llmManager.SetAssistantSpeaker(assistantLabel(ctx))
		prompt := strings.TrimSpace(ctx.State["args"].(string))
		if prompt == "" {
			sendTracked(ctx, "用法: /ai 你的问题")
			return
		}
		if !llmManager.IsEnabled() {
			sendTracked(ctx, "LLM未开启，可使用 /llm on 开启")
			return
		}
		reply, err := llmManager.Reply(context.Background(), prompt, sessionKey(ctx), speakerLabel(ctx))
		if err != nil {
			sendTracked(ctx, "LLM调用失败: "+llm.UserVisibleError(err))
			return
		}
		sendTracked(ctx, reply)
	})

	zero.OnCommand("ctx", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		sendContextUsage(ctx, llmManager)
	})

	zero.OnCommand("chat", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			sendTracked(ctx, chatUsageText())
			return
		}
		action, rest := parseActionArgs(args)
		switch action {
		case "on":
			if strings.TrimSpace(rest) != "" {
				sendTracked(ctx, chatUsageText())
				return
			}
			if !llmManager.IsEnabled() {
				sendTracked(ctx, "LLM 未开启，可使用 /llm on 开启")
				return
			}
			llmManager.SetImmersive(sessionKey(ctx), true)
			sendTimeAwareRandomMessage(
				ctx,
				[]string{"沉浸模式已开启，直接发消息就可以和我聊。"},
				[]string{"沉浸模式已开启，夜里也会陪你聊。"},
			)
		case "off":
			if strings.TrimSpace(rest) != "" {
				sendTracked(ctx, chatUsageText())
				return
			}
			llmManager.SetImmersive(sessionKey(ctx), false)
			engine.Clear(sessionKey(ctx))
			sendTimeAwareRandomMessage(
				ctx,
				[]string{"沉浸模式已关闭，需要时再叫我。"},
				[]string{"沉浸模式已关闭，先不打扰你休息。"},
			)
		case "status":
			if strings.TrimSpace(rest) != "" {
				sendTracked(ctx, chatUsageText())
				return
			}
			if llmManager.IsImmersive(sessionKey(ctx)) {
				sendTimeAwareRandomMessage(
					ctx,
					[]string{"沉浸模式当前是开启状态。"},
					[]string{"沉浸模式当前是开启状态。"},
				)
				return
			}
			sendTimeAwareRandomMessage(
				ctx,
				[]string{"沉浸模式当前是关闭状态。"},
				[]string{"沉浸模式当前是关闭状态。"},
			)
		case "debug":
			response, ok := buildChatDebugResponse(ctx, engine, rest)
			if !ok {
				return
			}
			sendTracked(ctx, response)
		default:
			sendTracked(ctx, chatUsageText())
		}
	})

	zero.OnCommand("chaton", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		if !llmManager.IsEnabled() {
			sendTracked(ctx, "LLM 未开启，可使用 /llm on 开启")
			return
		}
		args := strings.TrimSpace(ctx.State["args"].(string))
		targetSession, targetLabel, err := parseTargetSessionKey(args)
		if err != nil {
			sendTracked(ctx, "用法: /chaton group <群号> | /chaton private <QQ号>\n示例: /chaton group 123456")
			return
		}
		llmManager.SetImmersive(targetSession, true)
		if targetSession == sessionKey(ctx) {
			return
		}
		sendTracked(ctx, "已静默开启沉浸模式: "+targetLabel)
	})

	zero.OnCommand("chatoff", zero.SuperUserPermission).Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		args := strings.TrimSpace(ctx.State["args"].(string))
		targetSession, targetLabel, err := parseTargetSessionKey(args)
		if err != nil {
			sendTracked(ctx, "用法: /chatoff group <群号> | /chatoff private <QQ号>\n示例: /chatoff group 123456")
			return
		}
		llmManager.SetImmersive(targetSession, false)
		engine.Clear(targetSession)
		if targetSession == sessionKey(ctx) {
			return
		}
		sendTracked(ctx, "已静默关闭沉浸模式: "+targetLabel)
	})

	zero.OnMessage().Handle(func(ctx *zero.Ctx) {
		handleAmbientMessage(cfg, llmManager, engine, repeatEngine, ctx)
	})

	zero.On("notice/notify/poke").Handle(func(ctx *zero.Ctx) {
		engine.RefreshIdentityFromCtx(ctx)
		assistantSpeaker := assistantLabel(ctx)
		llmManager.SetAssistantSpeaker(assistantSpeaker)
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

		engine.RecordEvent(session, immersivepkg.NewPokeNoticeEvent(actorLabel, actorName, "inbound", time.Now()))

		sendPokeTracked(ctx, ctx.Event.GroupID, ctx.Event.UserID)
		engine.RecordEvent(session, immersivepkg.NewAssistantActionEvent(assistantSpeaker, "send_poke", time.Now(), map[string]string{
			"target_name": actorName,
		}))

		recordReply := func(rawReply string) {
			finalReply, segments := normalizeSegmentedReply(rawReply)
			if finalReply == "" {
				return
			}
			sendPokeReplySegments(ctx, segments)
			_ = llmManager.AppendAssistantEvent(session, finalReply, 0)
			engine.RecordEvent(session, immersivepkg.NewAssistantTextEvent(finalReply, assistantSpeaker, time.Now()))
		}

		reply, err := llmManager.ReplyStreamWithExtraPrompt(
			context.Background(),
			"",
			session,
			"",
			buildPokeReplyPrompt(pokeCount, moodTier),
			nil,
		)
		if err != nil {
			fallback := pokeFallbackReplies(moodTier)
			fallbackReply := fallback[rand.Intn(len(fallback))]
			recordReply(fallbackReply)
			return
		}
		reply = strings.TrimSpace(reply)
		if reply == "" {
			fallback := pokeFallbackReplies(moodTier)
			reply = fallback[0]
		}
		recordReply(reply)
	})
}

type ambientLLMState interface {
	IsEnabled() bool
	IsImmersive(sessionKey string) bool
	AppendUserEventWithMetadataAt(sessionKey, userInput, speaker string, eventTime time.Time, metadata map[string]string) (int64, bool)
}

func handleAmbientMessage(cfg *config.Config, llmState ambientLLMState, engine ImmersiveEngine, repeatEngine RepeatEngine, ctx *zero.Ctx) {
	if ctx == nil {
		return
	}
	if engine != nil {
		engine.RefreshIdentityFromCtx(ctx)
	}

	text := strings.TrimSpace(ctx.ExtractPlainText())
	if text == "" {
		return
	}
	if cfg != nil && strings.HasPrefix(text, cfg.CommandPrefix) {
		return
	}

	session := sessionKey(ctx)
	isPrivate := ctx.Event != nil && ctx.Event.DetailType == "private"
	speaker := speakerLabel(ctx)
	assistantSpeaker := assistantLabel(ctx)
	repeatEnabled := repeatEngine != nil && repeatEngine.IsEnabled(session)
	immersiveEnabled := engine != nil && llmState != nil && llmState.IsEnabled() && llmState.IsImmersive(session)

	if !repeatEnabled && !immersiveEnabled {
		return
	}

	now := time.Now()
	meta := immersivepkg.NewAmbientMessageMeta(text, speaker, isPrivate, now)
	if engine != nil {
		meta = engine.AnalyzeAmbientMessage(ctx, text, speaker, isPrivate, now)
	}

	var persistedSeq int64
	if llmState != nil {
		if seq, ok := llmState.AppendUserEventWithMetadataAt(session, meta.Text, meta.Speaker, meta.At, meta.HistoryMetadata()); ok {
			persistedSeq = seq
		}
	}

	if isPrivate {
		log.Info().
			Str("session", session).
			Str("reason", "private_session").
			Msg("ambient router chose immersive")
		if immersiveEnabled {
			engine.EnqueueAmbient(ctx, session, meta, persistedSeq)
		}
		return
	}

	if repeatEnabled && engine != nil && engine.ShouldYieldToImmersive(session, meta) {
		log.Info().
			Str("session", session).
			Bool("mention", meta.MentionBot).
			Bool("directed_question", meta.DirectedQuestion).
			Int("nick_pos", int(meta.NicknamePosition)).
			Str("reason", "addressed_to_bot_or_active_thread").
			Msg("ambient router chose immersive")
		if immersiveEnabled {
			engine.EnqueueAmbient(ctx, session, meta, persistedSeq)
		}
		return
	}

	if repeatEnabled && repeatEngine.TryRepeat(ctx, session, meta, assistantSpeaker) {
		log.Info().
			Str("session", session).
			Str("reason", "repeat_hit").
			Msg("ambient router chose repeat")
		return
	}

	if immersiveEnabled {
		log.Info().
			Str("session", session).
			Str("reason", "repeat_miss_or_disabled").
			Msg("ambient router chose immersive")
		engine.EnqueueAmbient(ctx, session, meta, persistedSeq)
	}
}

func sendContextUsage(ctx *zero.Ctx, llmManager llm.Service) {
	if !llmManager.IsEnabled() {
		sendTracked(ctx, "LLM未开启，可使用 /llm on 开启")
		return
	}
	usage := llmManager.SessionContextUsage(sessionKey(ctx))
	sessionStart := "未开始"
	if !usage.SessionStartedAt.IsZero() {
		sessionStart = usage.SessionStartedAt.Format("2006-01-02 15:04:05")
	}
	if usage.MaxTokens <= 0 {
		sendTracked(ctx, fmt.Sprintf(
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
	sendTracked(ctx, fmt.Sprintf(
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
	sendTracked(ctx, messages[rand.Intn(len(messages))])
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

func normalizeSegmentedReply(reply string) (string, []string) {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return "", nil
	}
	segments := immersivepkg.SplitReplySegments(trimmed)
	if len(segments) == 0 {
		segments = []string{trimmed}
	}
	finalReply := strings.TrimSpace(strings.Join(segments, "\n"))
	if finalReply == "" {
		return "", nil
	}
	return finalReply, segments
}

func sendPokeReplySegments(ctx *zero.Ctx, segments []string) {
	if len(segments) == 0 {
		return
	}
	for idx, segment := range segments {
		if idx > 0 {
			time.Sleep(immersivepkg.NextReplySegmentDelay(segment))
		}
		sendTracked(ctx, segment)
	}
}
