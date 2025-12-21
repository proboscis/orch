package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error: %v (%s)", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

func initRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitCmd(t, dir, "add", "README.md")
	runGitCmd(t, dir, "commit", "-m", "init")
	runGitCmd(t, dir, "branch", "-M", "main")
	runGitCmd(t, dir, "remote", "add", "origin", dir)
	return dir
}

func TestFindRepoRoot(t *testing.T) {
	repo := initRepo(t)
	sub := filepath.Join(repo, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	root, err := FindRepoRoot(sub)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	rootEval, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks root: %v", err)
	}
	repoEval, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks repo: %v", err)
	}
	if rootEval != repoEval {
		t.Fatalf("FindRepoRoot = %q, want %q", rootEval, repoEval)
	}
}

func TestFindRepoRootNotRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindRepoRoot(dir); err == nil {
		t.Fatal("expected error for non-repo directory")
	}
}

func TestCreateWorktree(t *testing.T) {
	repo := initRepo(t)
	worktreeRoot := filepath.Join(repo, ".git-worktrees")

	result, err := CreateWorktree(&WorktreeConfig{
		RepoRoot:     repo,
		WorktreeRoot: worktreeRoot,
		IssueID:      "issue",
		RunID:        "run",
	})
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}

	wantPath := filepath.Join(worktreeRoot, "issue", "run")
	gotPath, err := filepath.EvalSymlinks(result.WorktreePath)
	if err != nil {
		t.Fatalf("EvalSymlinks worktree: %v", err)
	}
	wantEval, err := filepath.EvalSymlinks(wantPath)
	if err != nil {
		t.Fatalf("EvalSymlinks want: %v", err)
	}
	if gotPath != wantEval {
		t.Fatalf("WorktreePath = %q, want %q", gotPath, wantEval)
	}
	if result.Branch != "issue/issue/run-run" {
		t.Fatalf("Branch = %q, want %q", result.Branch, "issue/issue/run-run")
	}
	if _, err := os.Stat(result.WorktreePath); err != nil {
		t.Fatalf("worktree missing: %v", err)
	}

	trees, err := ListWorktrees(repo)
	if err != nil {
		t.Fatalf("ListWorktrees error: %v", err)
	}
	if !containsPath(trees, repo) || !containsPath(trees, result.WorktreePath) {
		t.Fatalf("worktrees missing expected paths: %v", trees)
	}

	branch, err := GetCurrentBranch(repo)
	if err != nil {
		t.Fatalf("GetCurrentBranch error: %v", err)
	}
	if branch != "main" {
		t.Fatalf("branch = %q, want %q", branch, "main")
	}
}

func TestCreateWorktreePathExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := CreateWorktree(&WorktreeConfig{WorktreePath: dir}); err == nil {
		t.Fatal("expected error for existing worktree path")
	}
}

func containsPath(paths []string, want string) bool {
	wantEval, err := filepath.EvalSymlinks(want)
	if err != nil {
		wantEval = want
	}
	for _, p := range paths {
		gotEval, err := filepath.EvalSymlinks(p)
		if err != nil {
			gotEval = p
		}
		if gotEval == wantEval {
			return true
		}
	}
	return false
}
