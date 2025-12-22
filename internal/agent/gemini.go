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
	args = append(args, "gemini", "--yolo")
	if cfg.Prompt != "" {
		args = append(args, "--prompt-interactive", doubleQuote(cfg.Prompt))
	}

	return strings.Join(args, " "), nil
}

func (a *GeminiAdapter) PromptInjection() InjectionMethod {
	return InjectionArg
}

func (a *GeminiAdapter) ReadyPattern() string {
	return "" // Prompt passed via command line.
}

var _ Adapter = (*GeminiAdapter)(nil)
