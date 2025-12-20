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

	// codex CLI with full-auto approval mode
	args = append(args, "codex", "--full-auto")

	// Add the prompt
	if cfg.Prompt != "" {
		// Escape the prompt for shell
		escapedPrompt := strings.ReplaceAll(cfg.Prompt, "'", "'\"'\"'")
		args = append(args, fmt.Sprintf("'%s'", escapedPrompt))
	}

	return strings.Join(args, " "), nil
}

var _ Adapter = (*CodexAdapter)(nil)
