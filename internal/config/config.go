package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultPath = "config.yml"

var systemPromptFileRefPattern = regexp.MustCompile(`\{\{\s*file:([^\}]+)\s*\}\}`)

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
	CooldownMinMS     int                     `yaml:"cooldown_min_ms"`
	CooldownMaxMS     int                     `yaml:"cooldown_max_ms"`
	CooldownBaseMS    int                     `yaml:"cooldown_base_ms"`
	PrivateBaseMS     int                     `yaml:"private_base_ms"`
	WindowMS          int                     `yaml:"window_ms"`
	JitterMS          int                     `yaml:"jitter_ms"`
	MaxBatchMessages  int                     `yaml:"max_batch_messages"`
	MaxBatchChars     int                     `yaml:"max_batch_chars"`
	ImmediateDelayMS  int                     `yaml:"immediate_delay_ms"`
	MentionJudge      MentionJudgeConfig      `yaml:"mention_judge"`
	PostCooldownJudge PostCooldownJudgeConfig `yaml:"post_cooldown_judge"`
}

type MentionJudgeConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Model     string `yaml:"model"`
	Prompt    string `yaml:"prompt"`
	TimeoutMS int    `yaml:"timeout_ms"`
}

type PostCooldownJudgeConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Model       string `yaml:"model"`
	Prompt      string `yaml:"prompt"`
	TimeoutMS   int    `yaml:"timeout_ms"`
	ShortWaitMS int    `yaml:"short_wait_ms"`
	LongWaitMS  int    `yaml:"long_wait_ms"`
	MaxRounds   int    `yaml:"max_rounds"`
	FailOpen    bool   `yaml:"fail_open"`
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
	return &cfg, nil
}

func resolvePromptFields(cfg *Config, rootDir string) error {
	resolved, err := resolveSystemPromptFileRefs(cfg.LLM.SystemPrompt, rootDir)
	if err != nil {
		return fmt.Errorf("resolve llm.system_prompt failed: %w", err)
	}
	cfg.LLM.SystemPrompt = resolved

	resolved, err = resolveSystemPromptFileRefs(cfg.LLM.Immersive.MentionJudge.Prompt, rootDir)
	if err != nil {
		return fmt.Errorf("resolve llm.immersive.mention_judge.prompt failed: %w", err)
	}
	cfg.LLM.Immersive.MentionJudge.Prompt = resolved

	resolved, err = resolveSystemPromptFileRefs(cfg.LLM.Immersive.PostCooldownJudge.Prompt, rootDir)
	if err != nil {
		return fmt.Errorf("resolve llm.immersive.post_cooldown_judge.prompt failed: %w", err)
	}
	cfg.LLM.Immersive.PostCooldownJudge.Prompt = resolved
	return nil
}

func resolveSystemPromptFileRefs(systemPrompt, rootDir string) (string, error) {
	matches := systemPromptFileRefPattern.FindAllStringSubmatchIndex(systemPrompt, -1)
	if len(matches) == 0 {
		return systemPrompt, nil
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		refStart, refEnd := match[2], match[3]

		builder.WriteString(systemPrompt[last:fullStart])

		ref := strings.TrimSpace(systemPrompt[refStart:refEnd])
		content, err := readSystemPromptRefFile(rootDir, ref)
		if err != nil {
			return "", err
		}
		builder.WriteString(content)
		last = fullEnd
	}
	builder.WriteString(systemPrompt[last:])

	return builder.String(), nil
}

func readSystemPromptRefFile(rootDir, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("empty file reference")
	}
	if filepath.IsAbs(ref) {
		return "", fmt.Errorf("absolute path is not allowed: %q", ref)
	}

	cleanRef := filepath.Clean(ref)
	targetPath := filepath.Join(rootDir, cleanRef)
	if isWithinRoot(rootDir, targetPath) {
		if content, err := readFileIfExists(targetPath); err == nil {
			return content, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("read file %q failed: %w", ref, err)
		}
	}

	candidates, err := searchPromptRefCandidates(rootDir, cleanRef)
	if err != nil {
		return "", err
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("file reference %q not found under config root %q", ref, rootDir)
	case 1:
		content, readErr := os.ReadFile(candidates[0])
		if readErr != nil {
			return "", fmt.Errorf("read file %q failed: %w", ref, readErr)
		}
		return strings.TrimSpace(string(content)), nil
	default:
		relPaths := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			rel, relErr := filepath.Rel(rootDir, candidate)
			if relErr != nil {
				relPaths = append(relPaths, filepath.ToSlash(candidate))
				continue
			}
			relPaths = append(relPaths, filepath.ToSlash(rel))
		}
		return "", fmt.Errorf("file reference %q is ambiguous, matched: %s", ref, strings.Join(relPaths, ", "))
	}
}

func readFileIfExists(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func searchPromptRefCandidates(rootDir, cleanRef string) ([]string, error) {
	refSlash := filepath.ToSlash(cleanRef)
	baseName := filepath.Base(cleanRef)
	candidates := make([]string, 0, 4)

	walkErr := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == refSlash || strings.HasSuffix(relSlash, "/"+refSlash) || filepath.Base(path) == baseName {
			candidates = append(candidates, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("search file reference %q failed: %w", cleanRef, walkErr)
	}
	sort.Strings(candidates)
	return uniqueStrings(candidates), nil
}

func isWithinRoot(rootDir, path string) bool {
	rel, err := filepath.Rel(rootDir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func uniqueStrings(items []string) []string {
	if len(items) <= 1 {
		return items
	}
	out := make([]string, 0, len(items))
	var last string
	for i, item := range items {
		if i == 0 || item != last {
			out = append(out, item)
			last = item
		}
	}
	return out
}
