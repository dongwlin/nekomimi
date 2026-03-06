package llm

import (
	"context"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
)

// Service defines the public contract for the LLM subsystem. External
// consumers should depend on this interface rather than on *Manager directly,
// which makes testing and future refactoring easier.
type Service interface {
	// --- generation ---

	Reply(ctx context.Context, userInput, sessionKey, speaker string) (string, error)
	ReplyStream(ctx context.Context, userInput, sessionKey, speaker string, onEvent StreamEventHandler) (string, error)
	ReplyStreamWithExtraPrompt(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler) (string, error)
	ReplyStreamWithExtraPromptAllowTools(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler, immersiveCtx *contextassemble.ImmersiveContext) (string, error)
	DecideImmersiveIntent(ctx context.Context, userInput, sessionKey, speaker string, immersiveCtx *contextassemble.ImmersiveContext) (llmintent.ControlIntent, error)

	// --- history ---

	AppendTurn(sessionKey, userInput, speaker, assistantReply string)
	AppendUserEvent(sessionKey, userInput, speaker string) (int64, bool)
	AppendUserEventAt(sessionKey, userInput, speaker string, eventTime time.Time) (int64, bool)
	AppendAssistantEvent(sessionKey, assistantReply string, replyToCutoffSeq int64) bool
	AppendAssistantEventAt(sessionKey, assistantReply string, replyToCutoffSeq int64, eventTime time.Time) bool
	ClearHistory(sessionKey string)
	ListChatEvents(sessionKey string, opts chatlog.ListOptions) (chatlog.ListResult, error)
	SessionContextUsage(sessionKey string) SessionContextUsage

	// --- configuration ---

	IsEnabled() bool
	SetEnabled(enabled bool)
	SetProvider(provider string) error
	SetModel(model string)
	SetSystemPrompt(prompt string)
	SetAssistantSpeaker(speaker string)
	SetImmersive(sessionKey string, enabled bool)
	IsImmersive(sessionKey string) bool
	ResetDefaults()
	Status() (enabled bool, provider string, model string, systemPrompt string, apiURL string)
	ReloadConfig(cfg config.LLMConfig) error
}

var _ Service = (*Manager)(nil)
