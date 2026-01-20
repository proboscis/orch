package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const controlAgentSessionName = "orch-control-agent"

// hasTmuxSession checks if a tmux session exists
func hasTmuxSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// killTmuxSession kills a tmux session if it exists
func killTmuxSession(name string) {
	exec.Command("tmux", "kill-session", "-t", name).Run()
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
	killTmuxSession(controlAgentSessionName)

	// Remove existing state file
	stateFile := filepath.Join(orchDir, "control-agent.json")
	os.Remove(stateFile)

	cleanup = func() {
		killTmuxSession(controlAgentSessionName)
		os.Remove(stateFile)
	}

	return orchDir, cleanup
}

// runAgentCommand runs orch agent and waits for session creation
// Returns any error from the command (attach errors are expected)
func runAgentCommand(t *testing.T, args ...string) error {
	t.Helper()

	// Build full args with project root
	fullArgs := append([]string{
		"--project-root", testRepo,
		"--issues-root", testVault,
		"agent",
	}, args...)

	cmd := exec.Command(orchBinary, fullArgs...)
	cmd.Dir = testRepo
	// Ensure we're not inside tmux so attach will fail quickly
	cmd.Env = append(os.Environ(), "TMUX=")

	// Run with timeout - the command will fail on attach since there's no TTY
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		return nil
	}
}

func TestAgentCreatesNewSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	// Run agent command - will create session then fail on attach (no TTY)
	_ = runAgentCommand(t)

	// Give session time to be created
	time.Sleep(500 * time.Millisecond)

	// Verify session was created
	if !hasTmuxSession(controlAgentSessionName) {
		t.Error("expected control agent session to be created")
	}

	// Verify state file was created
	stateFile := filepath.Join(orchDir, "control-agent.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Error("expected state file to be created at .orch/control-agent.json")
	}
}

func TestAgentStateFilePersistence(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	// Run agent command
	_ = runAgentCommand(t)

	// Give time for file to be written
	time.Sleep(500 * time.Millisecond)

	// Read and verify state file
	stateFile := filepath.Join(orchDir, "control-agent.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var state struct {
		Backend     string `json:"backend"`
		CreatedAt   string `json:"created_at"`
		TmuxSession string `json:"tmux_session"`
		Port        int    `json:"port,omitempty"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse state file: %v", err)
	}

	// Verify state contents
	if state.Backend == "" {
		t.Error("expected backend to be set")
	}
	if state.TmuxSession != controlAgentSessionName {
		t.Errorf("expected tmux_session=%s, got %s", controlAgentSessionName, state.TmuxSession)
	}
	if state.CreatedAt == "" {
		t.Error("expected created_at to be set")
	}
}

func TestAgentAttachesToExistingSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	// First, create a session manually
	cmd := exec.Command("tmux", "new-session", "-d", "-s", controlAgentSessionName)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create initial session: %v", err)
	}

	// Create a state file pointing to it with a known timestamp
	stateFile := filepath.Join(orchDir, "control-agent.json")
	initialState := `{
		"backend": "opencode",
		"created_at": "2026-01-01T00:00:00Z",
		"tmux_session": "orch-control-agent"
	}`
	if err := os.WriteFile(stateFile, []byte(initialState), 0644); err != nil {
		t.Fatalf("failed to write state file: %v", err)
	}

	// Run agent command - should try to attach to existing
	_ = runAgentCommand(t)

	// Session should still exist
	if !hasTmuxSession(controlAgentSessionName) {
		t.Error("expected existing session to still exist")
	}

	// State file should not be overwritten (check created_at is unchanged)
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	if !strings.Contains(string(data), "2026-01-01T00:00:00Z") {
		t.Error("expected state file to not be overwritten when attaching to existing session")
	}
}

func TestAgentNewForcesNewSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	// First, create a session manually
	cmd := exec.Command("tmux", "new-session", "-d", "-s", controlAgentSessionName)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create initial session: %v", err)
	}

	// Create a state file with old timestamp
	stateFile := filepath.Join(orchDir, "control-agent.json")
	initialState := `{
		"backend": "claude",
		"created_at": "2025-01-01T00:00:00Z",
		"tmux_session": "orch-control-agent"
	}`
	if err := os.WriteFile(stateFile, []byte(initialState), 0644); err != nil {
		t.Fatalf("failed to write state file: %v", err)
	}

	// Run agent with --new flag
	_ = runAgentCommand(t, "--new")

	// Give time for session to be recreated
	time.Sleep(500 * time.Millisecond)

	// Session should exist (recreated)
	if !hasTmuxSession(controlAgentSessionName) {
		t.Error("expected new session to be created")
	}

	// State file should be updated with new timestamp
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var state struct {
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse state: %v", err)
	}

	// Timestamp should be updated (not the old 2025 one)
	if strings.HasPrefix(state.CreatedAt, "2025-") {
		t.Error("expected state file to be updated with new timestamp when --new flag used")
	}
}

func TestAgentKillTerminatesSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	// Create a session
	cmd := exec.Command("tmux", "new-session", "-d", "-s", controlAgentSessionName)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create state file
	stateFile := filepath.Join(orchDir, "control-agent.json")
	state := `{
		"backend": "opencode",
		"created_at": "2026-01-20T16:00:00Z",
		"tmux_session": "orch-control-agent"
	}`
	if err := os.WriteFile(stateFile, []byte(state), 0644); err != nil {
		t.Fatalf("failed to write state: %v", err)
	}

	// Run agent --kill
	output, err := runOrch(t, "--project-root", testRepo, "agent", "--kill")
	if err != nil {
		t.Fatalf("agent --kill failed: %v\nOutput: %s", err, output)
	}

	// Session should be gone
	if hasTmuxSession(controlAgentSessionName) {
		t.Error("expected session to be terminated after --kill")
	}

	// State file should be removed
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Error("expected state file to be removed after --kill")
	}
}
