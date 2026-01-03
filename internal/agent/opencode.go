package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpenCodeAdapter handles OpenCode CLI with HTTP API
// Unlike other agents that use CLI arguments for prompts,
// OpenCode runs as a headless server and receives prompts via HTTP API.
type OpenCodeAdapter struct{}

func (a *OpenCodeAdapter) Type() AgentType {
	return AgentOpenCode
}

func (a *OpenCodeAdapter) IsAvailable() bool {
	return findOpenCodeBinary() != ""
}

func findOpenCodeBinary() string {
	if path, err := exec.LookPath("opencode"); err == nil {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(home, ".opencode", "bin", "opencode")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func (a *OpenCodeAdapter) LaunchCommand(cfg *LaunchConfig) (string, error) {
	binary := findOpenCodeBinary()
	if binary == "" {
		return "", fmt.Errorf("opencode binary not found")
	}

	if cfg.ContinueSession {
		args := []string{binary, "--continue"}
		if cfg.Prompt != "" {
			args = append(args, "--prompt", cfg.Prompt)
		}
		return strings.Join(args, " "), nil
	}

	var args []string
	args = append(args, binary, "serve")

	port := cfg.Port
	if port == 0 {
		port = 4096
	}
	args = append(args, "--port", fmt.Sprintf("%d", port))
	args = append(args, "--hostname", "0.0.0.0")

	return strings.Join(args, " "), nil
}

// PromptInjection returns InjectionHTTP since opencode uses HTTP API for prompts.
// The prompt is NOT passed via command line or tmux send-keys.
func (a *OpenCodeAdapter) PromptInjection() InjectionMethod {
	return InjectionHTTP
}

func (a *OpenCodeAdapter) ReadyPattern() string {
	// Not used for HTTP-based agents - we check health endpoint instead
	return ""
}

// AttachCommand returns the command to attach to a running opencode server.
// This launches the opencode TUI connected to the server.
func (a *OpenCodeAdapter) AttachCommand(port int) string {
	return fmt.Sprintf("opencode attach http://127.0.0.1:%d", port)
}

// HealthEndpoint returns the URL to check if the server is ready.
func (a *OpenCodeAdapter) HealthEndpoint(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/global/health", port)
}

// Env returns additional environment variables for opencode.
// Sets OPENCODE_PERMISSION for autonomous operation.
func (a *OpenCodeAdapter) Env() []string {
	return []string{
		`OPENCODE_PERMISSION={"edit":"allow","bash":"allow","skill":"allow","webfetch":"allow","doom_loop":"allow","external_directory":"allow"}`,
	}
}

var _ Adapter = (*OpenCodeAdapter)(nil)
