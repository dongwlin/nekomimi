package config

const DefaultPath = "config/config.yml"

type Config struct {
	NickName      []string     `yaml:"nickname"`
	CommandPrefix string       `yaml:"command_prefix"`
	SuperUsers    []int64      `yaml:"super_users"`
	LLM           LLMConfig    `yaml:"llm"`
	Driver        DriverConfig `yaml:"driver"`
}

type LLMConfig struct {
	Enabled         bool            `yaml:"enabled"`
	Provider        string          `yaml:"provider"`
	API             string          `yaml:"api"`
	Key             string          `yaml:"key"`
	Model           string          `yaml:"model"`
	ReasoningEffort string          `yaml:"reasoning_effort"`
	ShowReasoning   bool            `yaml:"show_reasoning"`
	SystemPrompt    string          `yaml:"system_prompt"`
	HistoryMax      int             `yaml:"history_max"`
	ContextMax      int             `yaml:"context_max"`
	Immersive       ImmersiveConfig `yaml:"immersive"`
}

type ImmersiveConfig struct {
	CooldownMinMS     int                     `yaml:"cooldown_min_ms"`
	CooldownMaxMS     int                     `yaml:"cooldown_max_ms"`
	CooldownBaseMS    int                     `yaml:"cooldown_base_ms"`
	PrivateBaseMS     int                     `yaml:"private_base_ms"`
	WindowMS          int                     `yaml:"window_ms"`
	JitterMS          int                     `yaml:"jitter_ms"`
	MaxBatchMessages  int                     `yaml:"max_batch_messages"`
	MaxBatchChars     int                     `yaml:"max_batch_chars"`
	ImmediateDelayMS  int                     `yaml:"immediate_delay_ms"`
	SpeakGate         SpeakGateConfig         `yaml:"speak_gate"`
	MentionJudge      MentionJudgeConfig      `yaml:"mention_judge"`
	PostCooldownJudge PostCooldownJudgeConfig `yaml:"post_cooldown_judge"`
}

type SpeakGateConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Model           string `yaml:"model"`
	Prompt          string `yaml:"prompt"`
	TimeoutMS       int    `yaml:"timeout_ms"`
	ReasoningEffort string `yaml:"reasoning_effort"`
	FailOpen        bool   `yaml:"fail_open"`
}

type MentionJudgeConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Model           string `yaml:"model"`
	Prompt          string `yaml:"prompt"`
	TimeoutMS       int    `yaml:"timeout_ms"`
	ReasoningEffort string `yaml:"reasoning_effort"`
}

type PostCooldownJudgeConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Model           string `yaml:"model"`
	Prompt          string `yaml:"prompt"`
	TimeoutMS       int    `yaml:"timeout_ms"`
	ReasoningEffort string `yaml:"reasoning_effort"`
	ShortWaitMS     int    `yaml:"short_wait_ms"`
	LongWaitMS      int    `yaml:"long_wait_ms"`
	MaxRounds       int    `yaml:"max_rounds"`
	FailOpen        bool   `yaml:"fail_open"`
}

type DriverConfig struct {
	WebSocket WebSocketConfig `yaml:"websocket"`
}

type WebSocketConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}
