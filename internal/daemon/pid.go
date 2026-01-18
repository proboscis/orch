package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/xdg"
)

// Legacy constants for backward compatibility
const (
	orchDir      = ".orch"
	pidFile      = "daemon.pid"
	logFile      = "daemon.log"
	metadataFile = "daemon.json"
	lockFile     = "daemon.lock"
)

type DaemonMetadata struct {
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"started_at"`
	ExecPath    string    `json:"exec_path"`
	ExecMtime   time.Time `json:"exec_mtime"`
	ProjectRoot string    `json:"project_root,omitempty"` // Deprecated: for legacy compat
	Version     int       `json:"version,omitempty"`      // Schema version (2 = XDG global daemon)
}

// DaemonInfo contains information about a running daemon for listing
type DaemonInfo struct {
	PID         int
	ProjectRoot string // Empty for global daemon
	SocketPath  string
	StartedAt   time.Time
	Uptime      time.Duration
	IsHealthy   bool
	IsGlobal    bool // True if this is the XDG global daemon
}

// OrchDir returns the .orch directory for a project (legacy, for backward compat).
func OrchDir(projectRoot string) string {
	return filepath.Join(projectRoot, orchDir)
}

// PIDFilePath returns the global daemon PID file path.
func PIDFilePath(_ string) string {
	return xdg.PIDPath()
}

// LogFilePath returns the global daemon log file path.
func LogFilePath(_ string) string {
	return xdg.LogPath()
}

// MetadataFilePath returns the global daemon metadata file path.
func MetadataFilePath(_ string) string {
	return xdg.MetadataPath()
}

// LockFilePath returns the global daemon lock file path.
func LockFilePath(_ string) string {
	return xdg.LockPath()
}

// LegacyPIDFilePath returns the legacy per-project PID file path.
func LegacyPIDFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), pidFile)
}

// LegacyLogFilePath returns the legacy per-project log file path.
func LegacyLogFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), logFile)
}

// LegacyMetadataFilePath returns the legacy per-project metadata file path.
func LegacyMetadataFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), metadataFile)
}

// LegacyLockFilePath returns the legacy per-project lock file path.
func LegacyLockFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), lockFile)
}

func ReadMetadata(_ string) (*DaemonMetadata, error) {
	data, err := os.ReadFile(xdg.MetadataPath())
	if err != nil {
		return nil, err
	}
	var meta DaemonMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// EnsureOrchDir is kept for backward compatibility but now ensures XDG dirs.
func EnsureOrchDir(_ string) error {
	if err := xdg.EnsureRuntimeDir(); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	if err := xdg.EnsureStateDir(); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}
	return nil
}

// AcquireLock acquires the global daemon lock.
func AcquireLock(_ string) (*os.File, error) {
	if err := EnsureOrchDir(""); err != nil {
		return nil, err
	}

	lockPath := xdg.LockPath()
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

// WritePID writes the daemon PID to the global PID file.
func WritePID(_ string) error {
	if err := EnsureOrchDir(""); err != nil {
		return err
	}

	pidPath := xdg.PIDPath()
	pid := os.Getpid()
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)
}

// ReadPID reads the daemon PID from the global PID file.
func ReadPID(_ string) (int, error) {
	pidPath := xdg.PIDPath()
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

// RemovePID removes the global PID file.
func RemovePID(_ string) error {
	pidPath := xdg.PIDPath()
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

// IsRunning checks if the global daemon is running.
func IsRunning(_ string) bool {
	pid, err := ReadPID("")
	if err != nil {
		return false
	}

	return IsProcessRunning(pid)
}

// GetRunningPID returns the PID of the running global daemon, or 0 if not running.
func GetRunningPID(_ string) int {
	pid, err := ReadPID("")
	if err != nil {
		return 0
	}

	if !IsProcessRunning(pid) {
		return 0
	}

	return pid
}

func IsStaleBinary(_ string) (bool, error) {
	if !IsRunning("") {
		return false, nil
	}

	meta, err := ReadMetadata("")
	if err != nil {
		pidPath := xdg.PIDPath()
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

func RestartDaemon(_ string) error {
	pid := GetRunningPID("")
	if pid == 0 {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	return process.Signal(syscall.SIGHUP)
}

// globalOrchDir returns the global orch directory (legacy, now uses XDG).
func globalOrchDir() string {
	return xdg.DataDir()
}

func globalRegistryPath() string {
	return filepath.Join(xdg.DataDir(), "daemons.json")
}

type daemonRegistry struct {
	Daemons map[string]registryEntry `json:"daemons"`
}

type registryEntry struct {
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	SocketPath string    `json:"socket_path"`
}

func loadRegistry() (*daemonRegistry, error) {
	path := globalRegistryPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &daemonRegistry{Daemons: make(map[string]registryEntry)}, nil
		}
		return nil, err
	}

	var reg daemonRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return &daemonRegistry{Daemons: make(map[string]registryEntry)}, nil
	}
	if reg.Daemons == nil {
		reg.Daemons = make(map[string]registryEntry)
	}
	return &reg, nil
}

func saveRegistry(reg *daemonRegistry) error {
	path := globalRegistryPath()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// RegisterDaemon registers the global daemon (the projectRoot param is ignored now).
func RegisterDaemon(_ string) error {
	reg, err := loadRegistry()
	if err != nil {
		return err
	}

	// For global daemon, we use "global" as the key
	reg.Daemons["global"] = registryEntry{
		PID:        os.Getpid(),
		StartedAt:  time.Now(),
		SocketPath: xdg.SocketPath(),
	}

	return saveRegistry(reg)
}

// UnregisterDaemon unregisters the global daemon.
func UnregisterDaemon(_ string) error {
	reg, err := loadRegistry()
	if err != nil {
		return err
	}

	delete(reg.Daemons, "global")
	return saveRegistry(reg)
}

// ListAllDaemons returns info about the global daemon if running.
func ListAllDaemons() ([]*DaemonInfo, error) {
	var infos []*DaemonInfo

	// Check if global daemon is running
	pid := GetRunningPID("")
	if pid != 0 {
		meta, _ := ReadMetadata("")
		startedAt := time.Time{}
		if meta != nil {
			startedAt = meta.StartedAt
		}
		info := &DaemonInfo{
			PID:        pid,
			SocketPath: xdg.SocketPath(),
			StartedAt:  startedAt,
			Uptime:     time.Since(startedAt),
			IsHealthy:  IsDaemonSocketAvailable(""),
			IsGlobal:   true,
		}
		infos = append(infos, info)
	}

	return infos, nil
}

func CleanupStaleRegistrations() error {
	reg, err := loadRegistry()
	if err != nil {
		return err
	}

	changed := false
	for key, entry := range reg.Daemons {
		if !IsProcessRunning(entry.PID) {
			delete(reg.Daemons, key)
			changed = true
		}
	}

	if changed {
		return saveRegistry(reg)
	}
	return nil
}

// KillDaemon kills the global daemon.
func KillDaemon(_ string) error {
	pid := GetRunningPID("")
	if pid == 0 {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if IsProcessRunning(pid) {
		process.Signal(syscall.SIGKILL)
		time.Sleep(100 * time.Millisecond)
	}

	RemovePID("")
	UnregisterDaemon("")

	// Clean up socket file
	os.Remove(xdg.SocketPath())

	return nil
}

// KillAllDaemons kills the global daemon (now only one daemon).
func KillAllDaemons() (int, error) {
	if IsRunning("") {
		if err := KillDaemon(""); err != nil {
			return 0, err
		}
		return 1, nil
	}
	return 0, nil
}

// KillLegacyDaemon kills a legacy per-project daemon if running.
func KillLegacyDaemon(projectRoot string) error {
	// Check if legacy daemon is running
	legacyPIDPath := LegacyPIDFilePath(projectRoot)
	data, err := os.ReadFile(legacyPIDPath)
	if err != nil {
		return nil // No legacy daemon
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		// Clean up invalid PID file
		os.Remove(legacyPIDPath)
		return nil
	}

	if !IsProcessRunning(pid) {
		// Clean up stale PID file
		os.Remove(legacyPIDPath)
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if IsProcessRunning(pid) {
		process.Signal(syscall.SIGKILL)
	}

	// Clean up legacy files
	os.Remove(legacyPIDPath)
	os.Remove(xdg.LegacySocketPath(projectRoot))
	os.Remove(LegacyLockFilePath(projectRoot))
	os.Remove(LegacyMetadataFilePath(projectRoot))

	return nil
}

// HasLegacyDaemon checks if a legacy per-project daemon exists.
func HasLegacyDaemon(projectRoot string) bool {
	return xdg.HasLegacyDaemon(projectRoot)
}
