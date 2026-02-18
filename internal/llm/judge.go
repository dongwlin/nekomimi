package llm

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/rs/zerolog/log"
)

type SpeakGateAction string

const (
	SpeakGateActionReply SpeakGateAction = "REPLY"
	SpeakGateActionSkip  SpeakGateAction = "SKIP"
	SpeakGateActionWait  SpeakGateAction = "WAIT"
)

const (
	defaultSpeakGateWaitMS = 1000
	maxSpeakGateWaitMS     = 3000
)

var firstIntegerRegex = regexp.MustCompile(`\d+`)

type SpeakGateDecision struct {
	Action SpeakGateAction
	WaitMS int
}

func (d SpeakGateDecision) Normalized() SpeakGateDecision {
	switch d.Action {
	case SpeakGateActionWait:
		d.WaitMS = clampSpeakGateWaitMS(d.WaitMS)
	default:
		d.WaitMS = 0
	}
	return d
}

func clampSpeakGateWaitMS(waitMS int) int {
	if waitMS < 1 {
		return 1
	}
	if waitMS > maxSpeakGateWaitMS {
		return maxSpeakGateWaitMS
	}
	return waitMS
}

func (m *Manager) JudgeSpeakGate(ctx context.Context, message string) (SpeakGateDecision, bool, error) {
	if m == nil {
		return SpeakGateDecision{}, false, errors.New("LLM 未初始化")
	}
	m.mu.RLock()
	enabled := m.speakJudgeEnabled
	provider := m.provider
	model := m.speakJudgeModel
	if strings.TrimSpace(model) == "" {
		model = m.model
	}
	prompt := m.speakJudgePrompt
	timeout := m.speakJudgeTimeout
	reasoning := m.speakJudgeReasoning
	thinking := m.speakJudgeThinking
	m.mu.RUnlock()
	if !enabled {
		return SpeakGateDecision{}, false, nil
	}
	if strings.TrimSpace(model) == "" {
		return SpeakGateDecision{}, false, errors.New("未配置模型名")
	}
	input := buildSpeakGateJudgeInput(message)
	if strings.TrimSpace(input) == "" {
		return SpeakGateDecision{}, false, errors.New("待判断内容为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	reply, err := m.generateWithProvider(ctx, provider, model, prompt, []Message{
		{Role: "user", Content: input},
	}, llmclient.RequestOptions{
		Source:          "speak_gate_judge",
		ReasoningEffort: reasoning,
		ThinkingType:    thinking,
	})
	if err != nil {
		log.Warn().Err(err).Msg("speak-gate judge request failed")
		return SpeakGateDecision{}, false, err
	}
	decision, ok := parseSpeakGateDecision(reply)
	if !ok {
		log.Warn().
			Str("raw_reply", strings.TrimSpace(reply)).
			Msg("speak-gate judge returned unparsable output")
		return SpeakGateDecision{}, false, errors.New("判定结果不可解析")
	}
	decision = decision.Normalized()
	log.Info().
		Str("action", string(decision.Action)).
		Int("wait_ms", decision.WaitMS).
		Msg("speak-gate judge completed")
	return decision, true, nil
}

func buildSpeakGateJudgeInput(message string) string {
	var builder strings.Builder
	builder.WriteString("请判断机器人在当前批次的动作。")
	builder.WriteString("\n请严格只输出以下之一：")
	builder.WriteString("\n- REPLY")
	builder.WriteString("\n- SKIP")
	builder.WriteString("\n- WAIT:<毫秒>（毫秒必须在 1~3000）")
	builder.WriteString("\n若难以判断，优先 WAIT:1000。")
	builder.WriteString("\n\n批次上下文:\n")
	builder.WriteString(strings.TrimSpace(message))
	return strings.TrimSpace(builder.String())
}

func parseSpeakGateDecision(text string) (SpeakGateDecision, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return SpeakGateDecision{}, false
	}
	upper := strings.ToUpper(trimmed)
	switch {
	case strings.HasPrefix(upper, string(SpeakGateActionReply)),
		strings.HasPrefix(strings.ToLower(trimmed), "yes"),
		strings.HasPrefix(trimmed, "是"),
		strings.Contains(upper, "REPLY"),
		strings.Contains(upper, "NOW"),
		strings.Contains(strings.ToLower(trimmed), "yes"),
		strings.Contains(trimmed, "是"):
		return SpeakGateDecision{Action: SpeakGateActionReply}, true
	case strings.HasPrefix(upper, string(SpeakGateActionSkip)),
		strings.HasPrefix(strings.ToLower(trimmed), "no"),
		strings.HasPrefix(trimmed, "否"),
		strings.Contains(upper, "SKIP"),
		strings.Contains(strings.ToLower(trimmed), "no"),
		strings.Contains(trimmed, "否"):
		return SpeakGateDecision{Action: SpeakGateActionSkip}, true
	case strings.Contains(upper, string(SpeakGateActionWait)),
		strings.Contains(upper, "COOLDOWN"),
		strings.Contains(trimmed, "等"),
		strings.Contains(trimmed, "冷静"):
		waitMS := defaultSpeakGateWaitMS
		match := firstIntegerRegex.FindString(trimmed)
		if match != "" {
			if n, err := strconv.Atoi(match); err == nil {
				waitMS = n
			}
		}
		return SpeakGateDecision{Action: SpeakGateActionWait, WaitMS: waitMS}, true
	}
	return SpeakGateDecision{}, false
}
