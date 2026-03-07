package commands

import (
	"fmt"
	"strings"
	"time"

	immersivepkg "github.com/dongwlin/nekomimi/internal/bot/immersive"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func chatUsageText() string {
	return "用法: /chat on|off|status|debug [group <群号>|private <QQ号>]"
}

func buildChatDebugResponse(ctx *zero.Ctx, engine ImmersiveEngine, rest string) (string, bool) {
	if ctx == nil || ctx.Event == nil || !zero.SuperUserPermission(ctx) {
		return "", false
	}
	if engine == nil {
		return "沉浸引擎未初始化", true
	}

	targetSession := sessionKey(ctx)
	targetLabel := "当前会话"
	if strings.TrimSpace(rest) != "" {
		parsedSession, parsedLabel, err := parseTargetSessionKey(rest)
		if err != nil {
			return "用法: /chat debug [group <群号>|private <QQ号>]", true
		}
		targetSession = parsedSession
		targetLabel = parsedLabel
	}

	snapshot := engine.DebugSnapshot(targetSession)
	return formatImmersiveDebugSnapshot(targetLabel, snapshot, time.Now()), true
}

func formatImmersiveDebugSnapshot(label string, snapshot immersivepkg.DebugSnapshot, now time.Time) string {
	target := strings.TrimSpace(label)
	if target == "" {
		target = snapshot.SessionKey
	}
	if target == "" {
		target = "unknown"
	}
	if snapshot.UpdatedAt.IsZero() {
		return fmt.Sprintf("沉浸调试 [%s]\n暂无最近决策快照", target)
	}

	followupStatus := "否"
	switch {
	case !snapshot.PendingQuestion:
		followupStatus = "否"
	case snapshot.FollowupReady:
		followupStatus = "已到期"
	default:
		followupStatus = "等待中"
	}

	coldOpenStatus := "否"
	switch {
	case snapshot.ColdOpenWindowOpen:
		coldOpenStatus = "是"
	case !snapshot.ColdOpenEligibleAt.IsZero() && now.Before(snapshot.ColdOpenEligibleAt):
		coldOpenStatus = "冷却中"
	}

	fastRecoverStatus := "未触发"
	if snapshot.EnergyLastFastRecoverReason != "" {
		fastRecoverStatus = "已触发(" + snapshot.EnergyLastFastRecoverReason + ")"
	}

	lastProactive := "-"
	if snapshot.LastProactiveKind != "" || snapshot.LastProactiveStatus != "" || snapshot.LastProactiveReason != "" {
		lastProactive = strings.Trim(strings.Join([]string{
			strings.TrimSpace(snapshot.LastProactiveKind),
			strings.TrimSpace(snapshot.LastProactiveStatus),
			strings.TrimSpace(snapshot.LastProactiveReason),
		}, "/"), "/")
	}

	return strings.Join([]string{
		fmt.Sprintf("沉浸调试 [%s]", target),
		fmt.Sprintf("更新时间: %s", formatDebugTime(snapshot.UpdatedAt)),
		fmt.Sprintf("状态: %s", valueOrDash(snapshot.ConversationMode)),
		fmt.Sprintf("焦点: %s", valueOrDash(snapshot.FocusSpeaker)),
		fmt.Sprintf("能量: %d / target %d / baseline %d (%s, fast_recover=%s)",
			snapshot.EnergyValue,
			snapshot.EnergyTarget,
			snapshot.EnergyBaseline,
			valueOrDash(snapshot.EnergyLastDeltaReason),
			fastRecoverStatus,
		),
		fmt.Sprintf("信号: score=%d band=%s features=%s",
			snapshot.LastSignalScore,
			valueOrDash(snapshot.LastSignalBand),
			valueOrDash(snapshot.LastSignalFeatures),
		),
		fmt.Sprintf("调度: %s / %s / %dms",
			valueOrDash(snapshot.LastSchedulerReason),
			valueOrDash(snapshot.LastSchedulerPriority),
			snapshot.LastSchedulerDelayMS,
		),
		fmt.Sprintf("续问: pending=%t due=%s budget=%d ready=%s",
			snapshot.PendingQuestion,
			formatDebugTime(snapshot.FollowupDueAt),
			snapshot.FollowupBudget,
			followupStatus,
		),
		fmt.Sprintf("冷开场: eligible=%s next=%s ignored=%d last=%s",
			coldOpenStatus,
			formatDebugTime(snapshot.ColdOpenEligibleAt),
			snapshot.IgnoredProactiveCount,
			lastProactive,
		),
		fmt.Sprintf("控制: action=%s wait=%dms reason_code=%s reason=%s",
			valueOrDash(snapshot.LastControlAction),
			snapshot.LastControlWaitMS,
			valueOrDash(snapshot.LastControlReasonCode),
			valueOrDash(snapshot.LastControlReason),
		),
		fmt.Sprintf("最终: action=%s reason_code=%s reason=%s",
			valueOrDash(snapshot.LastFinalAction),
			valueOrDash(snapshot.LastFinalReasonCode),
			valueOrDash(snapshot.LastFinalReason),
		),
		fmt.Sprintf("强呼叫延迟: %s", formatLatency(snapshot.LastStrongCallLatencyMS)),
		fmt.Sprintf("主动提问判断: followup=%s cold_open=%s", followupStatus, coldOpenStatus),
		fmt.Sprintf("回复预览: %s", valueOrDash(safeCommandPreview(snapshot.LastReplyPreview))),
	}, "\n")
}

func formatDebugTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.Format("2006-01-02 15:04:05")
}

func valueOrDash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	return trimmed
}

func formatLatency(latencyMS int64) string {
	if latencyMS < 0 {
		return "-"
	}
	return fmt.Sprintf("%dms", latencyMS)
}

func safeCommandPreview(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= 160 {
		return text
	}
	return string(runes[:160]) + "..."
}
