package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/model"
)

func runGit(t *testing.T, dir string, args ...string) string {
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
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
	runGit(t, dir, "branch", "-M", "main")
	runGit(t, dir, "remote", "add", "origin", dir)
	// Fetch to create remote tracking branch (origin/main)
	// This is needed because CreateWorktree uses origin/main as the base
	runGit(t, dir, "fetch", "origin")
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
		WorktreeDir: worktreeRoot,
		IssueID:      "issue",
		RunID:        "run",
		Agent:        "claude",
	})
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}

	worktreeName := model.GenerateWorktreeName("issue", "run", "claude")
	wantPath := filepath.Join(worktreeRoot, "issue", worktreeName)
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

func TestCreateWorktreeRelativeRootUsesRepoRoot(t *testing.T) {
	repo := initRepo(t)
	outside := t.TempDir()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(outside); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	result, err := CreateWorktree(&WorktreeConfig{
		RepoRoot:     repo,
		WorktreeDir: ".git-worktrees",
		IssueID:      "issue",
		RunID:        "run",
		Agent:        "claude",
	})
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}

	worktreeName := model.GenerateWorktreeName("issue", "run", "claude")
	wantPath := filepath.Join(repo, ".git-worktrees", "issue", worktreeName)
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
	if !filepath.IsAbs(result.WorktreePath) {
		t.Fatalf("WorktreePath is not absolute: %q", result.WorktreePath)
	}
	if _, err := os.Stat(result.WorktreePath); err != nil {
		t.Fatalf("worktree missing: %v", err)
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

func TestCreateWorktreeFromBranch(t *testing.T) {
	repo := initRepo(t)
	runGit(t, repo, "branch", "feature-branch")

	worktreeRoot := filepath.Join(repo, ".git-worktrees")
	result, err := CreateWorktreeFromBranch(&WorktreeConfig{
		RepoRoot:     repo,
		WorktreeDir: worktreeRoot,
		IssueID:      "issue",
		RunID:        "run",
		Agent:        "claude",
		Branch:       "feature-branch",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeFromBranch error: %v", err)
	}

	if result.Branch != "feature-branch" {
		t.Fatalf("Branch = %q, want %q", result.Branch, "feature-branch")
	}
	if _, err := os.Stat(result.WorktreePath); err != nil {
		t.Fatalf("worktree missing: %v", err)
	}

	branch, err := GetCurrentBranch(result.WorktreePath)
	if err != nil {
		t.Fatalf("GetCurrentBranch error: %v", err)
	}
	if branch != "feature-branch" {
		t.Fatalf("worktree branch = %q, want %q", branch, "feature-branch")
	}
}

func TestListWorktreeInfos(t *testing.T) {
	repo := initRepo(t)
	worktreeRoot := filepath.Join(repo, ".git-worktrees")
	result, err := CreateWorktree(&WorktreeConfig{
		RepoRoot:     repo,
		WorktreeDir: worktreeRoot,
		IssueID:      "issue",
		RunID:        "run",
		Agent:        "claude",
	})
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}

	infos, err := ListWorktreeInfos(repo)
	if err != nil {
		t.Fatalf("ListWorktreeInfos error: %v", err)
	}

	if !containsWorktreeInfo(infos, repo, "main") {
		t.Fatalf("expected main worktree info for %s", repo)
	}
	if !containsWorktreeInfo(infos, result.WorktreePath, result.Branch) {
		t.Fatalf("expected worktree info for %s branch %s", result.WorktreePath, result.Branch)
	}
}

func TestFindWorktreesByBranch(t *testing.T) {
	repo := initRepo(t)
	worktreeRoot := filepath.Join(repo, ".git-worktrees")
	result, err := CreateWorktree(&WorktreeConfig{
		RepoRoot:     repo,
		WorktreeDir: worktreeRoot,
		IssueID:      "issue",
		RunID:        "run",
		Agent:        "claude",
	})
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}

	matches, err := FindWorktreesByBranch(repo, result.Branch)
	if err != nil {
		t.Fatalf("FindWorktreesByBranch error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if !containsPath([]string{matches[0].Path}, result.WorktreePath) {
		t.Fatalf("unexpected match path: %s", matches[0].Path)
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

func containsWorktreeInfo(infos []WorktreeInfo, wantPath, wantBranch string) bool {
	wantEval, err := filepath.EvalSymlinks(wantPath)
	if err != nil {
		wantEval = wantPath
	}
	for _, info := range infos {
		gotEval, err := filepath.EvalSymlinks(info.Path)
		if err != nil {
			gotEval = info.Path
		}
		if gotEval == wantEval && info.Branch == wantBranch {
			return true
		}
	}
	return false
}

func TestParseRemoteBranch(t *testing.T) {
	tests := []struct {
		name         string
		baseBranch   string
		wantRemote   string
		wantBranch   string
	}{
		{
			name:       "empty defaults to origin/main",
			baseBranch: "",
			wantRemote: "origin",
			wantBranch: "main",
		},
		{
			name:       "simple branch defaults to origin",
			baseBranch: "main",
			wantRemote: "origin",
			wantBranch: "main",
		},
		{
			name:       "develop branch defaults to origin",
			baseBranch: "develop",
			wantRemote: "origin",
			wantBranch: "develop",
		},
		{
			name:       "origin/main is parsed correctly",
			baseBranch: "origin/main",
			wantRemote: "origin",
			wantBranch: "main",
		},
		{
			name:       "upstream/develop is parsed correctly",
			baseBranch: "upstream/develop",
			wantRemote: "upstream",
			wantBranch: "develop",
		},
		{
			name:       "fork/feature-branch is parsed correctly",
			baseBranch: "fork/feature-branch",
			wantRemote: "fork",
			wantBranch: "feature-branch",
		},
		{
			name:       "origin/release/v1.0 handles nested paths",
			baseBranch: "origin/release/v1.0",
			wantRemote: "origin",
			wantBranch: "release/v1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRemote, gotBranch := ParseRemoteBranch(tt.baseBranch)
			if gotRemote != tt.wantRemote {
				t.Errorf("ParseRemoteBranch(%q) remote = %q, want %q", tt.baseBranch, gotRemote, tt.wantRemote)
			}
			if gotBranch != tt.wantBranch {
				t.Errorf("ParseRemoteBranch(%q) branch = %q, want %q", tt.baseBranch, gotBranch, tt.wantBranch)
			}
		})
	}
}

func TestRemoteBranchRef(t *testing.T) {
	tests := []struct {
		remote string
		branch string
		want   string
	}{
		{"origin", "main", "origin/main"},
		{"upstream", "develop", "upstream/develop"},
		{"fork", "feature", "fork/feature"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := RemoteBranchRef(tt.remote, tt.branch)
			if got != tt.want {
				t.Errorf("RemoteBranchRef(%q, %q) = %q, want %q", tt.remote, tt.branch, got, tt.want)
			}
		})
	}
}

func TestCreateWorktreeUsesRemoteBranch(t *testing.T) {
	repo := initRepo(t)
	worktreeRoot := filepath.Join(repo, ".git-worktrees")

	result, err := CreateWorktree(&WorktreeConfig{
		RepoRoot:    repo,
		WorktreeDir: worktreeRoot,
		IssueID:     "issue",
		RunID:       "run",
		Agent:       "claude",
		BaseBranch:  "main", // Simple branch name
	})
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}

	// Verify that BaseBranch is returned as remote ref (origin/main)
	if result.BaseBranch != "origin/main" {
		t.Errorf("BaseBranch = %q, want %q", result.BaseBranch, "origin/main")
	}
}

func TestCreateWorktreeWithExplicitRemoteBranch(t *testing.T) {
	repo := initRepo(t)
	worktreeRoot := filepath.Join(repo, ".git-worktrees")

	result, err := CreateWorktree(&WorktreeConfig{
		RepoRoot:    repo,
		WorktreeDir: worktreeRoot,
		IssueID:     "issue2",
		RunID:       "run2",
		Agent:       "claude",
		BaseBranch:  "origin/main", // Explicit remote/branch format
	})
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}

	// Verify that BaseBranch is preserved as origin/main
	if result.BaseBranch != "origin/main" {
		t.Errorf("BaseBranch = %q, want %q", result.BaseBranch, "origin/main")
	}
}

func TestCreateWorktreeFallsBackToLocalBranch(t *testing.T) {
	// Create a repo without a remote to test fallback behavior
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
	runGit(t, dir, "branch", "-M", "main")
	// Note: No remote added

	worktreeRoot := filepath.Join(dir, ".git-worktrees")
	result, err := CreateWorktree(&WorktreeConfig{
		RepoRoot:    dir,
		WorktreeDir: worktreeRoot,
		IssueID:     "issue",
		RunID:       "run",
		Agent:       "claude",
		BaseBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}

	// Should fall back to local branch "main" since origin/main doesn't exist
	if result.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q, want %q (fallback to local branch)", result.BaseBranch, "main")
	}

	if _, err := os.Stat(result.WorktreePath); err != nil {
		t.Fatalf("worktree missing: %v", err)
	}
}
