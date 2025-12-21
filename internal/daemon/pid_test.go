package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPIDFileOperations(t *testing.T) {
	dir := t.TempDir()

	if err := WritePID(dir); err != nil {
		t.Fatalf("WritePID error: %v", err)
	}

	pid, err := ReadPID(dir)
	if err != nil {
		t.Fatalf("ReadPID error: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want %d", pid, os.Getpid())
	}

	if !IsRunning(dir) {
		t.Fatal("expected daemon to be running")
	}
	if got := GetRunningPID(dir); got != os.Getpid() {
		t.Fatalf("GetRunningPID = %d, want %d", got, os.Getpid())
	}

	if err := RemovePID(dir); err != nil {
		t.Fatalf("RemovePID error: %v", err)
	}
	if err := RemovePID(dir); err != nil {
		t.Fatalf("RemovePID idempotent error: %v", err)
	}
}

func TestReadPIDInvalid(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureOrchDir(dir); err != nil {
		t.Fatalf("EnsureOrchDir error: %v", err)
	}
	pidPath := filepath.Join(OrchDir(dir), pidFile)
	if err := os.WriteFile(pidPath, []byte("bad"), 0644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	if _, err := ReadPID(dir); err == nil {
		t.Fatal("expected error for invalid pid")
	}
}
