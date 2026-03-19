package client

import (
	"encoding/json"
	"testing"
)

func TestBuildMessageParams_DisabledThinkingOmitsThinking(t *testing.T) {
	params := buildMessageParams(
		"claude-sonnet-4-6",
		nil,
		"",
		ThinkingConfig{Type: "disabled"},
		OutputConfig{Effort: "high"},
	)

	body := mustMarshalParams(t, params)
	if _, ok := body["thinking"]; ok {
		t.Fatalf("disabled thinking should omit thinking payload: %+v", body)
	}
	if _, ok := body["output_config"]; ok {
		t.Fatalf("disabled thinking should omit output_config payload: %+v", body)
	}
}

func TestBuildMessageParams_EnabledThinkingUsesBudgetTokens(t *testing.T) {
	params := buildMessageParams(
		"claude-sonnet-4-6",
		nil,
		"",
		ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
		OutputConfig{Effort: "max"},
	)

	body := mustMarshalParams(t, params)
	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("expected thinking payload, body=%+v", body)
	}
	if got := thinking["type"]; got != "enabled" {
		t.Fatalf("thinking.type mismatch: got %v, want %q", got, "enabled")
	}
	if got := thinking["budget_tokens"]; got != float64(4096) {
		t.Fatalf("thinking.budget_tokens mismatch: got %v, want %d", got, 4096)
	}
	if _, ok := body["output_config"]; ok {
		t.Fatalf("manual thinking should omit output_config payload: %+v", body)
	}
}

func TestBuildMessageParams_AdaptiveThinkingUsesOutputEffort(t *testing.T) {
	params := buildMessageParams(
		"claude-opus-4-6",
		nil,
		"",
		ThinkingConfig{Type: "adaptive"},
		OutputConfig{Effort: "high"},
	)

	body := mustMarshalParams(t, params)
	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("expected thinking payload, body=%+v", body)
	}
	if got := thinking["type"]; got != "adaptive" {
		t.Fatalf("thinking.type mismatch: got %v, want %q", got, "adaptive")
	}
	if _, ok := thinking["budget_tokens"]; ok {
		t.Fatalf("adaptive thinking should omit budget_tokens: %+v", thinking)
	}

	outputConfig, ok := body["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("expected output_config payload, body=%+v", body)
	}
	if got := outputConfig["effort"]; got != "high" {
		t.Fatalf("output_config.effort mismatch: got %v, want %q", got, "high")
	}
}

func mustMarshalParams(t *testing.T, params any) map[string]any {
	t.Helper()

	bodyBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params failed: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("unmarshal params failed: %v", err)
	}
	return body
}
