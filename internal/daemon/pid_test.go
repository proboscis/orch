package daemon

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/s22625/orch/internal/xdg"
)

func TestPIDFileOperations(t *testing.T) {
	// Use a temp XDG runtime dir for testing
	tmpDir := t.TempDir()
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	defer os.Setenv("XDG_RUNTIME_DIR", oldXDG)

	if err := WritePID(""); err != nil {
		t.Fatalf("WritePID error: %v", err)
	}

	pid, err := ReadPID("")
	if err != nil {
		t.Fatalf("ReadPID error: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want %d", pid, os.Getpid())
	}

	if !IsRunning("") {
		t.Fatal("expected daemon to be running")
	}
	if got := GetRunningPID(""); got != os.Getpid() {
		t.Fatalf("GetRunningPID = %d, want %d", got, os.Getpid())
	}

	if err := RemovePID(""); err != nil {
		t.Fatalf("RemovePID error: %v", err)
	}
	if err := RemovePID(""); err != nil {
		t.Fatalf("RemovePID idempotent error: %v", err)
	}
}

func TestReadPIDInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	defer os.Setenv("XDG_RUNTIME_DIR", oldXDG)

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("EnsureRuntimeDir error: %v", err)
	}
	pidPath := xdg.PIDPath()
	if err := os.WriteFile(pidPath, []byte("bad"), 0644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	if _, err := ReadPID(""); err == nil {
		t.Fatal("expected error for invalid pid")
	}
}

func TestShutdownReasonTracking(t *testing.T) {
	tmpDir := t.TempDir()
	oldRuntime := os.Getenv("XDG_RUNTIME_DIR")
	oldState := os.Getenv("XDG_STATE_HOME")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	os.Setenv("XDG_STATE_HOME", tmpDir)
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldRuntime)
		os.Setenv("XDG_STATE_HOME", oldState)
	}()

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("EnsureRuntimeDir error: %v", err)
	}
	if err := xdg.EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir error: %v", err)
	}

	meta := &DaemonMetadata{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		ExecPath:  "/usr/bin/orch",
		Version:   2,
	}
	if err := WriteMetadata(meta); err != nil {
		t.Fatalf("WriteMetadata error: %v", err)
	}

	if err := UpdateShutdownReason(ShutdownReasonGraceful); err != nil {
		t.Fatalf("UpdateShutdownReason error: %v", err)
	}

	readMeta, err := ReadMetadata("")
	if err != nil {
		t.Fatalf("ReadMetadata error: %v", err)
	}

	if readMeta.ShutdownReason != ShutdownReasonGraceful {
		t.Errorf("ShutdownReason = %q, want %q", readMeta.ShutdownReason, ShutdownReasonGraceful)
	}
	if readMeta.ShutdownAt.IsZero() {
		t.Error("ShutdownAt should not be zero")
	}
}

func TestCheckAndRecoverStaleArtifacts_NoArtifacts(t *testing.T) {
	tmpDir := t.TempDir()
	oldRuntime := os.Getenv("XDG_RUNTIME_DIR")
	oldState := os.Getenv("XDG_STATE_HOME")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	os.Setenv("XDG_STATE_HOME", tmpDir)
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldRuntime)
		os.Setenv("XDG_STATE_HOME", oldState)
	}()

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("EnsureRuntimeDir error: %v", err)
	}

	result, err := CheckAndRecoverStaleArtifacts(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HadStalePID || result.HadStaleSocket || result.WasAbruptExit {
		t.Errorf("expected no stale artifacts, got: stalePID=%v, staleSocket=%v, abruptExit=%v",
			result.HadStalePID, result.HadStaleSocket, result.WasAbruptExit)
	}
}

func TestCheckAndRecoverStaleArtifacts_StalePID(t *testing.T) {
	tmpDir := t.TempDir()
	oldRuntime := os.Getenv("XDG_RUNTIME_DIR")
	oldState := os.Getenv("XDG_STATE_HOME")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	os.Setenv("XDG_STATE_HOME", tmpDir)
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldRuntime)
		os.Setenv("XDG_STATE_HOME", oldState)
	}()

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("EnsureRuntimeDir error: %v", err)
	}

	pidPath := xdg.PIDPath()
	if err := os.WriteFile(pidPath, []byte("99999999"), 0644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	result, err := CheckAndRecoverStaleArtifacts(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.HadStalePID {
		t.Error("expected HadStalePID=true")
	}
	if result.PreviousPID != 99999999 {
		t.Errorf("PreviousPID = %d, want 99999999", result.PreviousPID)
	}

	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("STARTUP RECOVERY")) {
		t.Errorf("expected STARTUP RECOVERY in log output, got: %s", logOutput)
	}
	if !bytes.Contains([]byte(logOutput), []byte("stale PID file")) {
		t.Errorf("expected 'stale PID file' in log output, got: %s", logOutput)
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
	}
}

func TestCheckAndRecoverStaleArtifacts_StaleSocket(t *testing.T) {
	tmpDir := t.TempDir()
	oldRuntime := os.Getenv("XDG_RUNTIME_DIR")
	oldState := os.Getenv("XDG_STATE_HOME")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	os.Setenv("XDG_STATE_HOME", tmpDir)
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldRuntime)
		os.Setenv("XDG_STATE_HOME", oldState)
	}()

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("EnsureRuntimeDir error: %v", err)
	}

	socketPath := xdg.SocketPath()
	if err := os.WriteFile(socketPath, []byte(""), 0600); err != nil {
		t.Fatalf("write socket: %v", err)
	}

	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	result, err := CheckAndRecoverStaleArtifacts(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.HadStaleSocket {
		t.Error("expected HadStaleSocket=true")
	}

	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("stale socket file")) {
		t.Errorf("expected 'stale socket file' in log output, got: %s", logOutput)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("expected stale socket file to be removed")
	}
}

func TestCheckAndRecoverStaleArtifacts_AbruptExit(t *testing.T) {
	tmpDir := t.TempDir()
	oldRuntime := os.Getenv("XDG_RUNTIME_DIR")
	oldState := os.Getenv("XDG_STATE_HOME")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	os.Setenv("XDG_STATE_HOME", tmpDir)
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldRuntime)
		os.Setenv("XDG_STATE_HOME", oldState)
	}()

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("EnsureRuntimeDir error: %v", err)
	}
	if err := xdg.EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir error: %v", err)
	}

	meta := &DaemonMetadata{
		PID:            99999999,
		StartedAt:      time.Now().Add(-1 * time.Hour),
		ExecPath:       "/usr/bin/orch",
		Version:        2,
		ShutdownReason: "",
		ShutdownAt:     time.Time{},
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(xdg.MetadataPath(), metaData, 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	result, err := CheckAndRecoverStaleArtifacts(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.WasAbruptExit {
		t.Error("expected WasAbruptExit=true")
	}
	if !result.HadStaleMetadata {
		t.Error("expected HadStaleMetadata=true")
	}

	logOutput := logBuf.String()
	if !bytes.Contains([]byte(logOutput), []byte("exited ABRUPTLY")) {
		t.Errorf("expected 'exited ABRUPTLY' in log output, got: %s", logOutput)
	}
}

func TestCheckAndRecoverStaleArtifacts_ActiveDaemon(t *testing.T) {
	tmpDir := t.TempDir()
	oldRuntime := os.Getenv("XDG_RUNTIME_DIR")
	oldState := os.Getenv("XDG_STATE_HOME")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	os.Setenv("XDG_STATE_HOME", tmpDir)
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldRuntime)
		os.Setenv("XDG_STATE_HOME", oldState)
	}()

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("EnsureRuntimeDir error: %v", err)
	}

	pidPath := xdg.PIDPath()
	currentPID := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(filepath.Join(string(rune('0'+currentPID/10000%10)), string(rune('0'+currentPID/1000%10)), string(rune('0'+currentPID/100%10)), string(rune('0'+currentPID/10%10)), string(rune('0'+currentPID%10)))), 0644); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte(string(rune('0'+currentPID/10000%10))+string(rune('0'+currentPID/1000%10))+string(rune('0'+currentPID/100%10))+string(rune('0'+currentPID/10%10))+string(rune('0'+currentPID%10))), 0644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	pidStr := make([]byte, 0, 10)
	for n := currentPID; n > 0; n /= 10 {
		pidStr = append([]byte{byte('0' + n%10)}, pidStr...)
	}
	if err := os.WriteFile(pidPath, pidStr, 0644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	_, err := CheckAndRecoverStaleArtifacts(nil)
	if err == nil {
		t.Fatal("expected error when active daemon is running")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("already running")) {
		t.Errorf("expected 'already running' in error, got: %v", err)
	}
}

func TestCheckAndRecoverStaleArtifacts_GracefulShutdownLogged(t *testing.T) {
	tmpDir := t.TempDir()
	oldRuntime := os.Getenv("XDG_RUNTIME_DIR")
	oldState := os.Getenv("XDG_STATE_HOME")
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)
	os.Setenv("XDG_STATE_HOME", tmpDir)
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldRuntime)
		os.Setenv("XDG_STATE_HOME", oldState)
	}()

	if err := xdg.EnsureRuntimeDir(); err != nil {
		t.Fatalf("EnsureRuntimeDir error: %v", err)
	}
	if err := xdg.EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir error: %v", err)
	}

	meta := &DaemonMetadata{
		PID:            99999999,
		StartedAt:      time.Now().Add(-1 * time.Hour),
		ExecPath:       "/usr/bin/orch",
		Version:        2,
		ShutdownReason: ShutdownReasonGraceful,
		ShutdownAt:     time.Now().Add(-30 * time.Minute),
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(xdg.MetadataPath(), metaData, 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	result, err := CheckAndRecoverStaleArtifacts(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.WasAbruptExit {
		t.Error("expected WasAbruptExit=false for graceful shutdown")
	}
	if result.PreviousShutdown != ShutdownReasonGraceful {
		t.Errorf("PreviousShutdown = %q, want %q", result.PreviousShutdown, ShutdownReasonGraceful)
	}
}
