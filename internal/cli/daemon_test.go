package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	os.Stderr = w
	defer func() {
		os.Stderr = orig
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(out)
}

func setIsolatedXDG(t *testing.T) {
	t.Helper()

	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("ORCH_REMOTE", "")
	t.Setenv("ORCH_PROJECT", "")
}

func TestRunDaemonStatusWithoutProjectRoot(t *testing.T) {
	setIsolatedXDG(t)
	resetGlobalOpts(t)

	out := captureStdout(t, func() {
		if err := runDaemonStatus(); err != nil {
			t.Fatalf("runDaemonStatus() error = %v", err)
		}
	})

	if !strings.Contains(out, "Status: not running") {
		t.Fatalf("expected not running status, got: %s", out)
	}
}

func TestRunDaemonKillWithoutProjectRoot(t *testing.T) {
	setIsolatedXDG(t)
	resetGlobalOpts(t)

	out := captureStdout(t, func() {
		if err := runDaemonKill(&daemonKillOptions{}); err != nil {
			t.Fatalf("runDaemonKill() error = %v", err)
		}
	})

	if !strings.Contains(out, "No daemon running.") {
		t.Fatalf("expected no daemon running output, got: %s", out)
	}
}

func TestRunDaemonKillProjectFlagWarnsButIgnored(t *testing.T) {
	setIsolatedXDG(t)
	resetGlobalOpts(t)

	errOut := captureStderr(t, func() {
		if err := runDaemonKill(&daemonKillOptions{Project: "/tmp/any"}); err != nil {
			t.Fatalf("runDaemonKill() error = %v", err)
		}
	})

	if !strings.Contains(errOut, "--project is deprecated and ignored") {
		t.Fatalf("expected deprecation warning, got: %s", errOut)
	}
}

func TestDaemonRepoRegisterCommandUsesRepoURLIdentity(t *testing.T) {
	cmd := newDaemonRepoRegisterCmd()

	if cmd.Use != "register REPO_URL" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "register REPO_URL")
	}
	if !strings.Contains(strings.ToLower(cmd.Short), "repository url") {
		t.Fatalf("Short = %q, want repository URL guidance", cmd.Short)
	}
	if strings.Contains(strings.ToLower(cmd.Short), "project root") {
		t.Fatalf("Short should not mention project root, got %q", cmd.Short)
	}
}
