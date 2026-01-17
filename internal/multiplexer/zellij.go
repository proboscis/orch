package multiplexer

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func unsupportedErr(feature string) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, feature)
}

func extractSession(target string) string {
	if target == "" {
		return ""
	}
	if idx := strings.Index(target, ":"); idx != -1 {
		return target[:idx]
	}
	return target
}

type ZellijMultiplexer struct{}

func NewZellijMultiplexer() *ZellijMultiplexer {
	return &ZellijMultiplexer{}
}

func (z *ZellijMultiplexer) Type() Type {
	return TypeZellij
}

func (z *ZellijMultiplexer) IsAvailable() bool {
	cmd := execCommand("zellij", "--version")
	return cmd.Run() == nil
}

func (z *ZellijMultiplexer) IsInsideSession() bool {
	return os.Getenv("ZELLIJ") != ""
}

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

func (z *ZellijMultiplexer) NewSession(cfg *SessionConfig) error {
	env := os.Environ()
	env = append(env, cfg.Env...)

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}

	startScript := fmt.Sprintf(
		`cd %q && nohup zellij --session %q </dev/null >/dev/null 2>&1 &`,
		workDir, cfg.SessionName)

	startCmd := exec.Command("sh", "-c", startScript)
	startCmd.Env = env
	if err := startCmd.Run(); err != nil {
		return err
	}

	if err := z.waitForSession(cfg.SessionName, 5*time.Second); err != nil {
		return err
	}

	if cfg.Command != "" {
		return z.SendKeys(cfg.SessionName, cfg.Command)
	}
	return nil
}

func (z *ZellijMultiplexer) waitForSession(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if z.HasSession(name) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for zellij session %q to start", name)
}

func (z *ZellijMultiplexer) AttachSession(session string) error {
	cmd := execCommand("zellij", "attach", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (z *ZellijMultiplexer) KillSession(session string) error {
	cmd := execCommand("zellij", "kill-session", session)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (z *ZellijMultiplexer) ListSessions() ([]string, error) {
	cmd := execCommand("zellij", "list-sessions", "-n")
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

func (z *ZellijMultiplexer) ListWindows(session string) ([]Window, error) {
	return nil, unsupportedErr("list windows")
}

func (z *ZellijMultiplexer) NewWindow(session, name, workDir, command string) error {
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

func (z *ZellijMultiplexer) SelectWindow(session string, index int) error {
	args := []string{"--session", session, "action", "go-to-tab", strconv.Itoa(index + 1)}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (z *ZellijMultiplexer) SelectWindowByID(windowID string) error {
	return unsupportedErr("select window by ID")
}

func (z *ZellijMultiplexer) RenameWindow(session string, index int, name string) error {
	if err := z.SelectWindow(session, index); err != nil {
		return err
	}
	args := []string{"--session", session, "action", "rename-tab", name}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (z *ZellijMultiplexer) ListPanes(target string) ([]Pane, error) {
	return nil, unsupportedErr("list panes")
}

func (z *ZellijMultiplexer) SplitWindow(target string, vertical bool, percent int) (string, error) {
	session := extractSession(target)
	if session == "" {
		return "", unsupportedErr("split window without session")
	}

	direction := "down"
	if !vertical {
		direction = "right"
	}

	args := []string{"--session", session, "action", "new-pane", "--direction", direction}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return "", nil
}

func (z *ZellijMultiplexer) SelectPane(target string) error {
	return unsupportedErr("select pane by target")
}

func (z *ZellijMultiplexer) SetPaneTitle(target, title string) error {
	session := extractSession(target)
	if session == "" {
		return unsupportedErr("set pane title without session")
	}
	args := []string{"--session", session, "action", "rename-pane", title}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (z *ZellijMultiplexer) KillPane(target string) error {
	session := extractSession(target)
	if session == "" {
		return unsupportedErr("kill pane without session")
	}
	args := []string{"--session", session, "action", "close-pane"}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (z *ZellijMultiplexer) SwapPane(source, target string) error {
	return unsupportedErr("swap pane")
}

func (z *ZellijMultiplexer) SendKeys(session, keys string) error {
	if err := z.SendKeysLiteral(session, keys); err != nil {
		return err
	}
	// Send Enter key (ASCII 10 = newline)
	return z.sendKey(session, 10)
}

func (z *ZellijMultiplexer) SendKeysLiteral(session, keys string) error {
	args := []string{"--session", session, "action", "write-chars", "--", keys}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (z *ZellijMultiplexer) SendText(session, text string) error {
	return z.SendKeysLiteral(session, text)
}

func (z *ZellijMultiplexer) sendKey(session string, keyCode int) error {
	args := []string{"--session", session, "action", "write", strconv.Itoa(keyCode)}
	cmd := execCommand("zellij", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (z *ZellijMultiplexer) CapturePane(session string, lines int) (string, error) {
	tmpFile, err := os.CreateTemp("", "zellij-capture-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	args := []string{"--session", session, "action", "dump-screen", tmpPath}
	cmd := execCommand("zellij", args...)
	if err := cmd.Run(); err != nil {
		return "", err
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", err
	}

	allLines := strings.Split(string(content), "\n")
	if lines > 0 && len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}

	return strings.Join(allLines, "\n"), nil
}

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

func (z *ZellijMultiplexer) SwitchClient(session string) error {
	return z.AttachSession(session)
}

func (z *ZellijMultiplexer) CurrentSession() (string, error) {
	if name := os.Getenv("ZELLIJ_SESSION_NAME"); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("not inside a zellij session")
}

func (z *ZellijMultiplexer) SetOption(session, option, value string) error {
	return nil
}

func (z *ZellijMultiplexer) GetOption(session, option string) (string, error) {
	return "", unsupportedErr("runtime options")
}

func (z *ZellijMultiplexer) LinkWindow(sourceSession string, sourceWindow int, targetSession string, targetIndex int) error {
	return unsupportedErr("window linking")
}

func (z *ZellijMultiplexer) LinkWindowByID(windowID, targetSession string, targetIndex int) error {
	return unsupportedErr("window linking")
}

func (z *ZellijMultiplexer) UnlinkWindow(session string, index int) error {
	return unsupportedErr("window unlinking")
}

func (z *ZellijMultiplexer) ListPaneCommands() (map[string][]string, error) {
	return map[string][]string{}, nil
}

func (z *ZellijMultiplexer) AgentAlive(session string, paneCommands map[string][]string) (bool, bool) {
	if z.HasSession(session) {
		return true, true
	}
	return false, false
}
