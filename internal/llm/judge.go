package llm

import (
	"context"
	"errors"
	"strings"

	llmclient "github.com/dongwlin/nekomimi/internal/llm/client"
	"github.com/rs/zerolog/log"
)

type PostCooldownDecision string

const (
	DecisionReplyNow      PostCooldownDecision = "REPLY_NOW"
	DecisionCooldownShort PostCooldownDecision = "COOLDOWN_SHORT"
	DecisionCooldownLong  PostCooldownDecision = "COOLDOWN_LONG"
)

func (m *Manager) JudgeMentionImmediate(ctx context.Context, message, speaker, recent string) (bool, error) {
	if m == nil {
		return false, errors.New("LLM 未初始化")
	}
	m.mu.RLock()
	enabled := m.judgeEnabled
	provider := m.provider
	model := m.judgeModel
	if strings.TrimSpace(model) == "" {
		model = m.model
	}
	prompt := m.judgePrompt
	timeout := m.judgeTimeout
	reasoning := m.judgeReasoning
	thinking := m.judgeThinking
	m.mu.RUnlock()
	if !enabled {
		return false, nil
	}
	if strings.TrimSpace(model) == "" {
		return false, errors.New("未配置模型名")
	}
	input := buildJudgeInput(message, speaker, recent)
	if strings.TrimSpace(input) == "" {
		return false, errors.New("待判断内容为空")
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
		Source:          "mention_judge",
		ReasoningEffort: reasoning,
		ThinkingType:    thinking,
	})
	if err != nil {
		log.Warn().
			Err(err).
			Msg("mention judge request failed")
		return false, err
	}
	decision, ok := parseJudgeDecision(reply)
	if !ok {
		log.Warn().
			Str("raw_reply", strings.TrimSpace(reply)).
			Msg("mention judge returned unparsable output")
		return false, errors.New("判定结果不可解析")
	}
	log.Info().
		Bool("immediate", decision).
		Msg("mention judge completed")
	return decision, nil
}

func (m *Manager) JudgePostCooldown(ctx context.Context, message, speaker, recent string) (PostCooldownDecision, error) {
	if m == nil {
		return DecisionReplyNow, errors.New("LLM 未初始化")
	}
	m.mu.RLock()
	enabled := m.postJudgeEnabled
	provider := m.provider
	model := m.postJudgeModel
	if strings.TrimSpace(model) == "" {
		model = m.model
	}
	prompt := m.postJudgePrompt
	timeout := m.postJudgeTimeout
	reasoning := m.postJudgeReasoning
	thinking := m.postJudgeThinking
	m.mu.RUnlock()
	if !enabled {
		return DecisionReplyNow, nil
	}
	if strings.TrimSpace(model) == "" {
		return DecisionReplyNow, errors.New("未配置模型名")
	}
	input := buildPostCooldownJudgeInput(message, speaker, recent)
	if strings.TrimSpace(input) == "" {
		return DecisionReplyNow, errors.New("待判断内容为空")
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
		Source:          "post_cooldown_judge",
		ReasoningEffort: reasoning,
		ThinkingType:    thinking,
	})
	if err != nil {
		log.Warn().
			Err(err).
			Msg("post-cooldown judge request failed")
		return DecisionReplyNow, err
	}
	decision, ok := parsePostCooldownDecision(reply)
	if !ok {
		log.Warn().
			Str("raw_reply", strings.TrimSpace(reply)).
			Msg("post-cooldown judge returned unparsable output")
		return DecisionReplyNow, errors.New("判定结果不可解析")
	}
	log.Info().
		Str("decision", string(decision)).
		Msg("post-cooldown judge completed")
	return decision, nil
}

func (m *Manager) JudgeSpeakGate(ctx context.Context, message string) (bool, bool, error) {
	if m == nil {
		return false, false, errors.New("LLM 未初始化")
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
	failOpen := m.speakJudgeFailOpen
	reasoning := m.speakJudgeReasoning
	thinking := m.speakJudgeThinking
	m.mu.RUnlock()
	if !enabled {
		return false, false, nil
	}
	if strings.TrimSpace(model) == "" {
		return false, failOpen, errors.New("未配置模型名")
	}
	input := buildSpeakGateJudgeInput(message)
	if strings.TrimSpace(input) == "" {
		return false, failOpen, errors.New("待判断内容为空")
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
		return false, failOpen, err
	}
	decision, ok := parseJudgeDecision(reply)
	if !ok {
		log.Warn().
			Str("raw_reply", strings.TrimSpace(reply)).
			Msg("speak-gate judge returned unparsable output")
		return false, failOpen, errors.New("判定结果不可解析")
	}
	log.Info().Bool("should_speak", decision).Msg("speak-gate judge completed")
	return decision, true, nil
}

func buildJudgeInput(message, speaker, recent string) string {
	var builder strings.Builder
	builder.WriteString("请判断以下消息是否需要机器人立刻回复。")
	builder.WriteString("\n只输出 YES 或 NO。")
	builder.WriteString("\n\n消息内容:\n")
	builder.WriteString(strings.TrimSpace(message))
	if strings.TrimSpace(speaker) != "" {
		builder.WriteString("\n\n说话人:\n")
		builder.WriteString(strings.TrimSpace(speaker))
	}
	if strings.TrimSpace(recent) != "" {
		builder.WriteString("\n\n近期消息:\n")
		builder.WriteString(strings.TrimSpace(recent))
	}
	return strings.TrimSpace(builder.String())
}

func buildPostCooldownJudgeInput(message, speaker, recent string) string {
	var builder strings.Builder
	builder.WriteString("请基于以下对话上下文进行仲裁。")
	builder.WriteString("\n请严格只输出：REPLY_NOW、COOLDOWN_SHORT 或 COOLDOWN_LONG。")
	builder.WriteString("\n若难以判断，请倾向 COOLDOWN_SHORT。")
	builder.WriteString("\n上下文采用 YAML 风格结构：batch_meta(含 now_date/now_time 等) + transcript。")
	builder.WriteString("\n请优先依据 batch_meta 判断是否轮到机器人发言。")
	builder.WriteString("\n\n待回复内容:\n")
	builder.WriteString(strings.TrimSpace(message))
	if strings.TrimSpace(speaker) != "" {
		builder.WriteString("\n\n最后说话人:\n")
		builder.WriteString(strings.TrimSpace(speaker))
	}
	if strings.TrimSpace(recent) != "" {
		builder.WriteString("\n\n近期消息:\n")
		builder.WriteString(strings.TrimSpace(recent))
	}
	return strings.TrimSpace(builder.String())
}

func buildSpeakGateJudgeInput(message string) string {
	var builder strings.Builder
	builder.WriteString("请判断机器人是否应该在当前批次发言。")
	builder.WriteString("\n请严格只输出 YES 或 NO。")
	builder.WriteString("\n\n批次上下文:\n")
	builder.WriteString(strings.TrimSpace(message))
	return strings.TrimSpace(builder.String())
}

func parseJudgeDecision(text string) (bool, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, false
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "yes"):
		return true, true
	case strings.HasPrefix(lower, "no"):
		return false, true
	case strings.HasPrefix(trimmed, "是"):
		return true, true
	case strings.HasPrefix(trimmed, "否"):
		return false, true
	}
	if strings.Contains(lower, "yes") {
		return true, true
	}
	if strings.Contains(lower, "no") {
		return false, true
	}
	if strings.Contains(trimmed, "是") {
		return true, true
	}
	if strings.Contains(trimmed, "否") {
		return false, true
	}
	return false, false
}

func parsePostCooldownDecision(text string) (PostCooldownDecision, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(text))
	if normalized == "" {
		return "", false
	}
	switch {
	case strings.Contains(normalized, string(DecisionCooldownLong)), strings.Contains(normalized, "LONG"), strings.Contains(normalized, "长"):
		return DecisionCooldownLong, true
	case strings.Contains(normalized, string(DecisionCooldownShort)), strings.Contains(normalized, "SHORT"), strings.Contains(normalized, "短"):
		return DecisionCooldownShort, true
	case strings.Contains(normalized, string(DecisionReplyNow)), strings.Contains(normalized, "REPLY"), strings.Contains(normalized, "NOW"), strings.Contains(normalized, "立即"), strings.Contains(normalized, "回复"):
		return DecisionReplyNow, true
	}
	return "", false
}
