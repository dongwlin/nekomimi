package intent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	ProtocolVersion = "v1"
	MinWaitMS       = 1
	MaxWaitMS       = 3000
)

type Action string

const (
	ActionSkip  Action = "skip"
	ActionWait  Action = "wait"
	ActionReply Action = "reply"
)

type ControlIntent struct {
	Version   string `json:"version"`
	Action    Action `json:"action"`
	WaitMS    int    `json:"wait_ms,omitempty"`
	Reason    string `json:"reason,omitempty"`
	ReplyPlan string `json:"reply_plan,omitempty"`
}

var (
	ErrProtocol       = errors.New("invalid control intent protocol")
	ErrInvalidPayload = errors.New("invalid control intent payload")
	ErrInvalidVersion = errors.New("invalid control intent version")
	ErrInvalidAction  = errors.New("invalid control intent action")
	ErrReasonRequired = errors.New("control intent reason is required")
	ErrWaitMSRequired = errors.New("control intent wait_ms is required")
)

// Parse decodes and validates one JSON control-intent object.
func Parse(raw string) (ControlIntent, error) {
	candidate := extractJSONObjectCandidate(raw)
	if candidate == "" {
		return ControlIntent{}, wrapProtocolError(ErrInvalidPayload)
	}

	var decoded ControlIntent
	dec := json.NewDecoder(strings.NewReader(candidate))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		return ControlIntent{}, wrapProtocolError(fmt.Errorf("%w: %v", ErrInvalidPayload, err))
	}
	if err := ensureDecoderEOF(dec); err != nil {
		return ControlIntent{}, wrapProtocolError(fmt.Errorf("%w: %v", ErrInvalidPayload, err))
	}

	Normalize(&decoded)
	if err := Validate(decoded); err != nil {
		return ControlIntent{}, wrapProtocolError(err)
	}
	return decoded, nil
}

func ensureDecoderEOF(dec *json.Decoder) error {
	var trailing any
	if err := dec.Decode(&trailing); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("unexpected trailing content")
}

// Normalize applies stable canonicalization and range rules.
func Normalize(value *ControlIntent) {
	if value == nil {
		return
	}
	value.Version = strings.ToLower(strings.TrimSpace(value.Version))
	value.Action = Action(strings.ToLower(strings.TrimSpace(string(value.Action))))
	value.Reason = strings.TrimSpace(value.Reason)
	value.ReplyPlan = strings.TrimSpace(value.ReplyPlan)
	if value.Action == ActionWait {
		if value.WaitMS != 0 {
			value.WaitMS = clampWaitMS(value.WaitMS)
		}
		return
	}
	value.WaitMS = 0
}

// Validate checks semantic constraints after normalization.
func Validate(value ControlIntent) error {
	if value.Version != ProtocolVersion {
		return ErrInvalidVersion
	}
	switch value.Action {
	case ActionSkip:
		if value.Reason == "" {
			return ErrReasonRequired
		}
		return nil
	case ActionWait:
		if value.WaitMS == 0 {
			return ErrWaitMSRequired
		}
		if value.Reason == "" {
			return ErrReasonRequired
		}
		return nil
	case ActionReply:
		return nil
	default:
		return ErrInvalidAction
	}
}

func IsProtocolError(err error) bool {
	return errors.Is(err, ErrProtocol)
}

func wrapProtocolError(err error) error {
	if err == nil {
		return ErrProtocol
	}
	return fmt.Errorf("%w: %w", ErrProtocol, err)
}

func extractJSONObjectCandidate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = stripCodeFence(trimmed)
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}

	first := strings.IndexByte(trimmed, '{')
	last := strings.LastIndexByte(trimmed, '}')
	if first >= 0 && last > first {
		candidate := strings.TrimSpace(trimmed[first : last+1])
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}
	return ""
}

func stripCodeFence(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}
	if strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			lines = lines[:i]
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func clampWaitMS(value int) int {
	if value < MinWaitMS {
		return MinWaitMS
	}
	if value > MaxWaitMS {
		return MaxWaitMS
	}
	return value
}
