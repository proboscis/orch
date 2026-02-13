package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func createMainRepoWithTwoWorktrees(t *testing.T) (repoRoot string, worktreeA string, worktreeB string) {
	t.Helper()

	baseDir := t.TempDir()
	repoRoot = filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(repoRoot, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "orch-tests@example.com")
	runGit(t, repoRoot, "config", "user.name", "Orch Tests")

	readmePath := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readmePath, []byte("test\n"), 0644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "initial commit")

	worktreeA = filepath.Join(baseDir, "worktree-a")
	worktreeB = filepath.Join(baseDir, "worktree-b")
	runGit(t, repoRoot, "worktree", "add", "-b", "feature-a", worktreeA, "HEAD")
	runGit(t, repoRoot, "worktree", "add", "-b", "feature-b", worktreeB, "HEAD")

	return repoRoot, worktreeA, worktreeB
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}
