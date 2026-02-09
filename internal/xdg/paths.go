// Package xdg provides XDG Base Directory Spec-compliant path helpers for orch.
// See: https://specifications.freedesktop.org/basedir-spec/latest/
package xdg

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
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

// EnsureDataDir creates the data directory with appropriate permissions.
func EnsureDataDir() error {
	return EnsureDir(DataDir(), 0755)
}

// RepoID derives a repo identifier from the git remote URL.
// Returns "owner-repo" format (e.g., "proboscis-orch").
// Falls back to directory name if no git remote is found.
func RepoID(projectRoot string) (string, error) {
	// Try to get remote URL
	cmd := exec.Command("git", "-C", projectRoot, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to directory name with path-derived hash suffix
		// to avoid collisions when different repos share the same basename
		cleaned := filepath.Clean(projectRoot)
		h := sha256.Sum256([]byte(cleaned))
		return fmt.Sprintf("%s-%x", filepath.Base(cleaned), h[:4]), nil
	}

	remoteURL := strings.TrimSpace(string(output))
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
func ParseRepoID(remoteURL string) (string, error) {
	if remoteURL == "" {
		return "", fmt.Errorf("empty remote URL")
	}

	// SSH format: git@github.com:owner/repo.git
	sshPattern := regexp.MustCompile(`^git@[^:]+:([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := sshPattern.FindStringSubmatch(remoteURL); len(matches) == 3 {
		return sanitizeRepoID(matches[1], matches[2]), nil
	}

	// HTTPS/Git format: https://github.com/owner/repo.git
	httpsPattern := regexp.MustCompile(`^(?:https?|git|ssh)://[^/]+/([^/]+)/([^/]+?)(?:\.git)?/?$`)
	if matches := httpsPattern.FindStringSubmatch(remoteURL); len(matches) == 3 {
		return sanitizeRepoID(matches[1], matches[2]), nil
	}

	// Last resort: try to extract from path
	// Remove .git suffix and split by /
	cleaned := strings.TrimSuffix(remoteURL, ".git")
	parts := strings.Split(cleaned, "/")
	if len(parts) >= 2 {
		owner := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		// Handle colon in SSH format
		if idx := strings.LastIndex(owner, ":"); idx != -1 {
			owner = owner[idx+1:]
		}
		return sanitizeRepoID(owner, repo), nil
	}

	return "", fmt.Errorf("unable to parse remote URL: %s", remoteURL)
}

// sanitizeRepoID creates a safe directory name from owner and repo.
func sanitizeRepoID(owner, repo string) string {
	// Remove any unsafe characters, keeping alphanumeric, dash, underscore
	safePattern := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	safeOwner := safePattern.ReplaceAllString(owner, "")
	safeRepo := safePattern.ReplaceAllString(repo, "")
	return safeOwner + "-" + safeRepo
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
