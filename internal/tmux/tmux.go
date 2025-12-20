package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SessionConfig holds configuration for creating a tmux session
type SessionConfig struct {
	SessionName string
	WorkDir     string
	Command     string   // Command to run in the session
	Env         []string // Environment variables (KEY=VALUE format)
}

// HasSession checks if a tmux session exists
func HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// NewSession creates a new tmux session
func NewSession(cfg *SessionConfig) error {
	args := []string{
		"new-session",
		"-d", // detached
		"-s", cfg.SessionName,
	}

	if cfg.WorkDir != "" {
		args = append(args, "-c", cfg.WorkDir)
	}

	cmd := exec.Command("tmux", args...)
	cmd.Env = append(os.Environ(), cfg.Env...)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}

	// If a command is provided, send it to the session
	if cfg.Command != "" {
		if err := SendKeys(cfg.SessionName, cfg.Command); err != nil {
			return fmt.Errorf("failed to send command to session: %w", err)
		}
	}

	return nil
}

// SendKeys sends keys to a tmux session
func SendKeys(session, keys string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", session, keys, "Enter")
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CapturePane captures the content of a tmux pane
func CapturePane(session string, lines int) (string, error) {
	startLine := fmt.Sprintf("-%d", lines)
	cmd := exec.Command("tmux", "capture-pane", "-t", session, "-p", "-S", startLine)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// AttachSession attaches to an existing tmux session (foreground)
func AttachSession(session string) error {
	cmd := exec.Command("tmux", "attach-session", "-t", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// KillSession kills a tmux session
func KillSession(session string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", session)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ListSessions returns all tmux session names
func ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		// tmux returns error if no sessions exist
		if strings.Contains(err.Error(), "no server running") {
			return nil, nil
		}
		return nil, err
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// NewWindow creates a new window in an existing session
func NewWindow(session, name, workDir, command string) error {
	args := []string{"new-window", "-t", session}
	if name != "" {
		args = append(args, "-n", name)
	}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}

	cmd := exec.Command("tmux", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	if command != "" {
		target := session
		if name != "" {
			target = session + ":" + name
		}
		return SendKeys(target, command)
	}

	return nil
}

// IsTmuxAvailable checks if tmux is installed and accessible
func IsTmuxAvailable() bool {
	cmd := exec.Command("tmux", "-V")
	return cmd.Run() == nil
}
