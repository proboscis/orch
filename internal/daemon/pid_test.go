package daemon

import (
	"os"
	"testing"

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
	// Use a temp XDG runtime dir for testing
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
