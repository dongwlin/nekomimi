package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path failed: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	rootDir := filepath.Dir(absPath)
	if err := resolvePromptFields(&cfg, rootDir); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func resolvePromptFields(cfg *Config, rootDir string) error {
	resolved, err := resolveSystemPromptFileRefs(cfg.LLM.SystemPrompt, rootDir)
	if err != nil {
		return fmt.Errorf("resolve llm.system_prompt failed: %w", err)
	}
	cfg.LLM.SystemPrompt = resolved
	return nil
}
