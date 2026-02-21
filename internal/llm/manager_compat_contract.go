package llm

import "context"

// ProtectedManagerContract freezes manager methods that must remain available
// through package-6 and package-7 migrations.
type ProtectedManagerContract interface {
	// ReplyStreamWithExtraPrompt streams one response with structured stream events
	// and does not append this turn into session history.
	ReplyStreamWithExtraPrompt(ctx context.Context, userInput, sessionKey, speaker, extraPrompt string, onEvent StreamEventHandler) (string, error)

	// AppendTurn appends one completed user-assistant turn with the same
	// userInput/speaker normalization used by regular reply paths.
	AppendTurn(sessionKey, userInput, speaker, assistantReply string)

	// ClearHistory clears one session history and associated usage stats.
	ClearHistory(sessionKey string)

	// SessionContextUsage returns new-pipeline context usage stats for one session.
	SessionContextUsage(sessionKey string) SessionContextUsage
}

var _ ProtectedManagerContract = (*Manager)(nil)
