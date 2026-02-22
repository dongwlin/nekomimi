package llm

import (
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
	llmprompt "github.com/dongwlin/nekomimi/internal/llm/prompt"
)

func TestNewManager_SystemPromptUsesSpeakerThenDefaultWhenConfigEmpty(t *testing.T) {
	manager := NewManager(config.LLMConfig{})

	wantBase := llmprompt.SpeakerSystemPrompt
	wantSystem := composeSystemPrompt(llmprompt.SpeakerSystemPrompt, llmprompt.DefaultSystemPrompt)
	if manager.basePrompt != wantBase {
		t.Fatalf("base prompt mismatch: got %q, want %q", manager.basePrompt, wantBase)
	}
	if manager.systemPrompt != wantSystem {
		t.Fatalf("system prompt mismatch: got %q, want %q", manager.systemPrompt, wantSystem)
	}
}

func TestNewManager_SystemPromptUsesSpeakerThenCustomAndExcludesDefault(t *testing.T) {
	manager := NewManager(config.LLMConfig{SystemPrompt: "custom prompt"})

	wantBase := llmprompt.SpeakerSystemPrompt
	wantSystem := composeSystemPrompt(llmprompt.SpeakerSystemPrompt, "custom prompt")
	if manager.basePrompt != wantBase {
		t.Fatalf("base prompt mismatch: got %q, want %q", manager.basePrompt, wantBase)
	}
	if manager.systemPrompt != wantSystem {
		t.Fatalf("system prompt mismatch: got %q, want %q", manager.systemPrompt, wantSystem)
	}
}

func TestReloadConfig_SystemPromptDefaultAndCustomAreMutuallyExclusive(t *testing.T) {
	manager := NewManager(config.LLMConfig{})

	if err := manager.ReloadConfig(config.LLMConfig{SystemPrompt: "custom prompt"}); err != nil {
		t.Fatalf("reload with custom prompt failed: %v", err)
	}
	wantCustom := composeSystemPrompt(llmprompt.SpeakerSystemPrompt, "custom prompt")
	if manager.systemPrompt != wantCustom {
		t.Fatalf("custom system prompt mismatch: got %q, want %q", manager.systemPrompt, wantCustom)
	}

	if err := manager.ReloadConfig(config.LLMConfig{}); err != nil {
		t.Fatalf("reload with default prompt failed: %v", err)
	}
	wantDefault := composeSystemPrompt(llmprompt.SpeakerSystemPrompt, llmprompt.DefaultSystemPrompt)
	if manager.systemPrompt != wantDefault {
		t.Fatalf("default system prompt mismatch: got %q, want %q", manager.systemPrompt, wantDefault)
	}
}

func TestSetSystemPrompt_UsesSpeakerPlusRuntimePrompt(t *testing.T) {
	manager := NewManager(config.LLMConfig{})
	manager.SetSystemPrompt("runtime prompt")

	want := composeSystemPrompt(llmprompt.SpeakerSystemPrompt, "runtime prompt")
	if manager.systemPrompt != want {
		t.Fatalf("runtime system prompt mismatch: got %q, want %q", manager.systemPrompt, want)
	}
}
