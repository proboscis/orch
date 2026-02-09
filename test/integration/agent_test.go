package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const controlAgentSessionName = "orch-control-agent"

// hasSession checks if a multiplexer session exists
func hasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// killSession kills a multiplexer session if it exists
func killSession(name string) {
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

// runAgentCommand runs orch agent and waits for session creation
func runAgentCommand(t *testing.T, args ...string) error {
	t.Helper()

	fullArgs := append([]string{
		"--project-root", testRepo,
		"--issues-root", testVault,
		"agent",
	}, args...)

	cmd := exec.Command(orchBinary, fullArgs...)
	cmd.Dir = testRepo
	cmd.Env = append(os.Environ(), "TMUX=")

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

// writeAgentConfig writes a minimal config file for agent tests
func writeAgentConfig(t *testing.T, orchDir string, agent string, mux string) {
	t.Helper()
	configPath := filepath.Join(orchDir, "config.yaml")
	content := fmt.Sprintf("agent: %s\n", agent)
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

	_, err := runOrch(t, "--project-root", testRepo, "agent")
	if err == nil {
		t.Error("expected error when no agent configured")
	}
}

// TestAgentClaudeCreatesMultiplexerSession tests claude backend with tmux
func TestAgentClaudeCreatesMultiplexerSession(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not available")
	}

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	_ = runAgentCommand(t, "--backend", "claude")

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
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not available")
	}

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "opencode", "tmux")

	cmd := exec.Command(orchBinary,
		"--project-root", testRepo,
		"--issues-root", testVault,
		"agent", "--backend", "opencode")
	cmd.Dir = testRepo

	cmd.Start()
	time.Sleep(1 * time.Second)
	cmd.Process.Kill()

	if hasSession(controlAgentSessionName) {
		t.Error("opencode backend should NOT create tmux session")
	}

	stateFile := filepath.Join(orchDir, "control-agent.json")
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

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	cmd := exec.Command("tmux", "new-session", "-d", "-s", controlAgentSessionName)
	if err := cmd.Run(); err != nil {
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

	_ = runAgentCommand(t, "--backend", "claude")

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

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	exec.Command("tmux", "new-session", "-d", "-s", controlAgentSessionName).Run()

	stateFile := filepath.Join(orchDir, "control-agent.json")
	oldState := `{
		"backend": "claude",
		"created_at": "2025-01-01T00:00:00Z",
		"multiplexer_session": "orch-control-agent",
		"multiplexer": "tmux"
	}`
	os.WriteFile(stateFile, []byte(oldState), 0644)

	_ = runAgentCommand(t, "--backend", "claude", "--new")

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

	exec.Command("tmux", "new-session", "-d", "-s", controlAgentSessionName).Run()

	stateFile := filepath.Join(orchDir, "control-agent.json")
	state := `{
		"backend": "claude",
		"created_at": "2026-01-20T16:00:00Z",
		"multiplexer_session": "orch-control-agent",
		"multiplexer": "tmux"
	}`
	os.WriteFile(stateFile, []byte(state), 0644)

	output, err := runOrch(t, "--project-root", testRepo, "agent", "--kill")
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

	orchDir, cleanup := setupAgentTest(t)
	defer cleanup()

	writeAgentConfig(t, orchDir, "claude", "tmux")

	_ = runAgentCommand(t, "--backend", "claude")

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
