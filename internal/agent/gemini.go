package agent

import (
	"strings"
)

// GeminiAdapter handles Google Gemini CLI
type GeminiAdapter struct{}

func (a *GeminiAdapter) Type() AgentType {
	return AgentGemini
}

func (a *GeminiAdapter) ProbeAvailability() Availability {
	return probeCommand(AgentGemini, "gemini", "gemini --version", "--version")
}

func (a *GeminiAdapter) LaunchCommand(cfg *LaunchConfig) (string, error) {
	var args []string

	args = append(args, "gemini")

	// Add extra args from config, or use default if not specified
	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	} else {
		// Default: yolo mode for autonomous operation
		args = append(args, "--yolo")
	}

	if cfg.Prompt != "" {
		args = append(args, "--prompt-interactive", doubleQuote(cfg.Prompt))
	}

	return strings.Join(args, " "), nil
}

func (a *GeminiAdapter) ExtraEnv() []string {
	return nil
}

func (a *GeminiAdapter) PromptInjection() InjectionMethod {
	return InjectionArg
}

func (a *GeminiAdapter) ReadyPattern() string {
	return "" // Prompt passed via command line.
}

var _ Adapter = (*GeminiAdapter)(nil)
