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
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("ORCH_REMOTE", "")
	t.Setenv("ORCH_PROJECT_ROOT", "")
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
