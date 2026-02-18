package prompt

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed prompts/*.txt
var promptFS embed.FS

var (
	DefaultSystemPrompt     = mustReadPrompt("default_system.txt")
	SpeakerSystemPrompt     = mustReadPrompt("speaker_system.txt")
	SummarySystemPrompt     = mustReadPrompt("summary_system.txt")
	LightSummaryPrompt      = mustReadPrompt("light_summary.txt")
	SpeakGateJudgePrompt    = mustReadPrompt("speak_gate_judge.txt")
)

func mustReadPrompt(name string) string {
	content, err := promptFS.ReadFile("prompts/" + name)
	if err != nil {
		panic(fmt.Sprintf("read embedded prompt %q failed: %v", name, err))
	}
	return strings.TrimSpace(string(content))
}
