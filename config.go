package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type appConfig struct {
	NickName      []string `yaml:"nickname"`
	CommandPrefix string   `yaml:"command_prefix"`
	SuperUsers    []int64  `yaml:"super_users"`
	LLM           struct {
		Enabled      bool   `yaml:"enabled"`
		Provider     string `yaml:"provider"`
		API          string `yaml:"api"`
		Key          string `yaml:"key"`
		Model        string `yaml:"model"`
		SystemPrompt string `yaml:"system_prompt"`
		HistoryMax   int    `yaml:"history_max"`
		ContextMax   int    `yaml:"context_max"`
	} `yaml:"llm"`
	Driver struct {
		WebSocket struct {
			URL   string `yaml:"url"`
			Token string `yaml:"token"`
		} `yaml:"websocket"`
	} `yaml:"driver"`
}

const defaultConfigPath = "config.yml"

func loadConfig() (*appConfig, error) {
	data, err := os.ReadFile(defaultConfigPath)
	if err != nil {
		return nil, err
	}

	var cfg appConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
