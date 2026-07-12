package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// NewAgentSessionID mints the agent-native session id orch assigns to a
// claude launch (ADR-0005 R1). claude accepts any UUID via --session-id and
// pins its transcript file to it, making the session addressable for
// reap/revive from the recorded agent_session artifact.
func NewAgentSessionID() string {
	return uuid.NewString()
}

// CodexSessionsHome returns the CODEX_HOME the spawned codex process
// resolves on the execution host: the profile-selected dir when set
// (~ expanded here, like CodexHomeEnv), else the ambient CODEX_HOME the
// session inherits, else the codex default ~/.codex.
func (c *LaunchConfig) CodexSessionsHome() string {
	if home := expandHomePath(c.CodexHome); home != "" {
		return home
	}
	if env := strings.TrimSpace(os.Getenv("CODEX_HOME")); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".codex")
}

// codexSessionScanDays bounds the rollout scan to today's and yesterday's
// date directories: the rollout we resolve was written seconds-to-minutes
// ago, and two days covers midnight boundaries and timezone skew.
const codexSessionScanDays = 2

type codexSessionMeta struct {
	Type    string `json:"type"`
	Payload struct {
		ID  string `json:"id"`
		Cwd string `json:"cwd"`
	} `json:"payload"`
}

// ResolveCodexSessionID scans codexHome/sessions/YYYY/MM/DD/rollout-*.jsonl
// (relative to now) newest-first and returns the rollout session id whose
// first-line session_meta payload.cwd is this run's worktree. Worktree paths
// are unique per run, so a cwd match is an identity; matching newest-only is
// forbidden (concurrent launches would swap identities). A file whose first
// line is malformed or not a session_meta is skipped, never matched.
func ResolveCodexSessionID(codexHome, worktreePath string, now time.Time) (string, error) {
	sessionsDir := filepath.Join(codexHome, "sessions")
	candidates := codexRolloutCandidates(sessionsDir, now)
	if len(candidates) == 0 {
		return "", fmt.Errorf("no codex rollout files under %s (wanted session_meta cwd == %s); codex writes the rollout at boot", sessionsDir, worktreePath)
	}

	wantRaw := strings.TrimSpace(worktreePath)
	wantResolved := resolvePathForCwdMatch(wantRaw)
	for _, candidate := range candidates {
		meta, ok := readCodexSessionMeta(candidate)
		if !ok || meta.Type != "session_meta" {
			continue
		}
		id := strings.TrimSpace(meta.Payload.ID)
		cwd := strings.TrimSpace(meta.Payload.Cwd)
		if id == "" || cwd == "" {
			continue
		}
		if cwd == wantRaw || resolvePathForCwdMatch(cwd) == wantResolved {
			return id, nil
		}
	}
	return "", fmt.Errorf("no codex rollout with session_meta cwd == %s under %s (scanned %d files)", worktreePath, sessionsDir, len(candidates))
}

// ResolveCodexSessionIDWithRetry polls ResolveCodexSessionID until timeout:
// codex writes the rollout at boot, typically seconds after the session
// spawns, so the resolver backs off gently instead of hammering the
// filesystem. The returned error names CODEX_HOME and the worktree so the
// recorded miss is actionable.
func ResolveCodexSessionIDWithRetry(codexHome, worktreePath string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	delay := 250 * time.Millisecond
	for {
		id, err := ResolveCodexSessionID(codexHome, worktreePath, time.Now())
		if err == nil {
			return id, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", fmt.Errorf("codex session id unresolved after %s: %w", timeout, err)
		}
		if delay > remaining {
			delay = remaining
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay *= 2
		}
	}
}

// codexRolloutCandidates lists rollout files from the most recent date
// directories, newest-first by modification time. A missing date directory
// just means no rollouts that day.
func codexRolloutCandidates(sessionsDir string, now time.Time) []string {
	type fileWithTime struct {
		path  string
		mtime time.Time
	}
	var files []fileWithTime
	for d := 0; d < codexSessionScanDays; d++ {
		day := now.AddDate(0, 0, -d)
		dir := filepath.Join(sessionsDir, day.Format("2006"), day.Format("01"), day.Format("02"))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, fileWithTime{path: filepath.Join(dir, name), mtime: info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime.After(files[j].mtime) })
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	return paths
}

// readCodexSessionMeta parses the first line of a rollout file. ok=false for
// unreadable files or malformed JSON — the caller skips those.
func readCodexSessionMeta(path string) (codexSessionMeta, bool) {
	var meta codexSessionMeta
	f, err := os.Open(path)
	if err != nil {
		return meta, false
	}
	defer f.Close()

	reader := bufio.NewReaderSize(f, 64*1024)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return meta, false
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &meta); err != nil {
		return meta, false
	}
	return meta, true
}

// resolvePathForCwdMatch normalizes a path for cwd equality (e.g. macOS
// /tmp -> /private/tmp): codex records the cwd as its process saw it, which
// may differ from the path orch launched with by symlink resolution only.
func resolvePathForCwdMatch(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		return resolved
	}
	return path
}
