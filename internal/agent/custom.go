package agent

import (
	"fmt"
	"os"
)

// CustomAdapter handles custom agent commands
type CustomAdapter struct{}

func (a *CustomAdapter) Type() AgentType {
	return AgentCustom
}

func (a *CustomAdapter) ProbeAvailability() Availability {
	// A custom command is part of each launch config, not the adapter, so it
	// cannot be probed until launch. Preserve the existing availability
	// contract while making that deferred check explicit in startup logs.
	return Availability{
		Agent:     AgentCustom,
		Available: true,
		Probe:     "custom command",
		ExitCode:  -1,
		Path:      os.Getenv("PATH"),
		Deferred:  true,
	}
}

func (a *CustomAdapter) LaunchCommand(cfg *LaunchConfig) (string, error) {
	if cfg.CustomCmd == "" {
		return "", fmt.Errorf("custom agent requires --agent-cmd")
	}
	return cfg.CustomCmd, nil
}

func (a *CustomAdapter) ExtraEnv() []string {
	return nil
}

func (a *CustomAdapter) PromptInjection() InjectionMethod {
	return InjectionArg
}

func (a *CustomAdapter) ReadyPattern() string {
	return "" // Not needed - prompt passed via command line
}

var _ Adapter = (*CustomAdapter)(nil)
