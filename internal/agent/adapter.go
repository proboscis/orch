package agent

import (
	"fmt"
	"os"
)

// InjectionMethod specifies how the prompt should be sent to the agent
type InjectionMethod string

const (
	// InjectionArg means the prompt is passed as a command-line argument (default)
	InjectionArg InjectionMethod = "arg"
	// InjectionTmux means the prompt should be sent via tmux send-keys after the session starts
	InjectionTmux InjectionMethod = "tmux"
	// InjectionHTTP means the prompt is sent via HTTP API after the server starts
	InjectionHTTP InjectionMethod = "http"
)

// AgentType represents the type of agent
type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentGemini   AgentType = "gemini"
	AgentOpenCode AgentType = "opencode"
	AgentCustom   AgentType = "custom"
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
	case "opencode":
		return AgentOpenCode, nil
	case "custom":
		return AgentCustom, nil
	default:
		return "", fmt.Errorf("unknown agent type: %s", s)
	}
}

// LaunchConfig holds configuration for launching an agent
type LaunchConfig struct {
	Type            AgentType
	CustomCmd       string // Used when Type is AgentCustom
	WorkDir         string
	IssueID         string
	RunID           string
	RunPath         string
	VaultPath       string
	Branch          string
	Prompt          string // Initial prompt/instruction for the agent
	Resume          bool   // Whether to resume an existing session
	SessionName     string // For agents that support session naming
	Profile         string // Profile name for agents that support it (e.g., claude --profile)
	Port            int    // Port for HTTP-based agents (e.g., opencode)
	Model           string // Model in provider/model format (e.g., anthropic/claude-opus-4-5)
	ModelVariant    string // Model variant (e.g., "max" for max thinking)
	ContinueSession bool
}

// Env returns the environment variables to pass to the agent
func (c *LaunchConfig) Env() []string {
	env := []string{
		fmt.Sprintf("ORCH_ISSUE_ID=%s", c.IssueID),
		fmt.Sprintf("ORCH_RUN_ID=%s", c.RunID),
		fmt.Sprintf("ORCH_RUN_PATH=%s", c.RunPath),
		fmt.Sprintf("ORCH_WORKTREE_PATH=%s", c.WorkDir),
		fmt.Sprintf("ORCH_BRANCH=%s", c.Branch),
		fmt.Sprintf("ORCH_VAULT=%s", c.VaultPath),
	}
	// Ensure HOME is passed for OAuth credentials in ~/.claude.json
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, fmt.Sprintf("HOME=%s", home))
	}
	return env
}

// Adapter defines the interface for agent adapters
type Adapter interface {
	// Type returns the agent type
	Type() AgentType

	// LaunchCommand returns the command to launch the agent
	LaunchCommand(cfg *LaunchConfig) (string, error)

	// IsAvailable checks if the agent CLI is available
	IsAvailable() bool

	// PromptInjection returns how the prompt should be sent to the agent
	// Default implementations should return InjectionArg
	PromptInjection() InjectionMethod

	// ReadyPattern returns a regex pattern to detect when the agent is ready for input
	// The pattern is matched against the tmux pane content
	// Return empty string if no detection is needed (prompt is passed via command line)
	ReadyPattern() string
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
	case AgentOpenCode:
		return &OpenCodeAdapter{}, nil
	case AgentCustom:
		return &CustomAdapter{}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}
}
