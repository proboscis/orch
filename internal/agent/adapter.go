package agent

import (
	"fmt"
	"os"
	"strings"
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
	IssuesRoot      string
	Branch          string
	Prompt          string // Initial prompt/instruction for the agent
	Resume          bool   // Whether to resume an existing session
	SessionName     string // For agents that support session naming
	Profile         string // Resolved execution profile name (recorded for display; no agent CLI consumes it directly)
	Port            int    // Port for HTTP-based agents (e.g., opencode)
	Model           string // Model in provider/model format (e.g., anthropic/claude-opus-4-5)
	ModelVariant    string // Model variant (e.g., "max" for max thinking)
	ContinueSession bool
	ExtraArgs       []string // Additional CLI arguments from config
	CodexHome       string   // CODEX_HOME for codex auth isolation; empty = agent default (~/.codex). Leading ~ expands to $HOME.
	ClaudeConfigDir string   // CLAUDE_CONFIG_DIR for claude auth isolation; empty = agent default (~/.claude). Leading ~ expands to $HOME.
}

// Env returns the environment variables to pass to the agent
func (c *LaunchConfig) Env() []string {
	env := []string{
		fmt.Sprintf("ORCH_ISSUE_ID=%s", c.IssueID),
		fmt.Sprintf("ORCH_RUN_ID=%s", c.RunID),
		fmt.Sprintf("ORCH_RUN_PATH=%s", c.RunPath),
		fmt.Sprintf("ORCH_WORKTREE_PATH=%s", c.WorkDir),
		fmt.Sprintf("ORCH_BRANCH=%s", c.Branch),
	}
	// Ensure HOME is passed for OAuth credentials in ~/.claude.json
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, fmt.Sprintf("HOME=%s", home))
	}
	// Inject the profile auth directories when a profile selects one. The ~
	// expansion happens HERE, on the execution host, so master-resolved
	// profiles stay portable across hosts with different HOMEs.
	env = append(env, c.CodexHomeEnv()...)
	env = append(env, c.ClaudeConfigDirEnv()...)
	return env
}

// CodexHomeEnv returns the CODEX_HOME environment entry for the configured codex
// auth directory, or an empty slice when none is set. A leading ~ expands to
// $HOME. Empty injects nothing, which is safe for non-codex agents.
func (c *LaunchConfig) CodexHomeEnv() []string {
	if codexHome := expandHomePath(c.CodexHome); codexHome != "" {
		return []string{fmt.Sprintf("CODEX_HOME=%s", codexHome)}
	}
	return nil
}

// ClaudeConfigDirEnv returns the CLAUDE_CONFIG_DIR environment entry for the
// configured claude profile directory, or an empty slice when none is set. A
// leading ~ expands to $HOME. Empty injects nothing, which is safe for
// non-claude agents.
func (c *LaunchConfig) ClaudeConfigDirEnv() []string {
	if configDir := expandHomePath(c.ClaudeConfigDir); configDir != "" {
		return []string{fmt.Sprintf("CLAUDE_CONFIG_DIR=%s", configDir)}
	}
	return nil
}

// expandHomePath expands a leading ~ in a path to $HOME.
func expandHomePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" {
		if home := os.Getenv("HOME"); home != "" {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home := os.Getenv("HOME"); home != "" {
			return home + path[1:]
		}
	}
	return path
}

// Adapter defines the interface for agent adapters
type Adapter interface {
	// Type returns the agent type
	Type() AgentType

	// LaunchCommand returns the command to launch the agent
	LaunchCommand(cfg *LaunchConfig) (string, error)

	// ExtraEnv returns agent-specific environment variables to inject at launch.
	ExtraEnv() []string

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
