package agent

import (
	"fmt"
)

// AgentType represents the type of agent
type AgentType string

const (
	AgentClaude AgentType = "claude"
	AgentCodex  AgentType = "codex"
	AgentGemini AgentType = "gemini"
	AgentCustom AgentType = "custom"
)

// ParseAgentType parses an agent type string
func ParseAgentType(s string) (AgentType, error) {
	switch s {
	case "claude":
		return AgentClaude, nil
	case "codex":
		return AgentCodex, nil
	case "gemini":
		return AgentGemini, nil
	case "custom":
		return AgentCustom, nil
	default:
		return "", fmt.Errorf("unknown agent type: %s", s)
	}
}

// LaunchConfig holds configuration for launching an agent
type LaunchConfig struct {
	Type        AgentType
	CustomCmd   string // Used when Type is AgentCustom
	WorkDir     string
	IssueID     string
	RunID       string
	RunPath     string
	VaultPath   string
	Branch      string
	Prompt      string // Initial prompt/instruction for the agent
	Resume      bool   // Whether to resume an existing session
	SessionName string // For agents that support session naming
}

// Env returns the environment variables to pass to the agent
func (c *LaunchConfig) Env() []string {
	return []string{
		fmt.Sprintf("ORCH_ISSUE_ID=%s", c.IssueID),
		fmt.Sprintf("ORCH_RUN_ID=%s", c.RunID),
		fmt.Sprintf("ORCH_RUN_PATH=%s", c.RunPath),
		fmt.Sprintf("ORCH_WORKTREE_PATH=%s", c.WorkDir),
		fmt.Sprintf("ORCH_BRANCH=%s", c.Branch),
		fmt.Sprintf("ORCH_VAULT=%s", c.VaultPath),
	}
}

// Adapter defines the interface for agent adapters
type Adapter interface {
	// Type returns the agent type
	Type() AgentType

	// LaunchCommand returns the command to launch the agent
	LaunchCommand(cfg *LaunchConfig) (string, error)

	// IsAvailable checks if the agent CLI is available
	IsAvailable() bool
}

// GetAdapter returns the adapter for the given agent type
func GetAdapter(agentType AgentType) (Adapter, error) {
	switch agentType {
	case AgentClaude:
		return &ClaudeAdapter{}, nil
	case AgentCodex:
		return &CodexAdapter{}, nil
	case AgentGemini:
		return &GeminiAdapter{}, nil
	case AgentCustom:
		return &CustomAdapter{}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}
}
