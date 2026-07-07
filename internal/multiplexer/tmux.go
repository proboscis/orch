package multiplexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/executor"
)

var execCommand = exec.Command

// shellCommands are recognized shell command names.
var shellCommands = map[string]struct{}{
	"bash":       {},
	"zsh":        {},
	"sh":         {},
	"fish":       {},
	"ksh":        {},
	"tcsh":       {},
	"dash":       {},
	"pwsh":       {},
	"powershell": {},
	"cmd":        {},
	"cmd.exe":    {},
	"nu":         {},
	"elvish":     {},
}

// TmuxMultiplexer implements the Multiplexer interface for tmux.
type TmuxMultiplexer struct {
	executor executor.Executor
}

// NewTmuxMultiplexer creates a new TmuxMultiplexer.
func NewTmuxMultiplexer() *TmuxMultiplexer {
	return NewTmuxMultiplexerWithExecutor(executor.NewCommandFuncExecutor(execCommand))
}

func NewTmuxMultiplexerWithExecutor(exec executor.Executor) *TmuxMultiplexer {
	if exec == nil {
		exec = executor.NewLocalExecutor()
	}
	return &TmuxMultiplexer{executor: exec}
}

func (t *TmuxMultiplexer) run(args ...string) error {
	_, _, err := t.executor.RunCommand(context.Background(), "tmux", args, tmuxControlOptions(t.executor, executor.RunOptions{}))
	return err
}

func (t *TmuxMultiplexer) output(args ...string) ([]byte, error) {
	output, _, err := t.executor.RunCommand(context.Background(), "tmux", args, tmuxControlOptions(t.executor, executor.RunOptions{}))
	return output, err
}

func (t *TmuxMultiplexer) runWithOptions(args []string, opts executor.RunOptions) error {
	opts = tmuxControlOptions(t.executor, opts)
	_, _, err := t.executor.RunCommand(context.Background(), "tmux", args, opts)
	return err
}

// Caller-session operations are intentionally limited to attach UX: these
// commands must see TMUX so tmux can identify the user's current client.
func (t *TmuxMultiplexer) runWithCallerSessionEnv(args []string, opts executor.RunOptions) error {
	_, _, err := t.executor.RunCommand(context.Background(), "tmux", args, opts)
	return err
}

func (t *TmuxMultiplexer) outputWithCallerSessionEnv(args ...string) ([]byte, error) {
	output, _, err := t.executor.RunCommand(context.Background(), "tmux", args, executor.RunOptions{})
	return output, err
}

// Type returns the multiplexer type.
func (t *TmuxMultiplexer) Type() Type {
	return TypeTmux
}

// IsAvailable checks if tmux is installed and accessible.
func (t *TmuxMultiplexer) IsAvailable() bool {
	return t.run("-V") == nil
}

// IsInsideSession is an intentional caller-session exception for attach UX.
// It reads TMUX to decide whether orch should switch the current client.
func (t *TmuxMultiplexer) IsInsideSession() bool {
	return os.Getenv("TMUX") != ""
}

// HasSession checks if a tmux session exists.
func (t *TmuxMultiplexer) HasSession(name string) bool {
	return t.run("has-session", "-t", name) == nil
}

// NewSession creates a new tmux session.
func (t *TmuxMultiplexer) NewSession(cfg *SessionConfig) error {
	runAsSessionCommand := shouldPassCommandToNewSession(cfg.Command)

	args := []string{
		"new-session",
		"-d", // detached
		"-s", cfg.SessionName,
	}

	for _, entry := range sessionExtraEnv(t.executor, cfg.Env) {
		args = append(args, "-e", entry)
	}

	if cfg.WorkDir != "" {
		args = append(args, "-c", cfg.WorkDir)
	}
	if cfg.WindowName != "" {
		args = append(args, "-n", cfg.WindowName)
	}
	if runAsSessionCommand {
		args = append(args, cfg.Command)
	}

	if err := t.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr}); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Keep legacy behavior for shell command launches.
	if cfg.Command != "" && !runAsSessionCommand {
		if err := t.SendKeys(cfg.SessionName, cfg.Command); err != nil {
			return fmt.Errorf("failed to send command to session: %w", err)
		}
	}

	return nil
}

// AttachSession attaches to an existing tmux session (foreground).
func (t *TmuxMultiplexer) AttachSession(session string) error {
	return t.runWithOptions(
		[]string{"attach-session", "-t", session},
		executor.RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
	)
}

// KillSession kills a tmux session.
func (t *TmuxMultiplexer) KillSession(session string) error {
	return t.runWithOptions([]string{"kill-session", "-t", session}, executor.RunOptions{Stderr: os.Stderr})
}

// ListSessions returns all tmux session names.
func (t *TmuxMultiplexer) ListSessions() ([]string, error) {
	output, err := t.output("list-sessions", "-F", "#{session_name}")
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

// ListWindows returns windows for a session.
func (t *TmuxMultiplexer) ListWindows(session string) ([]Window, error) {
	output, err := t.output("list-windows", "-t", session, "-F", "#{window_index}:#{window_name}:#{window_id}")
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

// NewWindow creates a new window in an existing session.
func (t *TmuxMultiplexer) NewWindow(session, name, workDir, command string) error {
	args := []string{"new-window", "-t", session}
	if name != "" {
		args = append(args, "-n", name)
	}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}

	if err := t.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr}); err != nil {
		return err
	}

	if command != "" {
		target := session
		if name != "" {
			target = session + ":" + name
		}
		return t.SendKeys(target, command)
	}

	return nil
}

// SelectWindow switches to a window in a session.
func (t *TmuxMultiplexer) SelectWindow(session string, index int) error {
	target := fmt.Sprintf("%s:%d", session, index)
	return t.runWithOptions([]string{"select-window", "-t", target}, executor.RunOptions{Stderr: os.Stderr})
}

// SelectWindowByID switches to a window by ID.
func (t *TmuxMultiplexer) SelectWindowByID(windowID string) error {
	return t.runWithOptions([]string{"select-window", "-t", windowID}, executor.RunOptions{Stderr: os.Stderr})
}

// RenameWindow renames a window in a session.
func (t *TmuxMultiplexer) RenameWindow(session string, index int, name string) error {
	target := fmt.Sprintf("%s:%d", session, index)
	return t.runWithOptions([]string{"rename-window", "-t", target, name}, executor.RunOptions{Stderr: os.Stderr})
}

// ListPanes returns panes for a window target (session:window).
func (t *TmuxMultiplexer) ListPanes(target string) ([]Pane, error) {
	output, err := t.output("list-panes", "-t", target, "-F", "#{pane_id}:#{pane_index}:#{pane_title}:#{pane_current_command}")
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
func (t *TmuxMultiplexer) SplitWindow(target string, vertical bool, percent int) (string, error) {
	args := []string{"split-window", "-d", "-t", target, "-P", "-F", "#{pane_id}"}
	if vertical {
		args = append(args, "-v")
	} else {
		args = append(args, "-h")
	}
	if percent > 0 {
		args = append(args, "-p", strconv.Itoa(percent))
	}
	output, err := t.output(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// SelectPane focuses a pane.
func (t *TmuxMultiplexer) SelectPane(target string) error {
	return t.runWithOptions([]string{"select-pane", "-t", target}, executor.RunOptions{Stderr: os.Stderr})
}

// SetPaneTitle sets a pane title without changing focus.
func (t *TmuxMultiplexer) SetPaneTitle(target, title string) error {
	// Get current pane to restore focus after
	currentOutput, displayErr := t.output("display-message", "-p", "#{pane_id}")
	currentPane := strings.TrimSpace(string(currentOutput))

	// Set the title (this unfortunately selects the pane)
	if err := t.runWithOptions([]string{"select-pane", "-t", target, "-T", title}, executor.RunOptions{Stderr: os.Stderr}); err != nil {
		return err
	}

	// Restore focus to original pane if we had one
	if displayErr == nil && currentPane != "" && currentPane != target {
		_ = t.runWithOptions([]string{"select-pane", "-t", currentPane}, executor.RunOptions{Stderr: os.Stderr}) // Best effort
	}

	return nil
}

// KillPane kills a pane by ID or target.
func (t *TmuxMultiplexer) KillPane(target string) error {
	return t.runWithOptions([]string{"kill-pane", "-t", target}, executor.RunOptions{Stderr: os.Stderr})
}

// SwapPane swaps two panes.
func (t *TmuxMultiplexer) SwapPane(source, target string) error {
	return t.runWithOptions([]string{"swap-pane", "-s", source, "-t", target}, executor.RunOptions{Stderr: os.Stderr})
}

const enterKey = "Enter"

// SendKeys sends keys to a tmux session followed by Enter.
func (t *TmuxMultiplexer) SendKeys(session, keys string) error {
	if err := t.SendKeysLiteral(session, keys); err != nil {
		return err
	}
	return t.SendText(session, enterKey)
}

// SendKeysLiteral sends keys to a tmux session without pressing Enter.
// Uses -l flag to send keys literally (without interpreting special keys).
func (t *TmuxMultiplexer) SendKeysLiteral(session, keys string) error {
	return t.runWithOptions([]string{"send-keys", "-t", session, "-l", keys}, executor.RunOptions{Stderr: os.Stderr})
}

// SendText sends text to a tmux session without pressing Enter.
func (t *TmuxMultiplexer) SendText(session, text string) error {
	return t.runWithOptions([]string{"send-keys", "-t", session, text}, executor.RunOptions{Stderr: os.Stderr})
}

func (t *TmuxMultiplexer) SendBracketedPaste(session, text string) error {
	bufferName := fmt.Sprintf("orch-send-%d", time.Now().UnixNano())
	if err := t.runWithOptions([]string{"set-buffer", "-b", bufferName, text}, executor.RunOptions{Stderr: os.Stderr}); err != nil {
		return err
	}
	defer func() {
		_ = t.runWithOptions([]string{"delete-buffer", "-b", bufferName}, executor.RunOptions{Stderr: os.Stderr})
	}()
	return t.runWithOptions([]string{"paste-buffer", "-b", bufferName, "-p", "-t", session}, executor.RunOptions{Stderr: os.Stderr})
}

// CapturePane captures the content of a tmux pane.
func (t *TmuxMultiplexer) CapturePane(session string, lines int) (string, error) {
	startLine := fmt.Sprintf("-%d", lines)
	output, err := t.output("capture-pane", "-t", session, "-p", "-S", startLine)
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// WaitForReady polls the tmux pane until the pattern is found or timeout is reached.
func (t *TmuxMultiplexer) WaitForReady(session, pattern string, timeout time.Duration) error {
	if pattern == "" {
		return nil
	}

	deadline := time.Now().Add(timeout)
	pollInterval := 200 * time.Millisecond

	for time.Now().Before(deadline) {
		content, err := t.CapturePane(session, 50)
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

// SwitchClient switches the active tmux client to a session.
func (t *TmuxMultiplexer) SwitchClient(session string) error {
	return t.runWithCallerSessionEnv([]string{"switch-client", "-t", session}, executor.RunOptions{Stderr: os.Stderr})
}

// CurrentSession is an intentional caller-session exception for monitor/attach UX.
func (t *TmuxMultiplexer) CurrentSession() (string, error) {
	output, err := t.outputWithCallerSessionEnv("display-message", "-p", "#{session_name}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// SetOption sets a tmux option on a session.
func (t *TmuxMultiplexer) SetOption(session, option, value string) error {
	return t.runWithOptions([]string{"set-option", "-t", session, option, value}, executor.RunOptions{Stderr: os.Stderr})
}

// GetOption retrieves a tmux option value for a session.
func (t *TmuxMultiplexer) GetOption(session, option string) (string, error) {
	output, err := t.output("show-option", "-t", session, "-v", option)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// LinkWindow links an existing window into a session.
func (t *TmuxMultiplexer) LinkWindow(sourceSession string, sourceWindow int, targetSession string, targetIndex int) error {
	source := fmt.Sprintf("%s:%d", sourceSession, sourceWindow)
	target := fmt.Sprintf("%s:%d", targetSession, targetIndex)
	return t.runWithOptions([]string{"link-window", "-s", source, "-t", target}, executor.RunOptions{Stderr: os.Stderr})
}

// LinkWindowByID links an existing window by ID into a session.
func (t *TmuxMultiplexer) LinkWindowByID(windowID, targetSession string, targetIndex int) error {
	target := fmt.Sprintf("%s:%d", targetSession, targetIndex)
	return t.runWithOptions([]string{"link-window", "-s", windowID, "-t", target}, executor.RunOptions{Stderr: os.Stderr})
}

// UnlinkWindow removes a window from a session.
func (t *TmuxMultiplexer) UnlinkWindow(session string, index int) error {
	target := fmt.Sprintf("%s:%d", session, index)
	return t.runWithOptions([]string{"unlink-window", "-t", target}, executor.RunOptions{Stderr: os.Stderr})
}

// ListPaneCommands returns current commands for all panes grouped by session.
func (t *TmuxMultiplexer) ListPaneCommands() (map[string][]string, error) {
	output, err := t.output("list-panes", "-a", "-F", "#{session_name}\t#{pane_current_command}")
	if err != nil {
		if strings.Contains(err.Error(), "no server running") {
			return map[string][]string{}, nil
		}
		return nil, err
	}

	commands := make(map[string][]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		session := strings.TrimSpace(parts[0])
		if session == "" {
			continue
		}
		command := strings.TrimSpace(parts[1])
		commands[session] = append(commands[session], command)
	}

	return commands, nil
}

// AgentAlive reports whether a session has a non-shell foreground command.
func (t *TmuxMultiplexer) AgentAlive(session string, paneCommands map[string][]string) (bool, bool) {
	if paneCommands == nil {
		return false, false
	}
	session = strings.TrimSpace(session)
	if session == "" {
		return false, false
	}
	commands, ok := paneCommands[session]
	if !ok || len(commands) == 0 {
		return false, true
	}
	for _, command := range commands {
		if !isShellCommand(command) {
			return true, true
		}
	}
	return false, true
}

func isShellCommand(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return true
	}
	_, ok := shellCommands[command]
	return ok
}

func shouldPassCommandToNewSession(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}

	first := strings.ToLower(filepath.Base(fields[0]))
	_, isShell := shellCommands[first]
	return !isShell
}
