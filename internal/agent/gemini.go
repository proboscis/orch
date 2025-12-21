package agent

import (
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
	// Note: We don't pass -p flag here because Gemini exits after processing
	// the prompt. Instead, the prompt is sent via tmux send-keys to keep
	// the interactive session open. See PromptInjection() method.
	args = append(args, "gemini", "--yolo")

	return strings.Join(args, " "), nil
}

func (a *GeminiAdapter) PromptInjection() InjectionMethod {
	return InjectionTmux
}

var _ Adapter = (*GeminiAdapter)(nil)
