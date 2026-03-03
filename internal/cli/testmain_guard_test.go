package cli

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

const (
	cliTestLockPath = "/tmp/orch-cli-test.lock"
)

func TestMain(m *testing.M) {
	if info, err := os.Stat(cliTestLockPath); err == nil && info.IsDir() {
		_ = os.RemoveAll(cliTestLockPath)
	}

	lockFile, err := os.OpenFile(cliTestLockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open CLI test lock file: %v\n", err)
		os.Exit(2)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		fmt.Fprintf(os.Stderr, "failed to acquire CLI test lock: %v\n", err)
		os.Exit(2)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	defer func() {
		_ = os.Remove(cliTestLockPath)
	}()

	os.Exit(m.Run())
}
