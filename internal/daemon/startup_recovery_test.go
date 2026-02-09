package daemon

import (
	"bytes"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/xdg"
)

func setupStartupRecoveryEnv(t *testing.T) {
	t.Helper()

	baseDir, err := os.MkdirTemp("/tmp", "orch-recovery-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(baseDir)
	})

	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(baseDir, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(baseDir, "state"))
}

func TestRecoverStartupRuntimeArtifactsRemovesStaleSocketAndDeadPID(t *testing.T) {
	setupStartupRecoveryEnv(t)

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	socketPath := xdg.SocketPath()
	addr, err := net.ResolveUnixAddr("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to resolve unix addr: %v", err)
	}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("failed to create test socket: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	listener.Close()

	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("expected stale socket file to exist: %v", err)
	}

	deadPID := os.Getpid() + 1000000
	if IsProcessRunning(deadPID) {
		deadPID = os.Getpid() + 2000000
	}
	if err := os.WriteFile(xdg.PIDPath(), []byte(strconv.Itoa(deadPID)), 0644); err != nil {
		t.Fatalf("failed to write stale pid file: %v", err)
	}

	var logBuf bytes.Buffer
	d := &Daemon{logger: log.New(&logBuf, "", 0)}
	if err := d.recoverStartupRuntimeArtifacts(); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale socket to be removed, stat err=%v", err)
	}

	if _, err := os.Stat(xdg.PIDPath()); !os.IsNotExist(err) {
		t.Fatalf("expected stale pid file to be removed, stat err=%v", err)
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "removed stale PID file") {
		t.Fatalf("expected stale pid removal log, got: %s", logs)
	}
	if !strings.Contains(logs, "removed stale socket") {
		t.Fatalf("expected stale socket removal log, got: %s", logs)
	}
}

func TestRecoverStartupRuntimeArtifactsDoesNotOverrideActiveSocket(t *testing.T) {
	setupStartupRecoveryEnv(t)

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	socketPath := xdg.SocketPath()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create active socket: %v", err)
	}
	defer listener.Close()

	var logBuf bytes.Buffer
	d := &Daemon{logger: log.New(&logBuf, "", 0)}
	err = d.recoverStartupRuntimeArtifacts()
	if err == nil {
		t.Fatal("expected recovery to fail when socket is actively serving")
	}
	if !strings.Contains(err.Error(), "active") {
		t.Fatalf("expected active-socket protection error, got: %v", err)
	}

	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("expected active socket to remain, stat err=%v", err)
	}
}

// TestRecoverStartupRuntimeArtifactsHandlesPIDReuse tests that recovery succeeds
// when PID file points to an active process but the socket is inactive.
// This is the PID reuse case: the OS has reused the PID for an unrelated process.
// The socket is the authoritative signal for "daemon running", not the PID.
func TestRecoverStartupRuntimeArtifactsHandlesPIDReuse(t *testing.T) {
	setupStartupRecoveryEnv(t)

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	// Start a helper process to simulate an unrelated process that reused the PID
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Write the helper's PID to the PID file (simulating stale PID file from old daemon)
	if err := os.WriteFile(xdg.PIDPath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	// No socket exists - the daemon is NOT running, just the PID was reused

	var logBuf bytes.Buffer
	d := &Daemon{logger: log.New(&logBuf, "", 0)}
	err := d.recoverStartupRuntimeArtifacts()

	// Recovery should SUCCEED - socket inactive means no daemon running
	if err != nil {
		t.Fatalf("expected recovery to succeed on PID reuse (socket inactive), got: %v", err)
	}

	// PID file should be removed
	if _, err := os.Stat(xdg.PIDPath()); !os.IsNotExist(err) {
		t.Fatalf("expected stale PID file to be removed, stat err=%v", err)
	}

	// Should log about PID reuse
	logs := logBuf.String()
	if !strings.Contains(logs, "reused by unrelated process") {
		t.Fatalf("expected PID reuse log message, got: %s", logs)
	}
}

func TestLogPreviousLifecycleStateDetectsAbruptExit(t *testing.T) {
	setupStartupRecoveryEnv(t)

	if err := writeLifecycleState(&daemonLifecycleState{
		Version:   lifecycleStateVersion,
		Status:    lifecycleStatusRunning,
		PID:       os.Getpid() + 1000000,
		StartedAt: time.Unix(1730000000, 0),
	}); err != nil {
		t.Fatalf("failed to write lifecycle state: %v", err)
	}

	var logBuf bytes.Buffer
	d := &Daemon{logger: log.New(&logBuf, "", 0)}
	d.logPreviousLifecycleState()

	logs := logBuf.String()
	if !strings.Contains(logs, "exited unexpectedly") {
		t.Fatalf("expected abrupt-exit diagnostic in logs, got: %s", logs)
	}
}

func TestLogPreviousLifecycleStateReportsGracefulShutdown(t *testing.T) {
	setupStartupRecoveryEnv(t)

	if err := writeLifecycleState(&daemonLifecycleState{
		Version:        lifecycleStateVersion,
		Status:         lifecycleStatusStopped,
		PID:            1234,
		StartedAt:      time.Unix(1730000000, 0),
		ShutdownAt:     time.Unix(1730000300, 0),
		ShutdownReason: "signal:terminated",
	}); err != nil {
		t.Fatalf("failed to write lifecycle state: %v", err)
	}

	var logBuf bytes.Buffer
	d := &Daemon{logger: log.New(&logBuf, "", 0)}
	d.logPreviousLifecycleState()

	logs := logBuf.String()
	if !strings.Contains(logs, "shutdown was graceful") {
		t.Fatalf("expected graceful-shutdown diagnostic in logs, got: %s", logs)
	}
	if !strings.Contains(logs, "signal:terminated") {
		t.Fatalf("expected shutdown reason in logs, got: %s", logs)
	}
}
