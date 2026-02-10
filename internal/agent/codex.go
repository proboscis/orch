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

	args = append(args, "codex")

	// Add extra args from config, or use default if not specified
	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	} else {
		// Default: yolo mode for autonomous operation
		args = append(args, "--yolo")
	}

	// Codex CLI expects model IDs like "gpt-5.3-codex"; normalize provider/model
	// inputs from orch config by stripping the provider prefix when present.
	if model := codexModelName(cfg.Model); model != "" {
		args = append(args, "--model", shellQuote(model))
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

func codexModelName(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 && parts[0] != "" {
		model = strings.TrimSpace(parts[1])
	}
	return model
}

var _ Adapter = (*CodexAdapter)(nil)
