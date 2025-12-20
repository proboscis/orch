package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

// ClaudeAdapter handles Claude Code CLI
type ClaudeAdapter struct{}

func (a *ClaudeAdapter) Type() AgentType {
	return AgentClaude
}

func (a *ClaudeAdapter) IsAvailable() bool {
	cmd := exec.Command("claude", "--version")
	return cmd.Run() == nil
}

func (a *ClaudeAdapter) LaunchCommand(cfg *LaunchConfig) (string, error) {
	var args []string

	// Use --print for non-interactive mode
	args = append(args, "claude", "--dangerously-skip-permissions")

	// Add resume flag if applicable
	if cfg.Resume && cfg.SessionName != "" {
		args = append(args, "--resume", cfg.SessionName)
	}

	// Build the prompt from issue content
	if cfg.Prompt != "" {
		// Escape the prompt for shell
		escapedPrompt := strings.ReplaceAll(cfg.Prompt, "'", "'\"'\"'")
		args = append(args, "-p", fmt.Sprintf("'%s'", escapedPrompt))
	}

	return strings.Join(args, " "), nil
}

var _ Adapter = (*ClaudeAdapter)(nil)
