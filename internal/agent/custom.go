package agent

import (
	"fmt"
)

// CustomAdapter handles custom agent commands
type CustomAdapter struct{}

func (a *CustomAdapter) Type() AgentType {
	return AgentCustom
}

func (a *CustomAdapter) IsAvailable() bool {
	// Custom adapter is always "available" - the actual command check happens at runtime
	return true
}

func (a *CustomAdapter) LaunchCommand(cfg *LaunchConfig) (string, error) {
	if cfg.CustomCmd == "" {
		return "", fmt.Errorf("custom agent requires --agent-cmd")
	}
	return cfg.CustomCmd, nil
}

func (a *CustomAdapter) PromptInjection() InjectionMethod {
	return InjectionArg
}

var _ Adapter = (*CustomAdapter)(nil)
