package immersive

import (
	"errors"
	"strings"

	llmintent "github.com/dongwlin/nekomimi/internal/llm/intent"
)

type controlAction string

const (
	controlActionSkip  controlAction = "skip"
	controlActionWait  controlAction = "wait"
	controlActionReply controlAction = "reply"
)

type controlDecision struct {
	action controlAction
	waitMS int
	reason string
}

func decisionFromIntent(value llmintent.ControlIntent) controlDecision {
	decision := controlDecision{
		waitMS: value.WaitMS,
		reason: strings.TrimSpace(value.Reason),
	}
	switch value.Action {
	case llmintent.ActionSkip:
		decision.action = controlActionSkip
	case llmintent.ActionWait:
		decision.action = controlActionWait
	case llmintent.ActionReply:
		decision.action = controlActionReply
	}
	return decision
}

func isControlIntentProtocolError(err error) bool {
	return errors.Is(err, llmintent.ErrProtocol)
}
