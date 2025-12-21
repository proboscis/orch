package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

// GeminiAdapter handles Google Gemini CLI
type GeminiAdapter struct{}

func (a *GeminiAdapter) Type() AgentType {
	return AgentGemini
}

func (a *GeminiAdapter) IsAvailable() bool {
	cmd := exec.Command("gemini", "--version")
	return cmd.Run() == nil
}

func (a *GeminiAdapter) LaunchCommand(cfg *LaunchConfig) (string, error) {
	var args []string

	// gemini CLI with yolo mode
	args = append(args, "gemini", "--yolo")

	// Add the prompt
	if cfg.Prompt != "" {
		// Escape the prompt for shell
		escapedPrompt := strings.ReplaceAll(cfg.Prompt, "'", "'\"'\"'")
		args = append(args, "-p", fmt.Sprintf("'%s'", escapedPrompt))
	}

	return strings.Join(args, " "), nil
}

var _ Adapter = (*GeminiAdapter)(nil)
