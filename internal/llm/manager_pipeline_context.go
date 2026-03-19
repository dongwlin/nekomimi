package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/ctxasm"
	"github.com/dongwlin/nekomimi/internal/llm/model"
)

func (m *Manager) buildPipelineMessages(ctx context.Context, assembler *ctxasm.Assembler, sessionKey string, meta ctxasm.Meta, fallbackContent string, immersiveCtx *ctxasm.ImmersiveContext) ([]model.Message, bool, error) {
	session := strings.TrimSpace(sessionKey)
	blocks, compressed, err := m.buildPipelineBlocks(ctx, assembler, session, meta, immersiveCtx)
	if err != nil {
		return nil, false, err
	}
	content := renderAssembledBlocks(blocks)
	fallback := strings.TrimSpace(fallbackContent)
	hasPersistentSource := assembler != nil && session != ""
	if !hasPersistentSource && fallback != "" {
		if strings.TrimSpace(content) == "" {
			content = fallback
		} else {
			content = fallback + "\n\n" + content
		}
	} else if strings.TrimSpace(content) == "" {
		content = fallback
	}
	if strings.TrimSpace(content) == "" {
		return nil, false, errors.New("input is empty")
	}
	return []model.Message{{Role: "user", Content: content}}, compressed, nil
}

func (m *Manager) buildPipelineBlocks(ctx context.Context, assembler *ctxasm.Assembler, sessionKey string, meta ctxasm.Meta, immersiveCtx *ctxasm.ImmersiveContext) ([]ctxasm.Block, bool, error) {
	blocks := make([]ctxasm.Block, 0, 6)
	compressed := false
	if assembler != nil && sessionKey != "" {
		assembled, err := assembler.Assemble(ctx, ctxasm.Request{
			SessionKey: sessionKey,
			Meta:       meta,
		})
		if err != nil {
			return nil, false, fmt.Errorf("assemble context: %w", err)
		}
		for _, block := range assembled.Blocks {
			if block.Truncated {
				compressed = true
			}
		}
		blocks = append(blocks, assembled.Blocks...)
	}
	blocks = append(blocks, ctxasm.RenderImmersiveBlocks(immersiveCtx)...)
	return blocks, compressed, nil
}

func renderAssembledBlocks(blocks []ctxasm.Block) string {
	if len(blocks) == 0 {
		return ""
	}

	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		header := "[" + strings.TrimSpace(block.Name) + "]"
		if block.Truncated {
			header += " (truncated)"
		}
		content := strings.TrimSpace(block.Content)
		if content == "" {
			content = "(empty)"
		}
		parts = append(parts, header+"\n"+content)
	}
	return strings.Join(parts, "\n\n")
}

func buildPipelineMeta(sessionKey, assistantSpeaker string, override ctxasm.Meta) ctxasm.Meta {
	meta := ctxasm.Meta{
		Now:               time.Now().Format("2006-01-02 15:04:05"),
		AssistantIdentity: strings.TrimSpace(assistantSpeaker),
		BotConfigNames:    extractBotConfigNames(assistantSpeaker),
		SessionType:       inferSessionType(sessionKey),
	}

	if trimmed := strings.TrimSpace(override.Now); trimmed != "" {
		meta.Now = trimmed
	}
	if trimmed := strings.TrimSpace(override.AssistantIdentity); trimmed != "" {
		meta.AssistantIdentity = trimmed
	}
	if cleaned := cleanStringSlice(override.BotConfigNames); len(cleaned) > 0 {
		meta.BotConfigNames = cleaned
	}
	if trimmed := strings.TrimSpace(strings.ToLower(override.SessionType)); trimmed != "" {
		meta.SessionType = trimmed
	}
	return meta
}

func inferSessionType(sessionKey string) string {
	session := strings.TrimSpace(strings.ToLower(sessionKey))
	if session == "" {
		return "unknown"
	}
	if strings.HasPrefix(session, "group:") {
		return "group"
	}
	if strings.HasPrefix(session, "private:") {
		return "private"
	}
	if parts := strings.SplitN(session, ":", 2); len(parts) > 1 && strings.TrimSpace(parts[0]) != "" {
		return strings.TrimSpace(parts[0])
	}
	return "unknown"
}

func extractBotConfigNames(assistantSpeaker string) []string {
	trimmed := strings.TrimSpace(assistantSpeaker)
	if trimmed == "" {
		return nil
	}
	for _, part := range strings.Split(trimmed, ";") {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(strings.ToLower(key)) != "name" {
			continue
		}
		if name := strings.TrimSpace(value); name != "" {
			return []string{name}
		}
	}
	if strings.Contains(trimmed, "=") {
		return nil
	}
	return []string{trimmed}
}

func cleanStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
