package agent

import (
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

	args = append(args, "claude")

	// Skip all permission prompts for autonomous operation
	args = append(args, "--dangerously-skip-permissions")

	// Add profile if specified
	if cfg.Profile != "" {
		args = append(args, "--profile", cfg.Profile)
	}

	// Add resume flag if applicable
	if cfg.Resume && cfg.SessionName != "" {
		args = append(args, "--resume", cfg.SessionName)
	}

	// Add prompt as positional argument (must be last, in double quotes)
	if cfg.Prompt != "" {
		args = append(args, doubleQuote(cfg.Prompt))
	}

	return strings.Join(args, " "), nil
}

// doubleQuote wraps a string in double quotes, escaping special characters
func doubleQuote(s string) string {
	// Escape backslashes, double quotes, backticks, and dollar signs
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "$", "\\$")
	return "\"" + s + "\""
}

func (a *ClaudeAdapter) PromptInjection() InjectionMethod {
	return InjectionArg
}

func (a *ClaudeAdapter) ReadyPattern() string {
	return "" // Not needed - prompt passed via command line
}

var _ Adapter = (*ClaudeAdapter)(nil)
