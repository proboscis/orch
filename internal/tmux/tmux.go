package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var execCommand = exec.Command

// SessionConfig holds configuration for creating a tmux session
type SessionConfig struct {
	SessionName string
	WorkDir     string
	Command     string   // Command to run in the session
	Env         []string // Environment variables (KEY=VALUE format)
	WindowName  string
}

// HasSession checks if a tmux session exists
func HasSession(name string) bool {
	cmd := execCommand("tmux", "has-session", "-t", name)
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
	if cfg.WindowName != "" {
		args = append(args, "-n", cfg.WindowName)
	}

	cmd := execCommand("tmux", args...)
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, cfg.Env...)
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
	cmd := execCommand("tmux", "send-keys", "-t", session, keys, "Enter")
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CapturePane captures the content of a tmux pane
func CapturePane(session string, lines int) (string, error) {
	startLine := fmt.Sprintf("-%d", lines)
	cmd := execCommand("tmux", "capture-pane", "-t", session, "-p", "-S", startLine)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// AttachSession attaches to an existing tmux session (foreground)
func AttachSession(session string) error {
	cmd := execCommand("tmux", "attach-session", "-t", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// KillSession kills a tmux session
func KillSession(session string) error {
	cmd := execCommand("tmux", "kill-session", "-t", session)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ListSessions returns all tmux session names
func ListSessions() ([]string, error) {
	cmd := execCommand("tmux", "list-sessions", "-F", "#{session_name}")
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

	cmd := execCommand("tmux", args...)
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
	cmd := execCommand("tmux", "-V")
	return cmd.Run() == nil
}

// IsInsideTmux returns true if we're currently inside a tmux session
func IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// Window describes a tmux window.
type Window struct {
	Index int
	Name  string
	ID    string
}

// ListWindows returns windows for a session.
func ListWindows(session string) ([]Window, error) {
	cmd := execCommand("tmux", "list-windows", "-t", session, "-F", "#{window_index}:#{window_name}:#{window_id}")
	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "no server running") || strings.Contains(err.Error(), "can't find session") {
			return nil, nil
		}
		return nil, err
	}

	var windows []Window
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		index, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		windows = append(windows, Window{
			Index: index,
			Name:  parts[1],
			ID:    parts[2],
		})
	}
	return windows, nil
}

// MoveWindow moves a window to a new index.
func MoveWindow(session, source string, index int) error {
	src := fmt.Sprintf("%s:%s", session, source)
	target := fmt.Sprintf("%s:%d", session, index)
	cmd := execCommand("tmux", "move-window", "-s", src, "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// LinkWindow links an existing window into a session.
func LinkWindow(sourceSession string, sourceWindow int, targetSession string, targetIndex int) error {
	source := fmt.Sprintf("%s:%d", sourceSession, sourceWindow)
	target := fmt.Sprintf("%s:%d", targetSession, targetIndex)
	cmd := execCommand("tmux", "link-window", "-s", source, "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// UnlinkWindow removes a window from a session.
func UnlinkWindow(session string, index int) error {
	target := fmt.Sprintf("%s:%d", session, index)
	cmd := execCommand("tmux", "unlink-window", "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SelectWindow switches to a window in a session.
func SelectWindow(session string, index int) error {
	target := fmt.Sprintf("%s:%d", session, index)
	cmd := execCommand("tmux", "select-window", "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SwitchClient switches the active tmux client to a session.
func SwitchClient(session string) error {
	cmd := execCommand("tmux", "switch-client", "-t", session)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
