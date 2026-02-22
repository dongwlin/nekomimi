package jsonutil

import (
	"bytes"
	"encoding/json"
	"strings"
)

// NormalizeObjectArguments guarantees a non-empty JSON object payload.
func NormalizeObjectArguments(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return json.RawMessage(`{}`)
	}
	return raw
}

// CloneRawMessage creates a detached copy of raw JSON bytes.
func CloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}

// CompactRawMessage compacts JSON when possible and falls back to a cloned payload.
func CompactRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return CloneRawMessage(raw)
	}
	return append(json.RawMessage(nil), buf.Bytes()...)
}
