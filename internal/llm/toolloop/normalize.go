package toolloop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/dongwlin/nekomimi/internal/llm/jsonutil"
)

// NormalizeModelMessage makes common model deviations compatible with the
// current tool-loop message contract.
func NormalizeModelMessage(msg *Message) {
	if msg == nil {
		return
	}
	msg.Version = ""
	normalizeMessagePayload(msg.Type, msg.ToolCall, msg.Final)
}

// NormalizeModelStreamFrame makes common model deviations compatible with the
// current tool-loop stream contract.
func NormalizeModelStreamFrame(frame *StreamMessage) {
	if frame == nil {
		return
	}
	frame.Version = ""
	normalizeMessagePayload(frame.Type, frame.ToolCall, frame.Final)
}

func normalizeMessagePayload(messageType MessageType, toolCall *ToolCallPayload, final *FinalPayload) {
	switch messageType {
	case MessageTypeToolCall:
		normalizeToolCallPayload(toolCall)
	case MessageTypeFinal:
		if final != nil && final.StopReason == "" {
			final.StopReason = StopReasonFinal
		}
	}
}

func normalizeToolCallPayload(payload *ToolCallPayload) {
	if payload == nil {
		return
	}
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Arguments = normalizeToolCallArguments(payload.Arguments)
	payload.CallID = strings.TrimSpace(payload.CallID)
	if payload.CallID == "" {
		payload.CallID = fallbackToolCallID(payload.Name, payload.Arguments)
	}
}

func normalizeToolCallArguments(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`{}`)
	}
	if isJSONObjectRaw(trimmed) {
		return jsonutil.CloneRawMessage(json.RawMessage(trimmed))
	}

	// Some models stringify the JSON object inside arguments.
	var encoded string
	if err := json.Unmarshal(trimmed, &encoded); err == nil {
		decoded := strings.TrimSpace(encoded)
		if isJSONObjectRaw([]byte(decoded)) {
			return jsonutil.CloneRawMessage(json.RawMessage(decoded))
		}
	}

	return jsonutil.CloneRawMessage(json.RawMessage(trimmed))
}

func isJSONObjectRaw(raw []byte) bool {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false
	}
	_, ok := decoded.(map[string]any)
	return ok
}

func fallbackToolCallID(name string, arguments json.RawMessage) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.TrimSpace(name)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(bytes.TrimSpace(arguments))
	return fmt.Sprintf("auto_%08x", hash.Sum32())
}
