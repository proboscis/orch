package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const controlAgentSessionName = "orch-control-agent"

// hasSession checks if a multiplexer session exists
func hasSession(name string) bool {
	return tmuxCmd("has-session", "-t", name).Run() == nil
}

// killSession kills a multiplexer session if it exists
func killSession(name string) {
	tmuxCmd("kill-session", "-t", name).Run()
}

// setupAgentTest creates necessary directories and returns cleanup function
func setupAgentTest(t *testing.T) (orchDir string, cleanup func()) {
	t.Helper()

	// Create .orch directory in test repo
	orchDir = filepath.Join(testRepo, ".orch")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		t.Fatalf("failed to create .orch dir: %v", err)
	}

	// Ensure no existing control agent session
	killSession(controlAgentSessionName)

	// Remove existing state file
	stateFile := filepath.Join(orchDir, "control-agent.json")
	os.Remove(stateFile)

	cleanup = func() {
		killSession(controlAgentSessionName)
		os.Remove(stateFile)
	}

	return orchDir, cleanup
}

// requireAgentBinary skips the test when the given agent CLI is not in PATH,
// so the test never passes or fails accidentally on hosts without it.
// (TestMain installs fake claude/codex shims, so within this suite the
// binaries are normally present.)
func requireAgentBinary(t *testing.T, name string) {
	t.Helper()
	if !hasBinary(name) {
		t.Skipf("%s binary not available in PATH", name)
	}
}

// runAgentCommand runs `orch agent` with the given args and returns an error
// if the command failed before reaching its final interactive attach step.
// The test harness has no TTY, so even a fully successful launch exits
// non-zero when tmux attach-session reports "not a terminal"; that expected
// attach failure is not an error, but everything before it (session
// creation, state save) is.
func runAgentCommand(t *testing.T, args ...string) error {
	t.Helper()
	ensureRepoMapping(t, testRepo, testVault)

	fullArgs := append([]string{
		"agent",
	}, args...)

	cmd := newOrchCommand(fullArgs...)
	cmd.Dir = testRepo
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ORCH_REMOTE=") || strings.HasPrefix(kv, "ORCH_PROJECT=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "ORCH_PROJECT=")
	cmd.Env = env

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		if err == nil {
			return nil
		}
		if strings.Contains(output.String(), "Attaching to") {
			// The command reached the attach step, so session creation
			// and state save succeeded; only the interactive attach
			// failed, which is unavoidable without a TTY.
			return nil
		}
		return fmt.Errorf("orch agent %v failed: %w\noutput:\n%s", args, err, output.String())
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		return nil
	}
}

// writeAgentConfig writes a minimal config file for agent tests
func writeAgentConfig(t *testing.T, orchDir string, agent string, mux string) {
	t.Helper()
	configPath := filepath.Join(orchDir, "config.yaml")
	content := fmt.Sprintf("agent: %s\nissues:\n  path: %s\n", agent, testVault)
	if mux != "" {
		content += fmt.Sprintf("multiplexer: %s\n", mux)
	}
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
}

// TestAgentRequiresConfig verifies agent fails without config
func TestAgentRequiresConfig(t *testing.T) {
	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	os.Remove(filepath.Join(orchDir, "config.yaml"))

	_, err := runOrch(t, "agent", "--dry-run")
	if err == nil {
		t.Error("expected error when no agent configured")
	}
}

// TestAgentClaudeCreatesMultiplexerSession tests claude backend with tmux
func TestAgentClaudeCreatesMultiplexerSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}
	requireAgentBinary(t, "claude")

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	if err := runAgentCommand(t, "--backend", "claude"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)

	if !hasSession(controlAgentSessionName) {
		t.Error("expected tmux session to be created for claude backend")
	}

	stateFile := filepath.Join(orchDir, "control-agent.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var state struct {
		Backend            string `json:"backend"`
		MultiplexerSession string `json:"multiplexer_session"`
		Multiplexer        string `json:"multiplexer"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse state: %v", err)
	}

	if state.Backend != "claude" {
		t.Errorf("expected backend=claude, got %s", state.Backend)
	}
	if state.MultiplexerSession != controlAgentSessionName {
		t.Errorf("expected multiplexer_session=%s, got %s", controlAgentSessionName, state.MultiplexerSession)
	}
	if state.Multiplexer != "tmux" {
		t.Errorf("expected multiplexer=tmux, got %s", state.Multiplexer)
	}
}

// TestAgentOpenCodeNoMultiplexer tests opencode uses native session (no tmux)
func TestAgentOpenCodeNoMultiplexer(t *testing.T) {
	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "opencode", "tmux")

	// Hermetic backend: `orch agent --backend opencode` needs an `opencode`
	// binary only as the interactive process it execs AFTER saving the state
	// file; nothing this test asserts depends on real opencode behavior.
	// TestMain deliberately installs no opencode shim (other tests exercise
	// the real opencode server bootstrap), so give just this subprocess a
	// fake one on PATH. This replaces the previous skip-when-absent: the
	// test now runs identically on CI and developer machines, and no real
	// opencode process is ever launched (the old form leaked a live opencode
	// TUI into the host on every run — `cmd.Process.Kill` only killed orch).
	binDir := t.TempDir()
	fakeOpencode := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(fakeOpencode, []byte("#!/bin/sh\nprintf 'fake opencode ready\\n'\nsleep 30\n"), 0755); err != nil {
		t.Fatalf("write fake opencode: %v", err)
	}

	cmd := newOrchCommand("agent", "--backend", "opencode")
	cmd.Dir = testRepo
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ORCH_REMOTE=") || strings.HasPrefix(kv, "ORCH_PROJECT=") || strings.HasPrefix(kv, "PATH=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env,
		"ORCH_PROJECT=",
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	// Own process group so teardown kills orch agent AND the opencode child
	// it execs, not just the orch process.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start orch agent: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	defer func() {
		if exited {
			return
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}()

	// The state file write is the LAST step of a multi-RPC chain through the
	// suite daemon (getAPI construction, control-agent config, state read,
	// control prompt write, state write — internal/cli/agent.go
	// runOpenCodeAgent). Waiting a fixed wall-clock interval for it is a bet
	// that the whole chain beats the clock: measured ~0.4s on a half-idle
	// 16-core host and up to 1.9s under a full-suite compile storm, which is
	// exactly the flake this test had. Observe the completion event instead,
	// and surface the subprocess's own error output when it fails — the old
	// fixed sleep discarded it, leaving only "no such file or directory".
	stateFile := filepath.Join(orchDir, "control-agent.json")
	deadline := time.Now().Add(workerReadyTimeout())
	for {
		if _, err := os.Stat(stateFile); err == nil {
			break
		}
		select {
		case err := <-done:
			exited = true
			t.Fatalf("orch agent exited before writing state file %s: %v\noutput:\n%s", stateFile, err, output.String())
		default:
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
			exited = true
			t.Fatalf("state file %s did not appear within %v\noutput:\n%s", stateFile, workerReadyTimeout(), output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	if hasSession(controlAgentSessionName) {
		t.Error("opencode backend should NOT create tmux session")
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var state struct {
		Backend            string `json:"backend"`
		SessionID          string `json:"session_id"`
		MultiplexerSession string `json:"multiplexer_session"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse state: %v", err)
	}

	if state.Backend != "opencode" {
		t.Errorf("expected backend=opencode, got %s", state.Backend)
	}
	if state.SessionID == "" {
		t.Error("expected session_id to be set for opencode")
	}
	if state.MultiplexerSession != "" {
		t.Error("opencode should not have multiplexer_session")
	}
}

// TestAgentAttachesToExistingMultiplexerSession tests reattach to existing tmux session
func TestAgentAttachesToExistingMultiplexerSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}
	requireAgentBinary(t, "claude")

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	if err := tmuxCmd("new-session", "-d", "-s", controlAgentSessionName).Run(); err != nil {
		t.Fatalf("failed to create initial session: %v", err)
	}

	stateFile := filepath.Join(orchDir, "control-agent.json")
	initialState := `{
		"backend": "claude",
		"created_at": "2026-01-01T00:00:00Z",
		"multiplexer_session": "orch-control-agent",
		"multiplexer": "tmux"
	}`
	if err := os.WriteFile(stateFile, []byte(initialState), 0644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	if err := runAgentCommand(t, "--backend", "claude"); err != nil {
		t.Fatal(err)
	}

	if !hasSession(controlAgentSessionName) {
		t.Error("expected session to still exist")
	}

	data, _ := os.ReadFile(stateFile)
	if !strings.Contains(string(data), "2026-01-01T00:00:00Z") {
		t.Error("state file should not be overwritten when attaching to existing session")
	}
}

// TestAgentNewForcesNewMultiplexerSession tests --new flag recreates session
func TestAgentNewForcesNewMultiplexerSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}
	requireAgentBinary(t, "claude")

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	tmuxCmd("new-session", "-d", "-s", controlAgentSessionName).Run()

	stateFile := filepath.Join(orchDir, "control-agent.json")
	oldState := `{
		"backend": "claude",
		"created_at": "2025-01-01T00:00:00Z",
		"multiplexer_session": "orch-control-agent",
		"multiplexer": "tmux"
	}`
	os.WriteFile(stateFile, []byte(oldState), 0644)

	if err := runAgentCommand(t, "--backend", "claude", "--new"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)

	if !hasSession(controlAgentSessionName) {
		t.Error("expected new session to be created")
	}

	data, _ := os.ReadFile(stateFile)
	var state struct {
		CreatedAt string `json:"created_at"`
	}
	json.Unmarshal(data, &state)

	if strings.HasPrefix(state.CreatedAt, "2025-") {
		t.Error("state should be updated with new timestamp")
	}
}

// TestAgentKillTerminatesMultiplexerSession tests --kill flag
func TestAgentKillTerminatesMultiplexerSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	tmuxCmd("new-session", "-d", "-s", controlAgentSessionName).Run()

	stateFile := filepath.Join(orchDir, "control-agent.json")
	state := `{
		"backend": "claude",
		"created_at": "2026-01-20T16:00:00Z",
		"multiplexer_session": "orch-control-agent",
		"multiplexer": "tmux"
	}`
	os.WriteFile(stateFile, []byte(state), 0644)

	output, err := runOrch(t, "agent", "--kill")
	if err != nil {
		t.Fatalf("agent --kill failed: %v\nOutput: %s", err, output)
	}

	if hasSession(controlAgentSessionName) {
		t.Error("expected session to be terminated")
	}

	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Error("expected state file to be removed")
	}
}

// TestAgentStatePersistence tests state file schema
func TestAgentStatePersistence(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}
	requireAgentBinary(t, "claude")

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	if err := runAgentCommand(t, "--backend", "claude"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)

	stateFile := filepath.Join(orchDir, "control-agent.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}

	var state struct {
		Backend            string `json:"backend"`
		CreatedAt          string `json:"created_at"`
		SessionID          string `json:"session_id"`
		MultiplexerSession string `json:"multiplexer_session"`
		Multiplexer        string `json:"multiplexer"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse state: %v", err)
	}

	if state.Backend != "claude" {
		t.Errorf("expected backend=claude, got %s", state.Backend)
	}
	if state.CreatedAt == "" {
		t.Error("expected created_at to be set")
	}
	if state.MultiplexerSession == "" {
		t.Error("expected multiplexer_session to be set for claude")
	}
	if state.Multiplexer == "" {
		t.Error("expected multiplexer to be set for claude")
	}
}
