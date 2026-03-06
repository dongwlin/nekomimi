package toolloop

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm/jsonutil"
)

type NDJSONItem struct {
	Frame *StreamMessage
	Text  string
}

type NDJSONParser struct {
	buffer string
}

func NewNDJSONParser() *NDJSONParser {
	return &NDJSONParser{}
}

func (p *NDJSONParser) Feed(chunk string) ([]NDJSONItem, error) {
	if p == nil {
		return nil, nil
	}
	p.buffer += chunk
	return p.drain(false)
}

func (p *NDJSONParser) Flush() ([]NDJSONItem, error) {
	if p == nil {
		return nil, nil
	}
	return p.drain(true)
}

func (p *NDJSONParser) drain(flush bool) ([]NDJSONItem, error) {
	if p == nil {
		return nil, nil
	}

	items := make([]NDJSONItem, 0, 4)
	for {
		index := strings.IndexByte(p.buffer, '\n')
		if index < 0 {
			break
		}
		line := p.buffer[:index]
		p.buffer = p.buffer[index+1:]
		item, ok, err := parseNDJSONLine(line)
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, item)
		}
	}

	if flush && p.buffer != "" {
		line := p.buffer
		p.buffer = ""
		item, ok, err := parseNDJSONLine(line)
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func parseNDJSONLine(line string) (item NDJSONItem, ok bool, err error) {
	raw := strings.TrimRight(line, "\r")
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return NDJSONItem{}, false, nil
	}
	if !json.Valid([]byte(trimmed)) {
		return NDJSONItem{Text: raw}, true, nil
	}
	if !looksLikeStreamFrameJSON(trimmed) {
		return NDJSONItem{Text: raw}, true, nil
	}

	var frame StreamMessage
	if err := json.Unmarshal([]byte(trimmed), &frame); err != nil {
		return NDJSONItem{}, false, fmt.Errorf("invalid ndjson frame: %w", err)
	}
	NormalizeModelStreamFrame(&frame)
	if protocolErr := validateModelStreamFrame(frame); protocolErr != nil {
		return NDJSONItem{}, false, fmt.Errorf("invalid stream frame: %s", protocolErr.Message)
	}
	return NDJSONItem{Frame: &frame}, true, nil
}

func looksLikeStreamFrameJSON(candidate string) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
		return false
	}

	switch frameType := strings.TrimSpace(jsonutil.ReadJSONStringField(payload, "type")); frameType {
	case string(MessageTypeDelta):
		return jsonutil.HasJSONField(payload, "delta") || jsonutil.HasJSONField(payload, "version")
	case string(MessageTypeToolCall):
		return jsonutil.HasJSONField(payload, "tool_call") || jsonutil.HasJSONField(payload, "version")
	case string(MessageTypeToolResult):
		return jsonutil.HasJSONField(payload, "tool_result") || jsonutil.HasJSONField(payload, "version")
	case string(MessageTypeFinal):
		return jsonutil.HasJSONField(payload, "final") || jsonutil.HasJSONField(payload, "version")
	case string(MessageTypeError):
		return jsonutil.HasJSONField(payload, "error") || jsonutil.HasJSONField(payload, "version")
	default:
		return false
	}
}
