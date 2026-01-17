package multiplexer

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ZellijMultiplexer implements the Multiplexer interface for zellij.
type ZellijMultiplexer struct{}

// NewZellijMultiplexer creates a new ZellijMultiplexer.
func NewZellijMultiplexer() *ZellijMultiplexer {
	return &ZellijMultiplexer{}
}

// Type returns the multiplexer type.
func (z *ZellijMultiplexer) Type() Type {
	return TypeZellij
}

// IsAvailable checks if zellij is installed and accessible.
func (z *ZellijMultiplexer) IsAvailable() bool {
	cmd := execCommand("zellij", "--version")
	return cmd.Run() == nil
}

// IsInsideSession returns true if we're currently inside a zellij session.
func (z *ZellijMultiplexer) IsInsideSession() bool {
	return os.Getenv("ZELLIJ") != ""
}

// HasSession checks if a zellij session exists.
func (z *ZellijMultiplexer) HasSession(name string) bool {
	sessions, err := z.ListSessions()
	if err != nil {
		return false
	}
	for _, s := range sessions {
		if s == name {
			return true
		}
	}
	return false
}

// NewSession creates a new zellij session.
// Zellij doesn't have a native detached mode like tmux, so we simulate it
// by starting zellij in a subshell that backgrounds itself.
func (z *ZellijMultiplexer) NewSession(cfg *SessionConfig) error {
	env := os.Environ()
	env = append(env, cfg.Env...)

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}

	var script string
	if cfg.Command != "" {
		script = fmt.Sprintf(`cd %q && zellij --session %q &
sleep 2
zellij --session %q action write-chars -- %q
zellij --session %q action write 10`,
			workDir, cfg.SessionName,
			cfg.SessionName, cfg.Command,
			cfg.SessionName)
	} else {
		script = fmt.Sprintf(`cd %q && zellij --session %q &`, workDir, cfg.SessionName)
	}

	detachCmd := exec.Command("sh", "-c", script)
	detachCmd.Env = env
	return detachCmd.Run()
}

// AttachSession attaches to an existing zellij session (foreground).
func (z *ZellijMultiplexer) AttachSession(session string) error {
	cmd := execCommand("zellij", "attach", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// KillSession kills a zellij session.
func (z *ZellijMultiplexer) KillSession(session string) error {
	cmd := execCommand("zellij", "kill-session", session)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ListSessions returns all zellij session names.
func (z *ZellijMultiplexer) ListSessions() ([]string, error) {
	cmd := execCommand("zellij", "list-sessions", "-n") // -n for no formatting
	output, err := cmd.Output()
	if err != nil {
		// Check if the error is just "no sessions"
		if strings.Contains(err.Error(), "No active zellij sessions") ||
			strings.Contains(string(output), "No active zellij sessions") {
			return nil, nil
		}
		// Also check for server not running
		if strings.Contains(err.Error(), "No zellij server") ||
			strings.Contains(string(output), "No zellij server") {
			return nil, nil
		}
		return nil, err
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.Contains(line, "EXITED") {
			// Zellij may include additional info, extract just the session name
			// Format can be "name" or "name (EXITED - ...)"
			parts := strings.Split(line, " ")
			if len(parts) > 0 {
				sessions = append(sessions, parts[0])
			}
		}
	}
	return sessions, nil
}

// ListWindows returns windows (tabs) for a session.
// Note: Zellij uses "tabs" instead of "windows". This requires session to be active.
func (z *ZellijMultiplexer) ListWindows(session string) ([]Window, error) {
	// Zellij doesn't have a direct command to list tabs without being attached
	// We would need to use zellij action commands from within a session
	// For now, return empty - this is a limitation of zellij's CLI
	return nil, nil
}

// NewWindow creates a new tab in an existing session.
func (z *ZellijMultiplexer) NewWindow(session, name, workDir, command string) error {
	// Use zellij action new-tab
	args := []string{"--session", session, "action", "new-tab"}
	if workDir != "" {
		args = append(args, "--cwd", workDir)
	}
	if name != "" {
		args = append(args, "--name", name)
	}

	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	if command != "" {
		return z.SendKeys(session, command)
	}
	return nil
}

// SelectWindow switches to a tab in a session.
func (z *ZellijMultiplexer) SelectWindow(session string, index int) error {
	// zellij action go-to-tab <index>
	// Note: zellij tabs are 1-indexed
	args := []string{"--session", session, "action", "go-to-tab", strconv.Itoa(index + 1)}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SelectWindowByID switches to a tab by ID.
// Note: Zellij doesn't use window IDs the same way tmux does.
func (z *ZellijMultiplexer) SelectWindowByID(windowID string) error {
	// Convert ID to index if possible
	index, err := strconv.Atoi(windowID)
	if err != nil {
		return fmt.Errorf("zellij does not support window IDs, use index: %w", err)
	}
	// This won't work without a session context
	return z.SelectWindow("", index)
}

// RenameWindow renames a tab in a session.
func (z *ZellijMultiplexer) RenameWindow(session string, index int, name string) error {
	// First select the tab, then rename it
	if err := z.SelectWindow(session, index); err != nil {
		return err
	}
	args := []string{"--session", session, "action", "rename-tab", name}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ListPanes returns panes for a tab.
// Note: Zellij doesn't have a direct CLI command for this.
func (z *ZellijMultiplexer) ListPanes(target string) ([]Pane, error) {
	// Zellij doesn't expose pane listing via CLI in the same way
	return nil, nil
}

// SplitWindow splits the current pane.
func (z *ZellijMultiplexer) SplitWindow(target string, vertical bool, percent int) (string, error) {
	// zellij action new-pane --direction [up|down|left|right]
	direction := "down"
	if !vertical {
		direction = "right"
	}

	args := []string{"action", "new-pane", "--direction", direction}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	// Zellij doesn't return pane IDs like tmux
	return "", nil
}

// SelectPane focuses a pane.
func (z *ZellijMultiplexer) SelectPane(target string) error {
	// Zellij uses focus-next-pane or focus-previous-pane
	// Direct pane selection by ID isn't available
	return fmt.Errorf("zellij does not support direct pane selection by target")
}

// SetPaneTitle sets a pane title.
func (z *ZellijMultiplexer) SetPaneTitle(target, title string) error {
	// zellij action rename-pane <name>
	// But this requires being in the pane
	args := []string{"action", "rename-pane", title}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// KillPane closes the current pane.
func (z *ZellijMultiplexer) KillPane(target string) error {
	args := []string{"action", "close-pane"}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SwapPane swaps panes.
func (z *ZellijMultiplexer) SwapPane(source, target string) error {
	// Zellij doesn't have a direct swap-pane command
	return fmt.Errorf("zellij does not support swap-pane")
}

// SendKeys sends keys to a zellij session followed by Enter.
func (z *ZellijMultiplexer) SendKeys(session, keys string) error {
	if err := z.SendKeysLiteral(session, keys); err != nil {
		return err
	}
	// Send Enter key (ASCII 10 = newline)
	return z.sendKey(session, 10)
}

// SendKeysLiteral sends keys to a zellij session without pressing Enter.
func (z *ZellijMultiplexer) SendKeysLiteral(session, keys string) error {
	// zellij action write-chars "<text>"
	args := []string{"--session", session, "action", "write-chars", "--", keys}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SendText sends text to a zellij session without pressing Enter.
func (z *ZellijMultiplexer) SendText(session, text string) error {
	return z.SendKeysLiteral(session, text)
}

// sendKey sends a single key code to a zellij session.
func (z *ZellijMultiplexer) sendKey(session string, keyCode int) error {
	// zellij action write <keycode>
	args := []string{"--session", session, "action", "write", strconv.Itoa(keyCode)}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CapturePane captures the content of a zellij pane.
func (z *ZellijMultiplexer) CapturePane(session string, lines int) (string, error) {
	// zellij action dump-screen <file>
	// This dumps to a file, we need to capture to stdout
	// Alternative: use zellij action scroll-up/down and capture

	// For now, use a temp file approach
	tmpFile := fmt.Sprintf("/tmp/zellij-capture-%d", time.Now().UnixNano())
	defer os.Remove(tmpFile)

	args := []string{"--session", session, "action", "dump-screen", tmpFile}
	cmd := execCommand("zellij", args...)
	if err := cmd.Run(); err != nil {
		return "", err
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		return "", err
	}

	// If lines is specified, only return that many lines from the end
	allLines := strings.Split(string(content), "\n")
	if lines > 0 && len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}

	return strings.Join(allLines, "\n"), nil
}

// WaitForReady polls the zellij pane until the pattern is found or timeout is reached.
func (z *ZellijMultiplexer) WaitForReady(session, pattern string, timeout time.Duration) error {
	if pattern == "" {
		return nil
	}

	deadline := time.Now().Add(timeout)
	pollInterval := 200 * time.Millisecond

	for time.Now().Before(deadline) {
		content, err := z.CapturePane(session, 50)
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

// SwitchClient switches to a session (when already inside zellij).
func (z *ZellijMultiplexer) SwitchClient(session string) error {
	// Zellij doesn't have a direct switch-client like tmux
	// You would typically detach and reattach
	// But from within zellij, you can use plugins or session manager
	return z.AttachSession(session)
}

// CurrentSession returns the name of the current zellij session.
func (z *ZellijMultiplexer) CurrentSession() (string, error) {
	// Check ZELLIJ_SESSION_NAME environment variable
	if name := os.Getenv("ZELLIJ_SESSION_NAME"); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("not inside a zellij session")
}

// SetOption sets an option on a session.
// Note: Zellij handles options differently via configuration files.
func (z *ZellijMultiplexer) SetOption(session, option, value string) error {
	// Zellij uses YAML config files, not runtime options like tmux
	// This is a no-op for zellij
	return nil
}

// GetOption retrieves an option value for a session.
func (z *ZellijMultiplexer) GetOption(session, option string) (string, error) {
	// Zellij uses YAML config files
	return "", fmt.Errorf("zellij does not support runtime options")
}

// LinkWindow links a window.
// Note: Zellij doesn't have a concept of window linking.
func (z *ZellijMultiplexer) LinkWindow(sourceSession string, sourceWindow int, targetSession string, targetIndex int) error {
	return fmt.Errorf("zellij does not support window linking")
}

// LinkWindowByID links a window by ID.
func (z *ZellijMultiplexer) LinkWindowByID(windowID, targetSession string, targetIndex int) error {
	return fmt.Errorf("zellij does not support window linking")
}

// UnlinkWindow removes a window.
func (z *ZellijMultiplexer) UnlinkWindow(session string, index int) error {
	return fmt.Errorf("zellij does not support window unlinking")
}

// ListPaneCommands returns current commands for all panes.
// Note: Zellij doesn't expose this information via CLI.
func (z *ZellijMultiplexer) ListPaneCommands() (map[string][]string, error) {
	// Zellij doesn't have an equivalent command
	return map[string][]string{}, nil
}

// AgentAlive reports whether a session has a non-shell foreground command.
// Note: Limited support in zellij.
func (z *ZellijMultiplexer) AgentAlive(session string, paneCommands map[string][]string) (bool, bool) {
	// Without pane command information, we can only check if session exists
	if z.HasSession(session) {
		return true, true
	}
	return false, false
}
