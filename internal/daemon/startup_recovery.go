package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/xdg"
)

const (
	daemonLifecycleFilename = "daemon-lifecycle.json"
	lifecycleStateVersion   = 1

	lifecycleStatusRunning = "running"
	lifecycleStatusStopped = "stopped"
)

type daemonLifecycleState struct {
	Version        int       `json:"version"`
	Status         string    `json:"status"`
	PID            int       `json:"pid"`
	StartedAt      time.Time `json:"started_at"`
	ShutdownAt     time.Time `json:"shutdown_at,omitempty"`
	ShutdownReason string    `json:"shutdown_reason,omitempty"`
}

type pidFileStatus int

const (
	pidFileMissing pidFileStatus = iota
	pidFileInvalid
	pidFileStale
	pidFileActive
)

func lifecycleStatePath() string {
	return filepath.Join(xdg.StateDir(), daemonLifecycleFilename)
}

func readLifecycleState() (*daemonLifecycleState, error) {
	data, err := os.ReadFile(lifecycleStatePath())
	if err != nil {
		return nil, err
	}

	var state daemonLifecycleState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

func writeLifecycleState(state *daemonLifecycleState) error {
	if state == nil {
		return fmt.Errorf("lifecycle state is nil")
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal lifecycle state: %w", err)
	}

	return writeAtomicFile(lifecycleStatePath(), data, 0644)
}

func writeAtomicFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := tmpFile.Chmod(perm); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

func (d *Daemon) logPreviousLifecycleState() {
	if d.logger == nil {
		return
	}

	state, err := readLifecycleState()
	if err != nil {
		if !os.IsNotExist(err) {
			d.logger.Printf("warning: failed to read daemon lifecycle state: %v", err)
		}
		return
	}

	switch state.Status {
	case lifecycleStatusStopped:
		reason := state.ShutdownReason
		if reason == "" {
			reason = "unknown"
		}
		if state.ShutdownAt.IsZero() {
			d.logger.Printf("previous daemon shutdown was graceful (reason=%s)", reason)
		} else {
			d.logger.Printf("previous daemon shutdown was graceful (reason=%s, at=%s)", reason, state.ShutdownAt.Format(time.RFC3339))
		}
	case lifecycleStatusRunning:
		if state.PID == os.Getpid() {
			d.logger.Printf("daemon startup follows in-place restart (pid=%d)", state.PID)
			return
		}

		if state.PID > 0 && IsProcessRunning(state.PID) {
			d.logger.Printf("warning: previous daemon lifecycle still marked running with active pid=%d", state.PID)
			return
		}

		if state.StartedAt.IsZero() {
			d.logger.Printf("WARNING: previous daemon exited unexpectedly (pid=%d, no graceful shutdown marker)", state.PID)
		} else {
			d.logger.Printf("WARNING: previous daemon exited unexpectedly (pid=%d, started_at=%s, no graceful shutdown marker)", state.PID, state.StartedAt.Format(time.RFC3339))
		}
	default:
		d.logger.Printf("warning: unknown daemon lifecycle status %q", state.Status)
	}
}

func (d *Daemon) markLifecycleRunning() error {
	state := &daemonLifecycleState{
		Version:   lifecycleStateVersion,
		Status:    lifecycleStatusRunning,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	return writeLifecycleState(state)
}

func (d *Daemon) markLifecycleStopped(reason string) error {
	startedAt := time.Now()
	if prev, err := readLifecycleState(); err == nil && !prev.StartedAt.IsZero() {
		startedAt = prev.StartedAt
	}

	if reason == "" {
		reason = "unknown"
	}

	state := &daemonLifecycleState{
		Version:        lifecycleStateVersion,
		Status:         lifecycleStatusStopped,
		PID:            os.Getpid(),
		StartedAt:      startedAt,
		ShutdownAt:     time.Now(),
		ShutdownReason: reason,
	}

	return writeLifecycleState(state)
}

func (d *Daemon) recoverStartupRuntimeArtifacts() error {
	socketPath := xdg.SocketPath()
	pidPath := xdg.PIDPath()

	pid, pidStatus, err := inspectPIDFileStatus()
	if err != nil {
		return err
	}

	socketPresent, err := isPathPresent(socketPath)
	if err != nil {
		return fmt.Errorf("startup check failed to stat daemon socket: %w", err)
	}

	selfPID := os.Getpid()
	if pidStatus == pidFileActive && pid != selfPID {
		if socketPresent {
			socketActive, probeErr := isSocketActive(socketPath, 300*time.Millisecond)
			if probeErr != nil {
				return fmt.Errorf("startup check failed to probe daemon socket: %w", probeErr)
			}
			if socketActive {
				return fmt.Errorf("daemon already running (pid=%d)", pid)
			}
		}

		return fmt.Errorf("pid file %s points to active process pid=%d; refusing startup recovery", pidPath, pid)
	}

	recovered := false

	switch pidStatus {
	case pidFileInvalid:
		if err := RemovePID(""); err != nil {
			return fmt.Errorf("failed to remove invalid PID file: %w", err)
		}
		recovered = true
		if d.logger != nil {
			d.logger.Printf("startup recovery: removed invalid PID file at %s", pidPath)
		}
	case pidFileStale:
		if err := RemovePID(""); err != nil {
			return fmt.Errorf("failed to remove stale PID file: %w", err)
		}
		recovered = true
		if d.logger != nil {
			d.logger.Printf("startup recovery: removed stale PID file (pid=%d) at %s", pid, pidPath)
		}
	case pidFileActive:
		if pid == selfPID && d.logger != nil {
			d.logger.Printf("startup check: detected in-place restart with existing PID file (pid=%d)", pid)
		}
	}

	if socketPresent {
		socketActive, probeErr := isSocketActive(socketPath, 300*time.Millisecond)
		if probeErr != nil {
			return fmt.Errorf("startup check failed to probe existing daemon socket %s: %w", socketPath, probeErr)
		}

		if socketActive {
			return fmt.Errorf("daemon socket %s is active; refusing startup recovery", socketPath)
		}

		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove stale daemon socket %s: %w", socketPath, err)
		}

		recovered = true
		if d.logger != nil {
			d.logger.Printf("startup recovery: removed stale socket at %s", socketPath)
		}
	}

	if d.logger != nil {
		if recovered {
			d.logger.Printf("startup recovery complete: stale runtime artifacts repaired")
		} else {
			d.logger.Printf("startup check: no stale runtime artifacts detected")
		}
	}

	return nil
}

func inspectPIDFileStatus() (int, pidFileStatus, error) {
	pid, err := ReadPID("")
	if err == nil {
		if IsProcessRunning(pid) {
			return pid, pidFileActive, nil
		}
		return pid, pidFileStale, nil
	}

	if os.IsNotExist(err) {
		return 0, pidFileMissing, nil
	}

	var numErr *strconv.NumError
	if errors.As(err, &numErr) {
		return 0, pidFileInvalid, nil
	}

	return 0, pidFileMissing, fmt.Errorf("startup check failed to read pid file: %w", err)
}

func isPathPresent(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func isSocketActive(socketPath string, timeout time.Duration) (bool, error) {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err == nil {
		_ = conn.Close()
		return true, nil
	}

	if errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) {
		return false, nil
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ENOENT) ||
			errors.Is(opErr.Err, syscall.ECONNREFUSED) ||
			errors.Is(opErr.Err, syscall.ECONNRESET) {
			return false, nil
		}
	}

	return false, err
}
