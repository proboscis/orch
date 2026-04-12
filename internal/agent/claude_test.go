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
		Profile:     "work",
		Resume:      true,
		SessionName: "session-1",
	}

	cmd, err := adapter.LaunchCommand(cfg)
	if err != nil {
		t.Fatalf("LaunchCommand error: %v", err)
	}
	want := "claude --dangerously-skip-permissions --profile work --resume session-1 \"hello\""
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
