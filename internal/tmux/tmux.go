package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
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

// SendKeys sends keys to a tmux session followed by Enter
func SendKeys(session, keys string) error {
	cmd := execCommand("tmux", "send-keys", "-t", session, keys, "Enter")
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SendKeysLiteral sends keys to a tmux session without pressing Enter
// Uses -l flag to send keys literally (without interpreting special keys)
func SendKeysLiteral(session, keys string) error {
	cmd := execCommand("tmux", "send-keys", "-t", session, "-l", keys)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SendText sends text to a tmux session without pressing Enter
func SendText(session, text string) error {
	cmd := execCommand("tmux", "send-keys", "-t", session, text)
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

// WaitForReady polls the tmux pane until the pattern is found or timeout is reached
func WaitForReady(session, pattern string, timeout time.Duration) error {
	if pattern == "" {
		return nil
	}

	deadline := time.Now().Add(timeout)
	pollInterval := 200 * time.Millisecond

	for time.Now().Before(deadline) {
		content, err := CapturePane(session, 50)
		if err != nil {
			// Session might not be ready yet, keep trying
			time.Sleep(pollInterval)
			continue
		}

		if strings.Contains(content, pattern) {
			return nil
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout waiting for agent to be ready (pattern: %q)", pattern)
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

// Pane describes a tmux pane.
type Pane struct {
	ID      string
	Index   int
	Title   string
	Command string
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

// HasWindow checks if a window index exists in the session.
func HasWindow(session string, index int) bool {
	windows, err := ListWindows(session)
	if err != nil {
		return false
	}
	for _, window := range windows {
		if window.Index == index {
			return true
		}
	}
	return false
}

// ListPanes returns panes for a window target (session:window).
func ListPanes(target string) ([]Pane, error) {
	cmd := execCommand("tmux", "list-panes", "-t", target, "-F", "#{pane_id}:#{pane_index}:#{pane_title}:#{pane_current_command}")
	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "no server running") || strings.Contains(err.Error(), "can't find") {
			return nil, nil
		}
		return nil, err
	}

	var panes []Pane
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 2 {
			continue
		}
		index, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		pane := Pane{
			ID:    parts[0],
			Index: index,
		}
		if len(parts) > 2 {
			pane.Title = parts[2]
		}
		if len(parts) > 3 {
			pane.Command = parts[3]
		}
		panes = append(panes, pane)
	}
	return panes, nil
}

// SplitWindow splits a pane and returns the new pane ID.
func SplitWindow(target string, vertical bool, percent int) (string, error) {
	args := []string{"split-window", "-d", "-t", target, "-P", "-F", "#{pane_id}"}
	if vertical {
		args = append(args, "-v")
	} else {
		args = append(args, "-h")
	}
	if percent > 0 {
		args = append(args, "-p", strconv.Itoa(percent))
	}
	cmd := execCommand("tmux", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// KillPane kills a pane by ID or target.
func KillPane(target string) error {
	cmd := execCommand("tmux", "kill-pane", "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// MoveWindow moves a window to a new index.
func MoveWindow(session, source string, index int) error {
	src := fmt.Sprintf("%s:%s", session, source)
	target := fmt.Sprintf("%s:%d", session, index)
	cmd := execCommand("tmux", "move-window", "-s", src, "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// JoinPane joins a pane into another target.
func JoinPane(source, target string) error {
	cmd := execCommand("tmux", "join-pane", "-s", source, "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// MovePane moves a pane to another target.
func MovePane(source, target string) error {
	cmd := execCommand("tmux", "move-pane", "-s", source, "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SwapPane swaps two panes.
func SwapPane(source, target string) error {
	cmd := execCommand("tmux", "swap-pane", "-s", source, "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SelectPane focuses a pane.
func SelectPane(target string) error {
	cmd := execCommand("tmux", "select-pane", "-t", target)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SetPaneTitle sets a pane title without changing focus.
func SetPaneTitle(target, title string) error {
	// Get current pane to restore focus after
	currentCmd := execCommand("tmux", "display-message", "-p", "#{pane_id}")
	currentOutput, displayErr := currentCmd.Output()
	currentPane := strings.TrimSpace(string(currentOutput))

	// Set the title (this unfortunately selects the pane)
	cmd := execCommand("tmux", "select-pane", "-t", target, "-T", title)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	// Restore focus to original pane if we had one
	if displayErr == nil && currentPane != "" && currentPane != target {
		restoreCmd := execCommand("tmux", "select-pane", "-t", currentPane)
		restoreCmd.Stderr = os.Stderr
		_ = restoreCmd.Run() // Best effort
	}

	return nil
}

// RenameWindow renames a window in a session.
func RenameWindow(session string, index int, name string) error {
	target := fmt.Sprintf("%s:%d", session, index)
	cmd := execCommand("tmux", "rename-window", "-t", target, name)
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

// LinkWindowByID links an existing window by ID into a session.
func LinkWindowByID(windowID, targetSession string, targetIndex int) error {
	target := fmt.Sprintf("%s:%d", targetSession, targetIndex)
	cmd := execCommand("tmux", "link-window", "-s", windowID, "-t", target)
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

// CurrentSession returns the name of the current tmux session.
func CurrentSession() (string, error) {
	cmd := execCommand("tmux", "display-message", "-p", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// SetOption sets a tmux option on a session.
func SetOption(session, option, value string) error {
	cmd := execCommand("tmux", "set-option", "-t", session, option, value)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// GetOption retrieves a tmux option value for a session.
func GetOption(session, option string) (string, error) {
	cmd := execCommand("tmux", "show-option", "-t", session, "-v", option)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// SelectWindowByID switches to a window by ID.
func SelectWindowByID(windowID string) error {
	cmd := execCommand("tmux", "select-window", "-t", windowID)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SwitchClient switches the active tmux client to a session.
func SwitchClient(session string) error {
	cmd := execCommand("tmux", "switch-client", "-t", session)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
