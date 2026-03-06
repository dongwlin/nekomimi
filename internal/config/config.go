package config

const DefaultPath = "config/config.yml"

type Config struct {
	NickName      []string     `yaml:"nickname"`
	CommandPrefix string       `yaml:"command_prefix"`
	SuperUsers    []int64      `yaml:"super_users"`
	LLM           LLMConfig    `yaml:"llm"`
	Driver        DriverConfig `yaml:"driver"`
	API           APIConfig    `yaml:"api"`
}

type LLMConfig struct {
	Enabled         bool                  `yaml:"enabled"`
	Provider        string                `yaml:"provider"`
	API             string                `yaml:"api"`
	Key             string                `yaml:"key"`
	Model           string                `yaml:"model"`
	TimeoutMS       int                   `yaml:"timeout_ms"`
	ReasoningEffort string                `yaml:"reasoning_effort"`
	ThinkingType    string                `yaml:"thinking_type"`
	ShowReasoning   bool                  `yaml:"show_reasoning"`
	SystemPrompt    string                `yaml:"system_prompt"`
	ContextMax      int                   `yaml:"context_max"`
	ContextAssembly ContextAssemblyConfig `yaml:"context_assembly"`
	Tools           ToolsConfig           `yaml:"tools"`
	MCP             MCPConfig             `yaml:"mcp"`
	ToolLoop        ToolLoopConfig        `yaml:"tool_loop"`
	Immersive       ImmersiveConfig       `yaml:"immersive"`
}

type ContextAssemblyConfig struct {
	RecentChatLimit  int `yaml:"recent_chat_limit"`
	RecentDiaryLimit int `yaml:"recent_diary_limit"`
}

type ToolsConfig struct {
	Enabled        bool `yaml:"enabled"`
	MaxResultChars int  `yaml:"max_result_chars"`
}

type MCPConfig struct {
	Enabled          bool              `yaml:"enabled"`
	DefaultTimeoutMS int               `yaml:"default_timeout_ms"`
	MaxPayloadBytes  int               `yaml:"max_payload_bytes"`
	Servers          []MCPServerConfig `yaml:"servers"`
}

type MCPServerConfig struct {
	Name       string            `yaml:"name"`
	Transport  string            `yaml:"transport"`
	URL        string            `yaml:"url"`
	Command    string            `yaml:"command"`
	Args       []string          `yaml:"args"`
	Headers    map[string]string `yaml:"headers"`
	AllowTools []string          `yaml:"allow_tools"`
	TimeoutMS  int               `yaml:"timeout_ms"`
}

type ToolLoopConfig struct {
	MaxSteps  int `yaml:"max_steps"`
	TimeoutMS int `yaml:"timeout_ms"`
}

type ImmersiveConfig struct {
	RuntimeBuffer RuntimeBufferConfig `yaml:"runtime_buffer"`
	FlushPolicy   FlushPolicyConfig   `yaml:"flush_policy"`
	PokeReaction  PokeReactionConfig  `yaml:"poke_reaction"`
}

type RuntimeBufferConfig struct {
	MaxMessages int `yaml:"max_messages"`
}

type FlushPolicyConfig struct {
	MinBatchWaitMS int `yaml:"min_batch_wait_ms"`
	MaxBatchWaitMS int `yaml:"max_batch_wait_ms"`
	MaxBatchSize   int `yaml:"max_batch_size"`
}

type PokeReactionConfig struct {
	WindowMS         int `yaml:"window_ms"`
	MildThreshold    int `yaml:"mild_threshold"`
	AnnoyedThreshold int `yaml:"annoyed_threshold"`
}

type DriverConfig struct {
	WebSocket WebSocketConfig `yaml:"websocket"`
}

type WebSocketConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type APIConfig struct {
	Enabled bool          `yaml:"enabled"`
	Listen  string        `yaml:"listen"`
	Auth    APIAuthConfig `yaml:"auth"`
	CORS    APICORSConfig `yaml:"cors"`
}

type APIAuthConfig struct {
	Passphrase   string `yaml:"passphrase"`
	PasetoKeyHex string `yaml:"paseto_key_hex"`
	AccessTTLMS  int    `yaml:"access_ttl_ms"`
	RefreshTTLMS int    `yaml:"refresh_ttl_ms"`
}

type APICORSConfig struct {
	AllowOrigins []string `yaml:"allow_origins"`
}
