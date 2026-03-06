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

// ReadJSONStringField extracts a string value from a pre-parsed JSON object map.
func ReadJSONStringField(payload map[string]json.RawMessage, key string) string {
	if len(payload) == 0 {
		return ""
	}
	raw, ok := payload[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

// HasJSONField reports whether the pre-parsed JSON object map contains the key.
func HasJSONField(payload map[string]json.RawMessage, key string) bool {
	if len(payload) == 0 {
		return false
	}
	_, ok := payload[key]
	return ok
}
