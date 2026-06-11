package multiplexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/s22625/orch/internal/executor"
)

const zellijMaxSessionNameLen = 25

// zellijCommandTimeout bounds every non-interactive zellij CLI invocation.
// `zellij attach --create-background` has been observed to hang forever in
// some environments (TTY-less daemon/worker processes with fresh XDG dirs),
// and an unbounded multiplexer call freezes the entire worker control path —
// the worker keeps heartbeating but never completes or acknowledges its
// lease. A bounded call turns that hang into an explicit error instead.
const zellijCommandTimeout = 30 * time.Second

func zellijCommandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), zellijCommandTimeout)
}

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

func shortenSessionName(name string) string {
	if len(name) <= zellijMaxSessionNameLen {
		return name
	}
	hash := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(hash[:])[:6]
	prefix := name[:zellijMaxSessionNameLen-7]
	return prefix + "-" + suffix
}

type ZellijMultiplexer struct {
	executor executor.Executor
}

func NewZellijMultiplexer() *ZellijMultiplexer {
	return NewZellijMultiplexerWithExecutor(executor.NewCommandFuncExecutor(execCommand))
}

func NewZellijMultiplexerWithExecutor(exec executor.Executor) *ZellijMultiplexer {
	if exec == nil {
		exec = executor.NewLocalExecutor()
	}
	return &ZellijMultiplexer{executor: exec}
}

func (z *ZellijMultiplexer) run(args ...string) error {
	ctx, cancel := zellijCommandContext()
	defer cancel()
	_, _, err := z.executor.RunCommand(ctx, "zellij", args, executor.RunOptions{})
	return err
}

func (z *ZellijMultiplexer) output(args ...string) ([]byte, error) {
	ctx, cancel := zellijCommandContext()
	defer cancel()
	output, _, err := z.executor.RunCommand(ctx, "zellij", args, executor.RunOptions{})
	return output, err
}

func (z *ZellijMultiplexer) runWithOptions(args []string, opts executor.RunOptions) error {
	ctx, cancel := zellijCommandContext()
	defer cancel()
	return z.runWithOptionsContext(ctx, args, opts)
}

// runWithOptionsContext is the unbounded variant for interactive commands
// (attach) that legitimately run as long as the user stays attached.
func (z *ZellijMultiplexer) runWithOptionsContext(ctx context.Context, args []string, opts executor.RunOptions) error {
	_, _, err := z.executor.RunCommand(ctx, "zellij", args, opts)
	return err
}

func (z *ZellijMultiplexer) Type() Type {
	return TypeZellij
}

func (z *ZellijMultiplexer) IsAvailable() bool {
	return z.run("--version") == nil
}

func (z *ZellijMultiplexer) IsInsideSession() bool {
	return os.Getenv("ZELLIJ") != ""
}

func (z *ZellijMultiplexer) HasSession(name string) bool {
	name = shortenSessionName(name)
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
	sessionName := shortenSessionName(cfg.SessionName)

	env := sessionEnv(z.executor, cfg.Env)

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}

	// Use zellij attach --create-background to create a detached session.
	// The old nohup approach caused sessions to exit immediately because
	// zellij doesn't persist properly when started with stdin from /dev/null.
	args := []string{"attach", "--create-background", sessionName}
	if err := z.runWithOptions(args, executor.RunOptions{Dir: workDir, Env: env}); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}

	if err := z.waitForSession(sessionName, 5*time.Second); err != nil {
		return err
	}

	if cfg.Command != "" {
		return z.SendKeys(sessionName, cfg.Command)
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
	session = shortenSessionName(session)
	return z.runWithOptionsContext(
		context.Background(),
		[]string{"attach", session},
		executor.RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
	)
}

func (z *ZellijMultiplexer) KillSession(session string) error {
	session = shortenSessionName(session)
	return z.runWithOptions([]string{"kill-session", session}, executor.RunOptions{Stderr: os.Stderr})
}

func (z *ZellijMultiplexer) ListSessions() ([]string, error) {
	output, err := z.output("list-sessions", "-n")
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
	session = shortenSessionName(session)
	args := []string{"--session", session, "action", "new-tab"}
	if workDir != "" {
		args = append(args, "--cwd", workDir)
	}
	if name != "" {
		args = append(args, "--name", name)
	}

	if err := z.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr}); err != nil {
		return err
	}

	if command != "" {
		return z.SendKeys(session, command)
	}
	return nil
}

func (z *ZellijMultiplexer) SelectWindow(session string, index int) error {
	session = shortenSessionName(session)
	args := []string{"--session", session, "action", "go-to-tab", strconv.Itoa(index + 1)}
	return z.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr})
}

func (z *ZellijMultiplexer) SelectWindowByID(windowID string) error {
	return unsupportedErr("select window by ID")
}

func (z *ZellijMultiplexer) RenameWindow(session string, index int, name string) error {
	session = shortenSessionName(session)
	if err := z.SelectWindow(session, index); err != nil {
		return err
	}
	args := []string{"--session", session, "action", "rename-tab", name}
	return z.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr})
}

func (z *ZellijMultiplexer) ListPanes(target string) ([]Pane, error) {
	return nil, unsupportedErr("list panes")
}

func (z *ZellijMultiplexer) SplitWindow(target string, vertical bool, percent int) (string, error) {
	session := shortenSessionName(extractSession(target))
	if session == "" {
		return "", unsupportedErr("split window without session")
	}

	direction := "down"
	if !vertical {
		direction = "right"
	}

	args := []string{"--session", session, "action", "new-pane", "--direction", direction}
	if err := z.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr}); err != nil {
		return "", err
	}
	return "", nil
}

func (z *ZellijMultiplexer) SelectPane(target string) error {
	return unsupportedErr("select pane by target")
}

func (z *ZellijMultiplexer) SetPaneTitle(target, title string) error {
	session := shortenSessionName(extractSession(target))
	if session == "" {
		return unsupportedErr("set pane title without session")
	}
	args := []string{"--session", session, "action", "rename-pane", title}
	return z.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr})
}

func (z *ZellijMultiplexer) KillPane(target string) error {
	session := shortenSessionName(extractSession(target))
	if session == "" {
		return unsupportedErr("kill pane without session")
	}
	args := []string{"--session", session, "action", "close-pane"}
	return z.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr})
}

func (z *ZellijMultiplexer) SwapPane(source, target string) error {
	return unsupportedErr("swap pane")
}

func (z *ZellijMultiplexer) SendKeys(session, keys string) error {
	session = shortenSessionName(session)
	if err := z.SendKeysLiteral(session, keys); err != nil {
		return err
	}
	return z.sendKey(session, 10)
}

func (z *ZellijMultiplexer) SendKeysLiteral(session, keys string) error {
	session = shortenSessionName(session)
	args := []string{"--session", session, "action", "write-chars", "--", keys}
	return z.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr})
}

func (z *ZellijMultiplexer) SendText(session, text string) error {
	return z.SendKeysLiteral(session, text)
}

func (z *ZellijMultiplexer) SendBracketedPaste(session, text string) error {
	session = shortenSessionName(session)
	start := []string{"--session", session, "action", "write", "27", "91", "50", "48", "48", "126"}
	if err := z.runWithOptions(start, executor.RunOptions{Stderr: os.Stderr}); err != nil {
		return err
	}
	if err := z.SendKeysLiteral(session, text); err != nil {
		return err
	}
	end := []string{"--session", session, "action", "write", "27", "91", "50", "48", "49", "126"}
	return z.runWithOptions(end, executor.RunOptions{Stderr: os.Stderr})
}

func (z *ZellijMultiplexer) sendKey(session string, keyCode int) error {
	session = shortenSessionName(session)
	args := []string{"--session", session, "action", "write", strconv.Itoa(keyCode)}
	return z.runWithOptions(args, executor.RunOptions{Stderr: os.Stderr})
}

func (z *ZellijMultiplexer) CapturePane(session string, lines int) (string, error) {
	session = shortenSessionName(session)
	tmpFile, err := os.CreateTemp("", "zellij-capture-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// zellij 0.44+ takes the dump path via the --path flag; the old positional
	// form (`dump-screen <path>`) is rejected ("argument ... wasn't expected").
	args := []string{"--session", session, "action", "dump-screen", "--path", tmpPath}
	if err := z.run(args...); err != nil {
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
	session = shortenSessionName(session)
	if pattern == "" {
		return nil
	}

	deadline := time.Now().Add(timeout)
	pollInterval := 200 * time.Millisecond

	for time.Now().Before(deadline) {
		content, err := z.CapturePane(session, 50)
		if err != nil {
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

// ErrCrossSessionAttach is returned when attempting to attach to a different
// Zellij session from inside an existing Zellij session.
var ErrCrossSessionAttach = fmt.Errorf("cannot attach to a different Zellij session from inside Zellij")

func (z *ZellijMultiplexer) SwitchClient(session string) error {
	session = shortenSessionName(session)

	// Check if we're inside a Zellij session
	if !z.IsInsideSession() {
		// Not inside Zellij, just attach normally
		return z.AttachSession(session)
	}

	// Get current session name
	currentSession, err := z.CurrentSession()
	if err != nil {
		// Can't determine current session, try attach anyway
		return z.AttachSession(session)
	}

	// If target is the same session, nothing to do
	if currentSession == session {
		return nil
	}

	// Inside Zellij trying to attach to a different session - this doesn't work
	// Return a helpful error message
	return fmt.Errorf("%w: run 'orch attach' from outside Zellij, or open a new terminal", ErrCrossSessionAttach)
}

func (z *ZellijMultiplexer) CurrentSession() (string, error) {
	if name := os.Getenv("ZELLIJ_SESSION_NAME"); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("not inside a zellij session")
}

func (z *ZellijMultiplexer) SetOption(session, option, value string) error {
	return unsupportedErr("runtime options")
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
	return nil, unsupportedErr("pane command inspection")
}

func (z *ZellijMultiplexer) AgentAlive(session string, paneCommands map[string][]string) (bool, bool) {
	return false, false
}
