package jsonutil

import (
	"encoding/json"
	"strings"
)

// ExtractJSONObjectCandidate attempts to locate a valid JSON object from raw
// model output that may be wrapped in markdown code fences or surrounded by
// free-form text.
func ExtractJSONObjectCandidate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "```") {
		trimmed = StripCodeFence(trimmed)
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

// StripCodeFence removes surrounding markdown code fences (``` ... ```).
func StripCodeFence(content string) string {
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
