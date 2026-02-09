package summarizer

import (
	"context"
	"strings"

	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
	"github.com/dongwlin/nekomimi/internal/llm/provider"
	"github.com/dongwlin/nekomimi/internal/llm/token"
	"github.com/dongwlin/nekomimi/internal/llm/model"
)

type Mode int

const (
	ModeFull Mode = iota
	ModeLight
)

type Summarizer interface {
	Summarize(ctx context.Context, modelName string, messages []model.Message) string
}

type LLMSummarizer struct {
	provider   provider.Provider
	mode       Mode
	contextMax int
}

func NewLLM(provider provider.Provider, mode Mode, contextMax int) *LLMSummarizer {
	return &LLMSummarizer{
		provider:   provider,
		mode:       mode,
		contextMax: contextMax,
	}
}

func (s *LLMSummarizer) Summarize(ctx context.Context, modelName string, messages []model.Message) string {
	if s == nil || s.provider == nil || strings.TrimSpace(modelName) == "" || len(messages) == 0 {
		return ""
	}
	conversation := formatConversation(messages, s.contextMax)
	if strings.TrimSpace(conversation) == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	input := []model.Message{{Role: "user", Content: conversation}}
	systemPrompt := llmprompt.SummarySystemPrompt
	if s.mode == ModeLight {
		systemPrompt = llmprompt.LightSummaryPrompt
	}
	summary, err := s.provider.Generate(ctx, strings.TrimSpace(modelName), systemPrompt, input)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(summary)
}

type FallbackSummarizer struct {
	maxChars int
}

func NewFallback(maxChars int) *FallbackSummarizer {
	return &FallbackSummarizer{maxChars: maxChars}
}

func (s *FallbackSummarizer) Summarize(ctx context.Context, modelName string, messages []model.Message) string {
	if s == nil {
		return ""
	}
	return buildSummary(messages, s.maxChars)
}

type Chain struct {
	primary  Summarizer
	fallback Summarizer
}

func NewChain(primary, fallback Summarizer) *Chain {
	return &Chain{primary: primary, fallback: fallback}
}

func (c *Chain) Summarize(ctx context.Context, modelName string, messages []model.Message) string {
	if c == nil {
		return ""
	}
	if c.primary != nil {
		if result := strings.TrimSpace(c.primary.Summarize(ctx, modelName, messages)); result != "" {
			return result
		}
	}
	if c.fallback != nil {
		return strings.TrimSpace(c.fallback.Summarize(ctx, modelName, messages))
	}
	return ""
}

func formatConversation(messages []model.Message, contextMax int) string {
	var builder strings.Builder
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := "用户"
		switch strings.TrimSpace(msg.Role) {
		case "assistant":
			role = "助手"
		case "system":
			role = "系统"
		}
		builder.WriteString(role)
		builder.WriteString(": ")
		builder.WriteString(content)
		builder.WriteString("\n")
	}
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return ""
	}
	if contextMax > 0 {
		limit := contextMax - 256
		if limit < 256 {
			limit = contextMax
		}
		if limit > 0 {
			result = token.TruncateToTokens(result, limit)
		}
	}
	return result
}

func buildSummary(messages []model.Message, maxChars int) string {
	if len(messages) == 0 || maxChars <= 0 {
		return ""
	}
	var builder strings.Builder
	for _, msg := range messages {
		content := compactText(msg.Content, 80)
		if content == "" {
			continue
		}
		role := "用户"
		switch strings.TrimSpace(msg.Role) {
		case "assistant":
			role = "助手"
		case "system":
			role = "系统"
		}
		entry := role + ":" + content
		if builder.Len() > 0 {
			entry = "；" + entry
		}
		if builder.Len()+len([]rune(entry)) > maxChars {
			remaining := maxChars - len([]rune(builder.String()))
			if remaining > 0 {
				builder.WriteString(limitRunes(entry, remaining))
			}
			break
		}
		builder.WriteString(entry)
	}
	return builder.String()
}

func compactText(text string, maxRunes int) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.Join(strings.Fields(clean), " ")
	return limitRunes(clean, maxRunes)
}

func limitRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

