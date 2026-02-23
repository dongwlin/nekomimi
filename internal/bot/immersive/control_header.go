package immersive

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxControlHeaderLineLen = 64
	maxControlReasonLineLen = 200
	minControlWaitMS        = 1
	maxControlWaitMS        = 3000
)

var (
	errControlHeaderTooLong           = errors.New("control header first line too long")
	errControlHeaderMissingNewline    = errors.New("control header first line missing newline")
	errControlHeaderInvalid           = errors.New("control header first line invalid")
	errControlHeaderUnexpectedContent = errors.New("control header has unexpected trailing content")
	errControlHeaderReasonMissing     = errors.New("control header reason line missing")
	errControlHeaderReasonInvalid     = errors.New("control header reason line invalid")
	errControlHeaderReasonTooLong     = errors.New("control header reason line too long")
)

type controlAction string

const (
	controlActionSkip  controlAction = "skip"
	controlActionWait  controlAction = "wait"
	controlActionReply controlAction = "reply"
)

type controlHeaderDecision struct {
	action controlAction
	waitMS int
	reason string
}

type controlHeaderParser struct {
	lineBuffer       strings.Builder
	parsed           bool
	expectingReason  bool
	decision         controlHeaderDecision
	pendingFirstLine controlHeaderDecision
}

func newControlHeaderParser() *controlHeaderParser {
	return &controlHeaderParser{}
}

func (p *controlHeaderParser) Consume(delta string) (controlHeaderDecision, string, bool, error) {
	if p.parsed {
		if p.decision.action == controlActionReply {
			return p.decision, delta, true, nil
		}
		if strings.TrimSpace(delta) != "" {
			return controlHeaderDecision{}, "", false, errControlHeaderUnexpectedContent
		}
		return p.decision, "", true, nil
	}

	if delta == "" {
		return controlHeaderDecision{}, "", false, nil
	}

	p.lineBuffer.WriteString(delta)
	for {
		buffer := p.lineBuffer.String()
		newlineIdx := strings.IndexByte(buffer, '\n')
		if newlineIdx < 0 {
			limitErr := errControlHeaderTooLong
			lineLimit := maxControlHeaderLineLen
			if p.expectingReason {
				limitErr = errControlHeaderReasonTooLong
				lineLimit = maxControlReasonLineLen
			}
			if utf8.RuneCountInString(buffer) > lineLimit {
				return controlHeaderDecision{}, "", false, limitErr
			}
			return controlHeaderDecision{}, "", false, nil
		}

		line := strings.TrimSuffix(buffer[:newlineIdx], "\r")
		rest := buffer[newlineIdx+1:]
		p.lineBuffer.Reset()
		p.lineBuffer.WriteString(rest)

		if p.expectingReason {
			reason, err := parseControlReasonLine(line)
			if err != nil {
				return controlHeaderDecision{}, "", false, err
			}
			decision := p.pendingFirstLine
			decision.reason = reason
			p.pendingFirstLine = controlHeaderDecision{}
			p.expectingReason = false
			p.parsed = true
			p.decision = decision

			trailing := p.lineBuffer.String()
			p.lineBuffer.Reset()
			if strings.TrimSpace(trailing) != "" {
				return controlHeaderDecision{}, "", false, errControlHeaderUnexpectedContent
			}
			return decision, "", true, nil
		}

		if utf8.RuneCountInString(line) > maxControlHeaderLineLen {
			return controlHeaderDecision{}, "", false, errControlHeaderTooLong
		}
		decision, err := parseControlHeaderLine(line)
		if err != nil {
			return controlHeaderDecision{}, "", false, err
		}
		if decision.action == controlActionReply {
			p.parsed = true
			p.decision = decision
			body := p.lineBuffer.String()
			p.lineBuffer.Reset()
			return decision, body, true, nil
		}

		p.expectingReason = true
		p.pendingFirstLine = decision
	}
}

func (p *controlHeaderParser) Finalize() (controlHeaderDecision, error) {
	if p.parsed {
		return p.decision, nil
	}

	buffer := p.lineBuffer.String()
	if p.expectingReason {
		if buffer == "" {
			return controlHeaderDecision{}, errControlHeaderReasonMissing
		}
		reason, err := parseControlReasonLine(strings.TrimSuffix(buffer, "\r"))
		if err != nil {
			return controlHeaderDecision{}, err
		}
		decision := p.pendingFirstLine
		decision.reason = reason
		p.pendingFirstLine = controlHeaderDecision{}
		p.expectingReason = false
		p.parsed = true
		p.decision = decision
		p.lineBuffer.Reset()
		return decision, nil
	}

	if buffer == "" {
		return controlHeaderDecision{}, errControlHeaderMissingNewline
	}
	if utf8.RuneCountInString(buffer) > maxControlHeaderLineLen {
		return controlHeaderDecision{}, errControlHeaderTooLong
	}
	return controlHeaderDecision{}, errControlHeaderMissingNewline
}

func parseControlHeaderLine(line string) (controlHeaderDecision, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return controlHeaderDecision{}, errControlHeaderInvalid
	}

	upper := strings.ToUpper(trimmed)
	switch {
	case upper == "SKIP":
		return controlHeaderDecision{action: controlActionSkip}, nil
	case upper == "REPLY":
		return controlHeaderDecision{action: controlActionReply}, nil
	case strings.HasPrefix(upper, "WAIT:"):
		value := strings.TrimSpace(trimmed[len("WAIT:"):])
		waitMS, err := strconv.Atoi(value)
		if err != nil {
			return controlHeaderDecision{}, errControlHeaderInvalid
		}
		return controlHeaderDecision{
			action: controlActionWait,
			waitMS: clampControlWaitMS(waitMS),
		}, nil
	default:
		return controlHeaderDecision{}, errControlHeaderInvalid
	}
}

func parseControlReasonLine(line string) (string, error) {
	if utf8.RuneCountInString(line) > maxControlReasonLineLen {
		return "", errControlHeaderReasonTooLong
	}
	reason := strings.TrimSpace(line)
	if reason == "" {
		return "", errControlHeaderReasonInvalid
	}
	return reason, nil
}

func parseControlHeaderFallback(raw string) (controlHeaderDecision, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return controlHeaderDecision{}, "", false
	}

	if newlineIdx := strings.IndexAny(trimmed, "\r\n"); newlineIdx >= 0 {
		line := strings.TrimSpace(trimmed[:newlineIdx])
		remainder := strings.TrimLeft(trimmed[newlineIdx:], "\r\n")
		decision, err := parseControlHeaderLine(line)
		if err != nil {
			return controlHeaderDecision{}, "", false
		}
		if decision.action == controlActionReply {
			return decision, remainder, true
		}

		reasonLine := remainder
		rest := ""
		if reasonIdx := strings.IndexAny(remainder, "\r\n"); reasonIdx >= 0 {
			reasonLine = remainder[:reasonIdx]
			rest = strings.TrimLeft(remainder[reasonIdx:], "\r\n")
		}
		reason, err := parseControlReasonLine(reasonLine)
		if err != nil {
			return controlHeaderDecision{}, "", false
		}
		if strings.TrimSpace(rest) != "" {
			return controlHeaderDecision{}, "", false
		}
		decision.reason = reason
		return decision, "", true
	}

	if decision, err := parseControlHeaderLine(trimmed); err == nil {
		if decision.action != controlActionReply {
			return controlHeaderDecision{}, "", false
		}
		return decision, "", true
	}

	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "REPLY") {
		remainder := strings.TrimSpace(trimmed[len("REPLY"):])
		remainder = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(remainder, ":"), "："))
		return controlHeaderDecision{action: controlActionReply}, remainder, true
	}
	return controlHeaderDecision{}, "", false
}

func clampControlWaitMS(waitMS int) int {
	if waitMS < minControlWaitMS {
		return minControlWaitMS
	}
	if waitMS > maxControlWaitMS {
		return maxControlWaitMS
	}
	return waitMS
}
