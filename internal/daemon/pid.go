package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	orchDir = ".orch"
	pidFile = "daemon.pid"
	logFile = "daemon.log"
)

// OrchDir returns the path to the .orch directory in the vault
func OrchDir(vaultPath string) string {
	return filepath.Join(vaultPath, orchDir)
}

// PIDFilePath returns the path to the PID file
func PIDFilePath(vaultPath string) string {
	return filepath.Join(OrchDir(vaultPath), pidFile)
}

// LogFilePath returns the path to the daemon log file
func LogFilePath(vaultPath string) string {
	return filepath.Join(OrchDir(vaultPath), logFile)
}

// EnsureOrchDir creates the .orch directory if it doesn't exist
func EnsureOrchDir(vaultPath string) error {
	dir := OrchDir(vaultPath)
	return os.MkdirAll(dir, 0755)
}

// WritePID writes the current process PID to the PID file
func WritePID(vaultPath string) error {
	if err := EnsureOrchDir(vaultPath); err != nil {
		return fmt.Errorf("failed to create .orch directory: %w", err)
	}

	pidPath := PIDFilePath(vaultPath)
	pid := os.Getpid()
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)
}

// ReadPID reads the PID from the PID file
func ReadPID(vaultPath string) (int, error) {
	pidPath := PIDFilePath(vaultPath)
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

// RemovePID removes the PID file
func RemovePID(vaultPath string) error {
	pidPath := PIDFilePath(vaultPath)
	err := os.Remove(pidPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// IsProcessRunning checks if a process with the given PID is running
func IsProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0
	// to check if the process actually exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// IsRunning checks if the daemon is currently running for this vault
func IsRunning(vaultPath string) bool {
	pid, err := ReadPID(vaultPath)
	if err != nil {
		return false
	}

	return IsProcessRunning(pid)
}

// GetRunningPID returns the PID of the running daemon, or 0 if not running
func GetRunningPID(vaultPath string) int {
	pid, err := ReadPID(vaultPath)
	if err != nil {
		return 0
	}

	if !IsProcessRunning(pid) {
		return 0
	}

	return pid
}

func IsStaleBinary(vaultPath string) (bool, error) {
	if !IsRunning(vaultPath) {
		return false, nil
	}

	pidPath := PIDFilePath(vaultPath)
	pidInfo, err := os.Stat(pidPath)
	if err != nil {
		return false, err
	}
	daemonStartTime := pidInfo.ModTime()

	execPath, err := os.Executable()
	if err != nil {
		return false, err
	}

	execInfo, err := os.Stat(execPath)
	if err != nil {
		return false, err
	}

	return execInfo.ModTime().After(daemonStartTime), nil
}

func RestartDaemon(vaultPath string) error {
	pid := GetRunningPID(vaultPath)
	if pid == 0 {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	return process.Signal(syscall.SIGHUP)
}
