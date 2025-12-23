package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

// CodexAdapter handles OpenAI Codex CLI
type CodexAdapter struct{}

func (a *CodexAdapter) Type() AgentType {
	return AgentCodex
}

func (a *CodexAdapter) IsAvailable() bool {
	cmd := exec.Command("codex", "--version")
	return cmd.Run() == nil
}

func (a *CodexAdapter) LaunchCommand(cfg *LaunchConfig) (string, error) {
	var args []string

	// codex CLI with yolo mode
	args = append(args, "codex", "--yolo")

	// Add model if specified
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.Thinking != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%s", cfg.Thinking))
	}

	// Add the prompt
	if cfg.Prompt != "" {
		// Escape the prompt for shell
		escapedPrompt := strings.ReplaceAll(cfg.Prompt, "'", "'\"'\"'")
		args = append(args, fmt.Sprintf("'%s'", escapedPrompt))
	}

	return strings.Join(args, " "), nil
}

func (a *CodexAdapter) PromptInjection() InjectionMethod {
	return InjectionArg
}

func (a *CodexAdapter) ReadyPattern() string {
	return "" // Not needed - prompt passed via command line
}

var _ Adapter = (*CodexAdapter)(nil)
