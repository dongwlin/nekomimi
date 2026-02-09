package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultPath = "config.yml"

type Config struct {
	NickName      []string     `yaml:"nickname"`
	CommandPrefix string       `yaml:"command_prefix"`
	SuperUsers    []int64      `yaml:"super_users"`
	LLM           LLMConfig    `yaml:"llm"`
	Driver        DriverConfig `yaml:"driver"`
}

type LLMConfig struct {
	Enabled      bool            `yaml:"enabled"`
	Provider     string          `yaml:"provider"`
	API          string          `yaml:"api"`
	Key          string          `yaml:"key"`
	Model        string          `yaml:"model"`
	SystemPrompt string          `yaml:"system_prompt"`
	HistoryMax   int             `yaml:"history_max"`
	ContextMax   int             `yaml:"context_max"`
	Immersive    ImmersiveConfig `yaml:"immersive"`
}

type ImmersiveConfig struct {
	CooldownMinMS    int                `yaml:"cooldown_min_ms"`
	CooldownMaxMS    int                `yaml:"cooldown_max_ms"`
	CooldownBaseMS   int                `yaml:"cooldown_base_ms"`
	PrivateBaseMS    int                `yaml:"private_base_ms"`
	WindowMS         int                `yaml:"window_ms"`
	JitterMS         int                `yaml:"jitter_ms"`
	MaxBatchMessages int                `yaml:"max_batch_messages"`
	MaxBatchChars    int                `yaml:"max_batch_chars"`
	ImmediateDelayMS int                `yaml:"immediate_delay_ms"`
	MentionJudge     MentionJudgeConfig `yaml:"mention_judge"`
}

type MentionJudgeConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Model     string `yaml:"model"`
	Prompt    string `yaml:"prompt"`
	TimeoutMS int    `yaml:"timeout_ms"`
}

type DriverConfig struct {
	WebSocket WebSocketConfig `yaml:"websocket"`
}

type WebSocketConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
