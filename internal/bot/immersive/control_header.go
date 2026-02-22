package immersive

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxControlHeaderLineLen = 64
	minControlWaitMS        = 1
	maxControlWaitMS        = 3000
)

var (
	errControlHeaderTooLong           = errors.New("control header first line too long")
	errControlHeaderMissingNewline    = errors.New("control header first line missing newline")
	errControlHeaderInvalid           = errors.New("control header first line invalid")
	errControlHeaderUnexpectedContent = errors.New("control header has unexpected trailing content")
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
}

type controlHeaderParser struct {
	lineBuffer strings.Builder
	parsed     bool
	decision   controlHeaderDecision
}

func newControlHeaderParser() *controlHeaderParser {
	return &controlHeaderParser{}
}

func (p *controlHeaderParser) Consume(delta string) (controlHeaderDecision, string, bool, error) {
	if p.parsed {
		if p.decision.action == controlActionReply {
			return p.decision, delta, true, nil
		}
		if delta != "" {
			return controlHeaderDecision{}, "", false, errControlHeaderUnexpectedContent
		}
		return p.decision, "", true, nil
	}

	if delta == "" {
		return controlHeaderDecision{}, "", false, nil
	}

	p.lineBuffer.WriteString(delta)
	buffer := p.lineBuffer.String()
	newlineIdx := strings.IndexByte(buffer, '\n')
	if newlineIdx < 0 {
		if utf8.RuneCountInString(buffer) > maxControlHeaderLineLen {
			return controlHeaderDecision{}, "", false, errControlHeaderTooLong
		}
		return controlHeaderDecision{}, "", false, nil
	}

	line := strings.TrimSuffix(buffer[:newlineIdx], "\r")
	rest := buffer[newlineIdx+1:]
	if utf8.RuneCountInString(line) > maxControlHeaderLineLen {
		return controlHeaderDecision{}, "", false, errControlHeaderTooLong
	}

	decision, err := parseControlHeaderLine(line)
	if err != nil {
		return controlHeaderDecision{}, "", false, err
	}
	p.parsed = true
	p.decision = decision
	p.lineBuffer.Reset()

	if decision.action == controlActionReply {
		return decision, rest, true, nil
	}
	if rest != "" {
		return controlHeaderDecision{}, "", false, errControlHeaderUnexpectedContent
	}
	return decision, "", true, nil
}

func (p *controlHeaderParser) Finalize() (controlHeaderDecision, error) {
	if p.parsed {
		return p.decision, nil
	}
	buffer := p.lineBuffer.String()
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

func parseControlHeaderFallback(raw string) (controlHeaderDecision, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return controlHeaderDecision{}, "", false
	}

	if newlineIdx := strings.IndexAny(trimmed, "\r\n"); newlineIdx >= 0 {
		line := strings.TrimSpace(trimmed[:newlineIdx])
		body := strings.TrimLeft(trimmed[newlineIdx:], "\r\n")
		decision, err := parseControlHeaderLine(line)
		if err != nil {
			return controlHeaderDecision{}, "", false
		}
		if decision.action == controlActionReply {
			return decision, body, true
		}
		if strings.TrimSpace(body) == "" {
			return decision, "", true
		}
		return controlHeaderDecision{}, "", false
	}

	if decision, err := parseControlHeaderLine(trimmed); err == nil {
		return decision, "", true
	}

	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "REPLY") {
		remainder := strings.TrimSpace(trimmed[len("REPLY"):])
		remainder = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(remainder, ":"), "："))
		return controlHeaderDecision{action: controlActionReply}, remainder, true
	}
	if strings.HasPrefix(upper, "WAIT:") {
		remainder := strings.TrimSpace(trimmed[len("WAIT:"):])
		fields := strings.Fields(remainder)
		if len(fields) == 0 {
			return controlHeaderDecision{}, "", false
		}
		waitMS, err := strconv.Atoi(fields[0])
		if err != nil {
			return controlHeaderDecision{}, "", false
		}
		return controlHeaderDecision{
			action: controlActionWait,
			waitMS: clampControlWaitMS(waitMS),
		}, "", true
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
