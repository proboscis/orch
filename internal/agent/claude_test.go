package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/s22625/orch/internal/multiplexer"
)

func TestDoubleQuote(t *testing.T) {
	input := "path\\with \"quotes\" $HOME and `tick"
	got := doubleQuote(input)
	want := "\"path\\\\with \\\"quotes\\\" \\$HOME and \\`tick\""
	if got != want {
		t.Fatalf("doubleQuote = %q, want %q", got, want)
	}
}

func TestClaudeLaunchCommand(t *testing.T) {
	adapter := &ClaudeAdapter{}
	cfg := &LaunchConfig{
		Prompt:      "hello",
		Profile:     "work", // display-only: claude has no --profile flag
		Resume:      true,
		SessionName: "session-1",
	}

	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	// The profile must NOT reach the command line — claude has no --profile
	// flag and would refuse to start. Profile selection is CLAUDE_CONFIG_DIR.
	want := "claude --dangerously-skip-permissions --resume session-1 \"hello\""
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}

func TestClaudeConfigDirEnvInjection(t *testing.T) {
	t.Setenv("HOME", "/home/exec-host")
	cfg := &LaunchConfig{ClaudeConfigDir: "~/.config/claude-cryptic"}

	env := cfg.Env()
	want := "CLAUDE_CONFIG_DIR=/home/exec-host/.config/claude-cryptic"
	found := false
	for _, e := range env {
		if e == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("Env() = %v, want it to contain %q (expanded on the execution host)", env, want)
	}

	// No profile dir configured -> no CLAUDE_CONFIG_DIR injected (agent default).
	if entries := (&LaunchConfig{}).ClaudeConfigDirEnv(); len(entries) != 0 {
		t.Fatalf("ClaudeConfigDirEnv() with no dir = %v, want empty", entries)
	}
}

func TestClaudeLaunchCommandModelAndEffort(t *testing.T) {
	adapter := &ClaudeAdapter{}
	cfg := &LaunchConfig{
		Prompt:       "hello",
		Model:        "claude-fable-5",
		ModelVariant: "xhigh",
	}

	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	want := "claude --dangerously-skip-permissions --model claude-fable-5 --effort xhigh \"hello\""
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}

func TestClaudeExtraEnv(t *testing.T) {
	adapter := &ClaudeAdapter{}
	env := adapter.ExtraEnv()
	if len(env) != 1 {
		t.Fatalf("ExtraEnv() returned %d items, want 1", len(env))
	}
	if env[0] != "IS_DEMO=1" {
		t.Fatalf("ExtraEnv()[0] = %q, want %q", env[0], "IS_DEMO=1")
	}
}

func TestClaudeExtraEnvReachesTmuxSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	mux := multiplexer.NewTmuxMultiplexer()
	if !mux.IsAvailable() {
		t.Skip("tmux not available")
	}

	adapter := &ClaudeAdapter{}
	outPath := filepath.Join(t.TempDir(), "is_demo.txt")
	script := fmt.Sprintf(
		"import os, pathlib, time; pathlib.Path(%q).write_text(os.getenv('IS_DEMO', '')); time.sleep(0.2)",
		outPath,
	)
	sessionName := fmt.Sprintf("orch-claude-env-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = mux.KillSession(sessionName)
	})

	err := mux.NewSession(&multiplexer.SessionConfig{
		SessionName: sessionName,
		WorkDir:     t.TempDir(),
		Command:     fmt.Sprintf("python3 -c %s", shellQuote(script)),
		Env:         adapter.ExtraEnv(),
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(outPath)
		if err == nil {
			if got := string(data); got != "1" {
				t.Fatalf("tmux session wrote %q, want %q", got, "1")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for tmux session to write %s", outPath)
}
