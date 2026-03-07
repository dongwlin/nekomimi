package toolloop

import (
	"encoding/json"
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
		return NDJSONItem{}, false, NewProtocolError("stream line must be a JSON object")
	}
	if !looksLikeStreamFrameJSON(trimmed) {
		return NDJSONItem{}, false, NewProtocolError("stream line must include type and matching payload")
	}

	var frame StreamMessage
	if err := json.Unmarshal([]byte(trimmed), &frame); err != nil {
		return NDJSONItem{}, false, NewProtocolError("invalid stream frame JSON")
	}
	NormalizeModelStreamFrame(&frame)
	if protocolErr := validateModelStreamFrame(frame); protocolErr != nil {
		return NDJSONItem{}, false, protocolErr
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
		return jsonutil.HasJSONField(payload, "delta")
	case string(MessageTypeToolCall):
		return jsonutil.HasJSONField(payload, "tool_call")
	case string(MessageTypeToolResult):
		return jsonutil.HasJSONField(payload, "tool_result")
	case string(MessageTypeFinal):
		return jsonutil.HasJSONField(payload, "final")
	case string(MessageTypeError):
		return jsonutil.HasJSONField(payload, "error")
	default:
		return false
	}
}
