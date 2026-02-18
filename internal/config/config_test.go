package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ResolveSystemPromptFileRef(t *testing.T) {
	tmpDir := t.TempDir()

	promptPath := filepath.Join(tmpDir, "prompts", "role.txt")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatalf("mkdir prompt dir failed: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("你是测试猫娘。"), 0o644); err != nil {
		t.Fatalf("write prompt file failed: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  provider: "openai"
  api: ""
  key: ""
  model: "x"
  system_prompt: "{{file:prompts/role.txt}}"
  history_max: 10
  context_max: 1000
  immersive:
    cooldown_min_ms: 100
    cooldown_max_ms: 200
    cooldown_base_ms: 150
    private_base_ms: 100
    window_ms: 5000
    jitter_ms: 200
    max_batch_messages: 10
    max_batch_chars: 1000
    immediate_delay_ms: 100
    speak_gate:
      enabled: false
      model: ""
      prompt: ""
      timeout_ms: 1000
      fail_open: true
driver:
  websocket:
    url: "ws://localhost:3001"
    token: "token"
`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.LLM.SystemPrompt != "你是测试猫娘。" {
		t.Fatalf("unexpected system prompt: %q", cfg.LLM.SystemPrompt)
	}
}

func TestLoad_ResolveOtherPromptFileRefs(t *testing.T) {
	tmpDir := t.TempDir()

	mainPromptPath := filepath.Join(tmpDir, "prompts", "role.txt")
	speakGatePromptPath := filepath.Join(tmpDir, "prompts", "speak_gate_judge.txt")
	for _, path := range []string{mainPromptPath, speakGatePromptPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir prompt dir failed: %v", err)
		}
	}
	if err := os.WriteFile(mainPromptPath, []byte("主提示词"), 0o644); err != nil {
		t.Fatalf("write main prompt failed: %v", err)
	}
	if err := os.WriteFile(speakGatePromptPath, []byte("发言门控判定提示词"), 0o644); err != nil {
		t.Fatalf("write speak-gate prompt failed: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  provider: "openai"
  api: ""
  key: ""
  model: "x"
  system_prompt: "{{file:prompts/role.txt}}"
  history_max: 10
  context_max: 1000
  immersive:
    cooldown_min_ms: 100
    cooldown_max_ms: 200
    cooldown_base_ms: 150
    private_base_ms: 100
    window_ms: 5000
    jitter_ms: 200
    max_batch_messages: 10
    max_batch_chars: 1000
    immediate_delay_ms: 100
    speak_gate:
      enabled: true
      model: ""
      prompt: "{{file:prompts/speak_gate_judge.txt}}"
      timeout_ms: 1000
      fail_open: true
driver:
  websocket:
    url: "ws://localhost:3001"
    token: "token"
`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.LLM.SystemPrompt != "主提示词" {
		t.Fatalf("unexpected system prompt: %q", cfg.LLM.SystemPrompt)
	}
	if cfg.LLM.Immersive.SpeakGate.Prompt != "发言门控判定提示词" {
		t.Fatalf("unexpected speak gate judge prompt: %q", cfg.LLM.Immersive.SpeakGate.Prompt)
	}
}

func TestLoad_RejectSystemPromptPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	outsidePath := filepath.Join(tmpDir, "..", "outside.txt")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file failed: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  provider: "openai"
  api: ""
  key: ""
  model: "x"
  system_prompt: "{{file:../outside.txt}}"
  history_max: 10
  context_max: 1000
  immersive:
    cooldown_min_ms: 100
    cooldown_max_ms: 200
    cooldown_base_ms: 150
    private_base_ms: 100
    window_ms: 5000
    jitter_ms: 200
    max_batch_messages: 10
    max_batch_chars: 1000
    immediate_delay_ms: 100
    speak_gate:
      enabled: false
      model: ""
      prompt: ""
      timeout_ms: 1000
      fail_open: true
driver:
  websocket:
    url: "ws://localhost:3001"
    token: "token"
`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected path traversal error, got nil")
	}
}

func TestLoad_ParseImmersivePokeReaction(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  provider: "openai"
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  history_max: 10
  context_max: 1000
  immersive:
    max_batch_messages: 10
    max_batch_chars: 1000
    immediate_delay_ms: 100
    poke_reaction:
      window_ms: 60000
      mild_threshold: 4
      annoyed_threshold: 8
      max_reply_chars: 18
driver:
  websocket:
    url: "ws://localhost:3001"
    token: "token"
`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.LLM.Immersive.PokeReaction.WindowMS != 60000 {
		t.Fatalf("unexpected window_ms: %d", cfg.LLM.Immersive.PokeReaction.WindowMS)
	}
	if cfg.LLM.Immersive.PokeReaction.MildThreshold != 4 {
		t.Fatalf("unexpected mild_threshold: %d", cfg.LLM.Immersive.PokeReaction.MildThreshold)
	}
	if cfg.LLM.Immersive.PokeReaction.AnnoyedThreshold != 8 {
		t.Fatalf("unexpected annoyed_threshold: %d", cfg.LLM.Immersive.PokeReaction.AnnoyedThreshold)
	}
	if cfg.LLM.Immersive.PokeReaction.MaxReplyChars != 18 {
		t.Fatalf("unexpected max_reply_chars: %d", cfg.LLM.Immersive.PokeReaction.MaxReplyChars)
	}
}
