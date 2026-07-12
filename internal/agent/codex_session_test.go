package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRollout(t *testing.T, codexHome string, day time.Time, name, firstLine string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(codexHome, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	content := firstLine + "\n" + `{"type":"response_item","payload":{}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

func sessionMetaLine(id, cwd string) string {
	return `{"timestamp":"2026-07-13T03:00:00.000Z","type":"session_meta","payload":{"id":"` + id + `","timestamp":"2026-07-13T03:00:00.000Z","cwd":"` + cwd + `","originator":"codex_cli_rs","cli_version":"0.9.0"}}`
}

func TestResolveCodexSessionIDMatchesByCwdNotNewest(t *testing.T) {
	codexHome := t.TempDir()
	worktree := filepath.Join(t.TempDir(), "wt-run-1")
	if err := os.MkdirAll(worktree, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	// The MATCHING rollout is older than a non-matching one: the resolver
	// must match by payload.cwd, never by newest-file-only.
	writeRollout(t, codexHome, now, "rollout-2026-07-13T03-00-00-aaaa.jsonl",
		sessionMetaLine("target-session-id", worktree), now.Add(-2*time.Minute))
	writeRollout(t, codexHome, now, "rollout-2026-07-13T03-05-00-bbbb.jsonl",
		sessionMetaLine("other-session-id", "/some/other/worktree"), now.Add(-1*time.Minute))

	id, err := ResolveCodexSessionID(codexHome, worktree, now)
	if err != nil {
		t.Fatalf("ResolveCodexSessionID() error = %v", err)
	}
	if id != "target-session-id" {
		t.Fatalf("id = %q, want target-session-id (matched by cwd)", id)
	}
}

func TestResolveCodexSessionIDNoMatch(t *testing.T) {
	codexHome := t.TempDir()
	now := time.Now()
	writeRollout(t, codexHome, now, "rollout-2026-07-13T03-00-00-aaaa.jsonl",
		sessionMetaLine("other-session-id", "/some/other/worktree"), now)

	_, err := ResolveCodexSessionID(codexHome, "/wanted/worktree", now)
	if err == nil {
		t.Fatal("expected error when no rollout matches the worktree cwd")
	}
	if !strings.Contains(err.Error(), "/wanted/worktree") {
		t.Fatalf("error should name the unmatched worktree, got: %v", err)
	}
}

func TestResolveCodexSessionIDSkipsMalformedFirstLine(t *testing.T) {
	codexHome := t.TempDir()
	worktree := "/wanted/worktree"
	now := time.Now()

	// Newest file has a malformed first line; the resolver must skip it and
	// keep scanning instead of failing or mismatching.
	writeRollout(t, codexHome, now, "rollout-2026-07-13T03-05-00-bbbb.jsonl",
		"{not json", now.Add(-1*time.Minute))
	// A non-session_meta first line is also skipped, not matched.
	writeRollout(t, codexHome, now, "rollout-2026-07-13T03-04-00-cccc.jsonl",
		`{"type":"response_item","payload":{"id":"impostor","cwd":"`+worktree+`"}}`, now.Add(-90*time.Second))
	writeRollout(t, codexHome, now, "rollout-2026-07-13T03-00-00-aaaa.jsonl",
		sessionMetaLine("target-session-id", worktree), now.Add(-2*time.Minute))

	id, err := ResolveCodexSessionID(codexHome, worktree, now)
	if err != nil {
		t.Fatalf("ResolveCodexSessionID() error = %v", err)
	}
	if id != "target-session-id" {
		t.Fatalf("id = %q, want target-session-id", id)
	}
}

func TestResolveCodexSessionIDFindsYesterdayDir(t *testing.T) {
	// A run launched just before midnight writes its rollout under
	// yesterday's date directory relative to the resolver's clock.
	codexHome := t.TempDir()
	worktree := "/wanted/worktree"
	now := time.Now()
	writeRollout(t, codexHome, now.AddDate(0, 0, -1), "rollout-2026-07-12T23-59-59-aaaa.jsonl",
		sessionMetaLine("target-session-id", worktree), now.Add(-5*time.Minute))

	id, err := ResolveCodexSessionID(codexHome, worktree, now)
	if err != nil {
		t.Fatalf("ResolveCodexSessionID() error = %v", err)
	}
	if id != "target-session-id" {
		t.Fatalf("id = %q, want target-session-id (yesterday's date dir scanned)", id)
	}
}

func TestResolveCodexSessionIDMissingSessionsDir(t *testing.T) {
	codexHome := t.TempDir() // no sessions/ subtree at all
	_, err := ResolveCodexSessionID(codexHome, "/wanted/worktree", time.Now())
	if err == nil {
		t.Fatal("expected error for missing sessions dir")
	}
}

func TestResolveCodexSessionIDWithRetryFindsLateRollout(t *testing.T) {
	// codex writes the rollout at boot, some time after the session spawns;
	// the resolver must keep polling until it appears.
	codexHome := t.TempDir()
	worktree := "/wanted/worktree"

	go func() {
		time.Sleep(300 * time.Millisecond)
		day := time.Now()
		dir := filepath.Join(codexHome, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"))
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "rollout-late.jsonl"),
			[]byte(sessionMetaLine("late-session-id", worktree)+"\n"), 0644)
	}()

	id, err := ResolveCodexSessionIDWithRetry(codexHome, worktree, 5*time.Second)
	if err != nil {
		t.Fatalf("ResolveCodexSessionIDWithRetry() error = %v", err)
	}
	if id != "late-session-id" {
		t.Fatalf("id = %q, want late-session-id", id)
	}
}

func TestResolveCodexSessionIDWithRetryTimesOutLoudly(t *testing.T) {
	codexHome := t.TempDir()
	start := time.Now()
	_, err := ResolveCodexSessionIDWithRetry(codexHome, "/wanted/worktree", 400*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("retry ran %s, want it bounded by the ~400ms budget", elapsed)
	}
	// The failure must name the concrete miss so the error artifact is actionable.
	if !strings.Contains(err.Error(), codexHome) || !strings.Contains(err.Error(), "/wanted/worktree") {
		t.Fatalf("error should name CODEX_HOME and the worktree, got: %v", err)
	}
}

func TestLaunchConfigCodexSessionsHome(t *testing.T) {
	t.Setenv("HOME", "/home/exec-host")
	t.Setenv("CODEX_HOME", "")

	// Profile-resolved CODEX_HOME wins, with ~ expanded on the execution host.
	cfg := &LaunchConfig{CodexHome: "~/.codex-company"}
	if got := cfg.CodexSessionsHome(); got != "/home/exec-host/.codex-company" {
		t.Fatalf("CodexSessionsHome() = %q, want /home/exec-host/.codex-company", got)
	}

	// No profile: the ambient CODEX_HOME (what the spawned codex sees) wins.
	t.Setenv("CODEX_HOME", "/opt/codex-home")
	if got := (&LaunchConfig{}).CodexSessionsHome(); got != "/opt/codex-home" {
		t.Fatalf("CodexSessionsHome() = %q, want /opt/codex-home", got)
	}

	// Neither: the codex default ~/.codex.
	t.Setenv("CODEX_HOME", "")
	if got := (&LaunchConfig{}).CodexSessionsHome(); got != "/home/exec-host/.codex" {
		t.Fatalf("CodexSessionsHome() = %q, want /home/exec-host/.codex", got)
	}
}
