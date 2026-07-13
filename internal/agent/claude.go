package agent

import (
	"fmt"
	"strings"
)

// ClaudeAdapter handles Claude Code CLI
type ClaudeAdapter struct{}

func (a *ClaudeAdapter) Type() AgentType {
	return AgentClaude
}

func (a *ClaudeAdapter) ProbeAvailability() Availability {
	return probeCommand(AgentClaude, "claude", "claude --version", "--version")
}

func (a *ClaudeAdapter) LaunchCommand(cfg *LaunchConfig) (string, error) {
	var args []string

	args = append(args, "claude")

	// Add extra args from config, or use default if not specified
	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	} else {
		// Default: skip all permission prompts for autonomous operation
		args = append(args, "--dangerously-skip-permissions")
	}

	// Route the resolved model/variant like the opencode adapter does:
	// claude exposes them as --model and --effort (low|medium|high|xhigh|max).
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.ModelVariant != "" {
		args = append(args, "--effort", cfg.ModelVariant)
	}

	// NOTE: claude has no --profile flag. Profile selection happens entirely
	// via the CLAUDE_CONFIG_DIR environment variable (LaunchConfig.Env), so
	// cfg.Profile must never reach the command line.

	// ADR-0005 R1/R5: at launch, pin the agent-native session id so the
	// transcript is addressable for reap/revive (--session-id). On revive,
	// resume that SAME id: `claude --resume <id>` appends to the existing
	// transcript — identity is stable across generations, and the CLI
	// rejects --session-id alongside --resume (it requires --fork-session,
	// which is new-conversation semantics, not revive). Measured physics:
	// docs/design/revive-physics.md.
	if cfg.Resume {
		id := strings.TrimSpace(cfg.AgentSessionID)
		if id == "" {
			return "", fmt.Errorf("claude resume requires the recorded agent session id (ADR-0005 LS5)")
		}
		args = append(args, "--resume", id)
	} else if cfg.AgentSessionID != "" {
		args = append(args, "--session-id", cfg.AgentSessionID)
	}

	// Add prompt as positional argument (must be last, in double quotes)
	if cfg.Prompt != "" {
		args = append(args, doubleQuote(cfg.Prompt))
	}

	return strings.Join(args, " "), nil
}

func (a *ClaudeAdapter) ExtraEnv() []string {
	return []string{"IS_DEMO=1"}
}

func (a *ClaudeAdapter) PromptInjection() InjectionMethod {
	return InjectionArg
}

func (a *ClaudeAdapter) ReadyPattern() string {
	return "" // Not needed - prompt passed via command line
}

var _ Adapter = (*ClaudeAdapter)(nil)
