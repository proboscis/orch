// Package xdg provides XDG Base Directory Spec-compliant path helpers for orch.
// See: https://specifications.freedesktop.org/basedir-spec/latest/
package xdg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/proboscis/orch/internal/model"
)

const appName = "orch"

// RuntimeDir returns the XDG runtime directory for orch.
// Falls back to /tmp/orch-{uid} if XDG_RUNTIME_DIR is not set.
// On macOS, falls back to ~/Library/Caches/orch/run.
func RuntimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, appName)
	}

	// macOS fallback: ~/Library/Caches/orch/run
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Caches", appName, "run")
		}
	}

	// Linux/Unix fallback: /tmp/orch-{uid}
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d", appName, os.Getuid()))
}

// StateDir returns the XDG state directory for orch.
// This is where daemon logs and other state files go.
// ~/.local/state/orch
func StateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, appName)
	}

	// macOS fallback: ~/Library/Logs/orch
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Logs", appName)
		}
	}

	// Default: ~/.local/state/orch
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", appName)
	}
	return filepath.Join(os.TempDir(), appName, "state")
}

// DataDir returns the XDG data directory for orch.
// This is where per-repo run data goes.
// ~/.local/share/orch
func DataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, appName)
	}

	// macOS fallback: ~/Library/Application Support/orch
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", appName)
		}
	}

	// Default: ~/.local/share/orch
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", appName)
	}
	return filepath.Join(os.TempDir(), appName, "data")
}

// ConfigDir returns the XDG config directory for orch (global config).
// ~/.config/orch
func ConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, appName)
	}

	// Default: ~/.config/orch (works on both macOS and Linux)
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", appName)
	}
	return filepath.Join(os.TempDir(), appName, "config")
}

// SocketPath returns the path to the global daemon socket.
func SocketPath() string {
	return filepath.Join(RuntimeDir(), "daemon.sock")
}

// PIDPath returns the path to the global daemon PID file.
func PIDPath() string {
	return filepath.Join(RuntimeDir(), "daemon.pid")
}

// LockPath returns the path to the global daemon lock file.
func LockPath() string {
	return filepath.Join(RuntimeDir(), "daemon.lock")
}

// MetadataPath returns the path to the global daemon metadata file.
func MetadataPath() string {
	return filepath.Join(RuntimeDir(), "daemon.json")
}

// LogPath returns the path to the daemon log file.
func LogPath() string {
	return filepath.Join(StateDir(), "daemon.log")
}

// WorkersStateDir returns the directory for local managed worker state.
func WorkersStateDir() string {
	return filepath.Join(StateDir(), "workers")
}

// WorkersRuntimeDir returns the directory for local managed worker runtime files.
func WorkersRuntimeDir() string {
	return filepath.Join(RuntimeDir(), "workers")
}

// StderrLogPath returns the path to the daemon stderr capture file.
// This captures Go panics and runtime errors that would otherwise be lost
// when the daemon runs in background mode (where stderr is /dev/null).
func StderrLogPath() string {
	return filepath.Join(StateDir(), "daemon-stderr.log")
}

// DaemonDBPath returns the path to the daemon state database.
// macOS: ~/Library/Application Support/orch/daemon.db
// Linux: ~/.local/share/orch/daemon.db
func DaemonDBPath() string {
	return filepath.Join(DataDir(), "daemon.db")
}

// RepoDataDir returns the data directory for a specific repo.
// ~/.local/share/orch/{repoID}/
func RepoDataDir(repoID string) string {
	return filepath.Join(DataDir(), repoID)
}

// RepoRunsDir returns the runs directory for a specific repo.
// ~/.local/share/orch/{repoID}/runs/
func RepoRunsDir(repoID string) string {
	return filepath.Join(RepoDataDir(repoID), "runs")
}

// GlobalDaemonStatePath returns the path to global daemon state.
// ~/.local/share/orch/global/daemon_state.json
func GlobalDaemonStatePath() string {
	return filepath.Join(DataDir(), "global", "daemon_state.json")
}

// EnsureDir creates a directory with the given permissions if it doesn't exist.
func EnsureDir(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// EnsureRuntimeDir creates the runtime directory with appropriate permissions.
func EnsureRuntimeDir() error {
	return EnsureDir(RuntimeDir(), 0700)
}

// EnsureStateDir creates the state directory with appropriate permissions.
func EnsureStateDir() error {
	return EnsureDir(StateDir(), 0755)
}

// EnsureWorkersStateDir creates the managed worker state directory.
func EnsureWorkersStateDir() error {
	return EnsureDir(WorkersStateDir(), 0755)
}

// EnsureWorkersRuntimeDir creates the managed worker runtime directory.
func EnsureWorkersRuntimeDir() error {
	return EnsureDir(WorkersRuntimeDir(), 0700)
}

// EnsureDataDir creates the data directory with appropriate permissions.
func EnsureDataDir() error {
	return EnsureDir(DataDir(), 0755)
}

// RepoID derives a repo identifier strictly from the git remote URL.
// Returns "owner-repo" format (e.g., "proboscis-orch").
func RepoID(projectRoot string) (model.RepoID, error) {
	return repoIDFromRemote(projectRoot)
}

// RepoIDStrict derives repo identifier strictly from git remote URL.
// Unlike RepoID, it does not fall back to a path-derived identifier.
func RepoIDStrict(projectRoot string) (model.RepoID, error) {
	return repoIDFromRemote(projectRoot)
}

func repoIDFromRemote(projectRoot string) (model.RepoID, error) {
	cmd := exec.Command("git", "-C", projectRoot, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve git remote for %s: %w", projectRoot, err)
	}

	remoteURL := strings.TrimSpace(string(output))
	if remoteURL == "" {
		return "", fmt.Errorf("empty git remote URL for %s", projectRoot)
	}

	return ParseRepoID(remoteURL)
}

// LegacyRepoID returns the bare basename that was used before collision-safe
// hashing was added. Useful for migration lookups.
func LegacyRepoID(projectRoot string) string {
	return filepath.Base(projectRoot)
}

// ParseRepoID extracts owner-repo from a git remote URL.
// Handles various formats:
//   - https://github.com/owner/repo.git
//   - git@github.com:owner/repo.git
//   - git://github.com/owner/repo.git
//   - ssh://git@github.com/owner/repo.git
func ParseRepoID(remoteURL string) (model.RepoID, error) {
	return model.NewRepoID(remoteURL)
}

// sanitizeRepoID creates a safe directory name from owner and repo.
func sanitizeRepoID(owner, repo string) string {
	id, err := model.NewRepoID(owner + "/" + repo)
	if err != nil {
		return ""
	}
	return string(id)
}

// LegacyOrchDir returns the legacy .orch directory path for a project.
// Used for migration and backward compatibility.
func LegacyOrchDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".orch")
}

// LegacySocketPath returns the legacy socket path in .orch/.
func LegacySocketPath(projectRoot string) string {
	return filepath.Join(LegacyOrchDir(projectRoot), "daemon.sock")
}

// LegacyPIDPath returns the legacy PID file path in .orch/.
func LegacyPIDPath(projectRoot string) string {
	return filepath.Join(LegacyOrchDir(projectRoot), "daemon.pid")
}

// LegacyRunsDir returns the legacy runs directory in the issues root.
func LegacyRunsDir(issuesRoot string) string {
	return filepath.Join(issuesRoot, "runs")
}

// HasLegacyDaemon checks if a legacy per-project daemon is running.
func HasLegacyDaemon(projectRoot string) bool {
	sockPath := LegacySocketPath(projectRoot)
	_, err := os.Stat(sockPath)
	return err == nil
}

// NeedsMigration checks if a project has legacy .orch/runs data that should be migrated.
func NeedsMigration(issuesRoot string) bool {
	legacyRuns := LegacyRunsDir(issuesRoot)
	info, err := os.Stat(legacyRuns)
	return err == nil && info.IsDir()
}
