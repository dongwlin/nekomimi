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
	if err := os.WriteFile(promptPath, []byte("system role prompt"), 0o644); err != nil {
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
  api: ""
  key: ""
  model: "x"
  system_prompt: "{{file:prompts/role.txt}}"
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
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
	if cfg.LLM.SystemPrompt != "system role prompt" {
		t.Fatalf("unexpected system prompt: %q", cfg.LLM.SystemPrompt)
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
  api: ""
  key: ""
  model: "x"
  system_prompt: "{{file:../outside.txt}}"
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
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
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
    poke_reaction:
      window_ms: 60000
      mild_threshold: 4
      annoyed_threshold: 8
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
}

func TestLoad_ParseImmersiveRuntimeBuffer(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
    runtime_buffer:
      max_messages: 321
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
	if cfg.LLM.Immersive.RuntimeBuffer.MaxMessages != 321 {
		t.Fatalf("unexpected runtime_buffer.max_messages: %d", cfg.LLM.Immersive.RuntimeBuffer.MaxMessages)
	}
}

func TestLoad_ParseImmersiveFlushPolicy(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
    flush_policy:
      min_batch_wait_ms: 600
      max_batch_wait_ms: 3000
      max_batch_size: 15
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
	if cfg.LLM.Immersive.FlushPolicy.MinBatchWaitMS != 600 {
		t.Fatalf("unexpected flush_policy.min_batch_wait_ms: %d", cfg.LLM.Immersive.FlushPolicy.MinBatchWaitMS)
	}
	if cfg.LLM.Immersive.FlushPolicy.MaxBatchWaitMS != 3000 {
		t.Fatalf("unexpected flush_policy.max_batch_wait_ms: %d", cfg.LLM.Immersive.FlushPolicy.MaxBatchWaitMS)
	}
	if cfg.LLM.Immersive.FlushPolicy.MaxBatchSize != 15 {
		t.Fatalf("unexpected flush_policy.max_batch_size: %d", cfg.LLM.Immersive.FlushPolicy.MaxBatchSize)
	}
}

func TestLoad_IgnoreLegacyImmersiveTimeline(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
    timeline:
      max_messages: 222
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
	if cfg.LLM.Immersive.RuntimeBuffer.MaxMessages != 0 {
		t.Fatalf("legacy timeline should not map into runtime_buffer, got %d", cfg.LLM.Immersive.RuntimeBuffer.MaxMessages)
	}
}

func TestLoad_IgnoreLegacyImmersiveContinuousSpeech(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
    continuous_speech:
      enabled: true
      min_chunk_chars: 12
      max_chunk_chars: 80
      min_interval_ms: 300
      max_interval_ms: 900
      require_stream: false
    poke_reaction:
      window_ms: 60000
      mild_threshold: 4
      annoyed_threshold: 8
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
}

func TestLoad_APIDefaultsApplied(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
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
	if cfg.API.Listen != "127.0.0.1:8080" {
		t.Fatalf("unexpected api.listen: %q", cfg.API.Listen)
	}
	if cfg.API.Auth.AccessTTLMS != 900000 {
		t.Fatalf("unexpected api.auth.access_ttl_ms: %d", cfg.API.Auth.AccessTTLMS)
	}
	if cfg.API.Auth.RefreshTTLMS != 604800000 {
		t.Fatalf("unexpected api.auth.refresh_ttl_ms: %d", cfg.API.Auth.RefreshTTLMS)
	}
	if len(cfg.API.CORS.AllowOrigins) != 1 || cfg.API.CORS.AllowOrigins[0] != "http://localhost:5173" {
		t.Fatalf("unexpected api.cors.allow_origins: %#v", cfg.API.CORS.AllowOrigins)
	}
}

func TestLoad_AllowEnabledAPIMissingPassphrase(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
driver:
  websocket:
    url: "ws://localhost:3001"
    token: "token"
api:
  enabled: true
  auth:
    passphrase: ""
    paseto_key_hex: "9f10ec4ee8ca74d6b6a6460f6609409e63d76ca4bc5f8cc86f3bd9464f694f16"
`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	if _, err := Load(configPath); err != nil {
		t.Fatalf("expected missing passphrase to be allowed, got error: %v", err)
	}
}

func TestLoad_AllowEnabledAPIMissingPasetoKey(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
driver:
  websocket:
    url: "ws://localhost:3001"
    token: "token"
api:
  enabled: true
  auth:
    passphrase: "secret"
    paseto_key_hex: ""
`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	if _, err := Load(configPath); err != nil {
		t.Fatalf("expected missing paseto key to be allowed, got error: %v", err)
	}
}

func TestLoad_RejectEnabledAPIInvalidPasetoKey(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
driver:
  websocket:
    url: "ws://localhost:3001"
    token: "token"
api:
  enabled: true
  auth:
    passphrase: "secret"
    paseto_key_hex: "not-a-valid-key"
`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected invalid paseto key error, got nil")
	}
}

func TestLoad_RejectEnabledAPINonPositiveTTL(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
driver:
  websocket:
    url: "ws://localhost:3001"
    token: "token"
api:
  enabled: true
  auth:
    passphrase: "secret"
    paseto_key_hex: "9f10ec4ee8ca74d6b6a6460f6609409e63d76ca4bc5f8cc86f3bd9464f694f16"
    access_ttl_ms: -1
`)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected non-positive access ttl error, got nil")
	}
}

func TestLoad_ParseRepeatConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
repeat:
  enabled: true
  flush_policy:
    min_batch_wait_ms: 320
    max_batch_wait_ms: 1800
    max_batch_size: 9
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
	if !cfg.Repeat.Enabled {
		t.Fatal("expected repeat.enabled to be true")
	}
	if cfg.Repeat.FlushPolicy.MinBatchWaitMS != 320 {
		t.Fatalf("unexpected repeat min_batch_wait_ms: %d", cfg.Repeat.FlushPolicy.MinBatchWaitMS)
	}
	if cfg.Repeat.FlushPolicy.MaxBatchWaitMS != 1800 {
		t.Fatalf("unexpected repeat max_batch_wait_ms: %d", cfg.Repeat.FlushPolicy.MaxBatchWaitMS)
	}
	if cfg.Repeat.FlushPolicy.MaxBatchSize != 9 {
		t.Fatalf("unexpected repeat max_batch_size: %d", cfg.Repeat.FlushPolicy.MaxBatchSize)
	}
}

func TestLoad_DefaultRepeatDisabledWhenOmitted(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := []byte(`
nickname:
  - "test"
command_prefix: "/"
super_users: []
llm:
  enabled: true
  api: ""
  key: ""
  model: "x"
  system_prompt: ""
  context_max: 1000
  context_assembly:
    recent_chat_limit: 50
    recent_diary_limit: 50
  immersive:
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
	if cfg.Repeat.Enabled {
		t.Fatal("expected repeat.enabled to default to false")
	}
}
