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
    mention_judge:
      enabled: false
      model: ""
      prompt: ""
      timeout_ms: 1000
    post_cooldown_judge:
      enabled: false
      model: ""
      prompt: ""
      timeout_ms: 1000
      short_wait_ms: 1000
      long_wait_ms: 2000
      max_rounds: 3
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
	mentionPromptPath := filepath.Join(tmpDir, "prompts", "mention_judge.txt")
	postPromptPath := filepath.Join(tmpDir, "prompts", "post_cooldown_judge.txt")
	for _, path := range []string{mainPromptPath, mentionPromptPath, postPromptPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir prompt dir failed: %v", err)
		}
	}
	if err := os.WriteFile(mainPromptPath, []byte("主提示词"), 0o644); err != nil {
		t.Fatalf("write main prompt failed: %v", err)
	}
	if err := os.WriteFile(mentionPromptPath, []byte("提及判定提示词"), 0o644); err != nil {
		t.Fatalf("write mention prompt failed: %v", err)
	}
	if err := os.WriteFile(postPromptPath, []byte("冷静期后判定提示词"), 0o644); err != nil {
		t.Fatalf("write post prompt failed: %v", err)
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
    mention_judge:
      enabled: true
      model: ""
      prompt: "{{file:prompts/mention_judge.txt}}"
      timeout_ms: 1000
    post_cooldown_judge:
      enabled: true
      model: ""
      prompt: "{{file:prompts/post_cooldown_judge.txt}}"
      timeout_ms: 1000
      short_wait_ms: 1000
      long_wait_ms: 2000
      max_rounds: 3
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
	if cfg.LLM.Immersive.MentionJudge.Prompt != "提及判定提示词" {
		t.Fatalf("unexpected mention judge prompt: %q", cfg.LLM.Immersive.MentionJudge.Prompt)
	}
	if cfg.LLM.Immersive.PostCooldownJudge.Prompt != "冷静期后判定提示词" {
		t.Fatalf("unexpected post cooldown judge prompt: %q", cfg.LLM.Immersive.PostCooldownJudge.Prompt)
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
    mention_judge:
      enabled: false
      model: ""
      prompt: ""
      timeout_ms: 1000
    post_cooldown_judge:
      enabled: false
      model: ""
      prompt: ""
      timeout_ms: 1000
      short_wait_ms: 1000
      long_wait_ms: 2000
      max_rounds: 3
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
