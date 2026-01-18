package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	orchDir      = ".orch"
	pidFile      = "daemon.pid"
	logFile      = "daemon.log"
	metadataFile = "daemon.json"
	lockFile     = "daemon.lock"
)

type DaemonMetadata struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	ExecPath  string    `json:"exec_path"`
	ExecMtime time.Time `json:"exec_mtime"`
}

func OrchDir(projectRoot string) string {
	return filepath.Join(projectRoot, orchDir)
}

func PIDFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), pidFile)
}

func LogFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), logFile)
}

func MetadataFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), metadataFile)
}

func LockFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), lockFile)
}

func ReadMetadata(projectRoot string) (*DaemonMetadata, error) {
	data, err := os.ReadFile(MetadataFilePath(projectRoot))
	if err != nil {
		return nil, err
	}
	var meta DaemonMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func EnsureOrchDir(projectRoot string) error {
	dir := OrchDir(projectRoot)
	return os.MkdirAll(dir, 0700)
}

func AcquireLock(projectRoot string) (*os.File, error) {
	if err := EnsureOrchDir(projectRoot); err != nil {
		return nil, fmt.Errorf("failed to create .orch directory: %w", err)
	}

	lockPath := LockFilePath(projectRoot)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("another daemon instance is already running")
		}
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	return f, nil
}

func WritePID(projectRoot string) error {
	if err := EnsureOrchDir(projectRoot); err != nil {
		return fmt.Errorf("failed to create .orch directory: %w", err)
	}

	pidPath := PIDFilePath(projectRoot)
	pid := os.Getpid()
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)
}

func ReadPID(projectRoot string) (int, error) {
	pidPath := PIDFilePath(projectRoot)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}

	return pid, nil
}

func RemovePID(projectRoot string) error {
	pidPath := PIDFilePath(projectRoot)
	err := os.Remove(pidPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func IsProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func IsRunning(projectRoot string) bool {
	pid, err := ReadPID(projectRoot)
	if err != nil {
		return false
	}

	return IsProcessRunning(pid)
}

func GetRunningPID(projectRoot string) int {
	pid, err := ReadPID(projectRoot)
	if err != nil {
		return 0
	}

	if !IsProcessRunning(pid) {
		return 0
	}

	return pid
}

func IsStaleBinary(projectRoot string) (bool, error) {
	if !IsRunning(projectRoot) {
		return false, nil
	}

	meta, err := ReadMetadata(projectRoot)
	if err != nil {
		pidPath := PIDFilePath(projectRoot)
		pidInfo, err := os.Stat(pidPath)
		if err != nil {
			return false, err
		}
		daemonStartTime := pidInfo.ModTime()

		execPath, err := os.Executable()
		if err != nil {
			return false, err
		}
		resolved, _ := filepath.EvalSymlinks(execPath)
		if resolved != "" {
			execPath = resolved
		}

		execInfo, err := os.Stat(execPath)
		if err != nil {
			return false, err
		}
		return execInfo.ModTime().After(daemonStartTime), nil
	}

	execInfo, err := os.Stat(meta.ExecPath)
	if err != nil {
		return false, err
	}

	return execInfo.ModTime().After(meta.ExecMtime), nil
}

func RestartDaemon(projectRoot string) error {
	pid := GetRunningPID(projectRoot)
	if pid == 0 {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	return process.Signal(syscall.SIGHUP)
}
