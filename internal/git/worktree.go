package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/s22625/orch/internal/model"
)

var execCommand = exec.Command

// WorktreeConfig holds configuration for worktree creation
type WorktreeConfig struct {
	RepoRoot     string
	WorktreeDir  string
	IssueID      string
	RunID        string
	Agent        string
	BaseBranch   string
	Branch       string
	WorktreePath string // Computed or provided
}

// WorktreeResult contains the result of worktree creation
type WorktreeResult struct {
	WorktreePath string
	Branch       string
	BaseBranch   string
}

// WorktreeInfo holds worktree metadata from git.
type WorktreeInfo struct {
	Path   string
	Branch string
}

func normalizeWorktreePath(cfg *WorktreeConfig) error {
	if cfg.WorktreePath == "" {
		worktreeName := model.GenerateWorktreeName(cfg.IssueID, cfg.RunID, cfg.Agent)
		cfg.WorktreePath = filepath.Join(cfg.WorktreeDir, cfg.IssueID, worktreeName)
	}

	if filepath.IsAbs(cfg.WorktreePath) {
		cfg.WorktreePath = filepath.Clean(cfg.WorktreePath)
		return nil
	}

	base := cfg.RepoRoot
	if base != "" && !filepath.IsAbs(base) {
		absBase, err := filepath.Abs(base)
		if err != nil {
			return fmt.Errorf("failed to resolve repo root: %w", err)
		}
		base = absBase
	}
	if base != "" {
		cfg.WorktreePath = filepath.Join(base, cfg.WorktreePath)
	}

	absPath, err := filepath.Abs(cfg.WorktreePath)
	if err != nil {
		return fmt.Errorf("failed to resolve worktree path: %w", err)
	}
	cfg.WorktreePath = absPath
	return nil
}

// FindRepoRoot finds the git repository root from the current directory
// Note: For worktrees, this returns the worktree directory, not the main repo.
// Use FindMainRepoRoot to get the main repository root.
func FindRepoRoot(startDir string) (string, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	cmd := execCommand("git", "-C", startDir, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// FindMainRepoRoot finds the main git repository root, even when inside a worktree.
// This uses --git-common-dir to find the shared .git directory, then returns its parent.
func FindMainRepoRoot(startDir string) (string, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	cmd := exec.Command("git", "-C", startDir, "rev-parse", "--git-common-dir")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}

	gitDir := strings.TrimSpace(string(output))

	// If gitDir is absolute, return its parent
	// If relative (like ".git"), resolve from startDir
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(startDir, gitDir)
	}

	// Return the parent of the .git directory
	return filepath.Dir(gitDir), nil
}

// parseRemoteBranch parses a branch ref like "origin/main" into remote and branch parts.
// If no "/" is found, returns empty remote and the original branch name.
func parseRemoteBranch(ref string) (remote, branch string) {
	if idx := strings.Index(ref, "/"); idx > 0 {
		return ref[:idx], ref[idx+1:]
	}
	return "", ref
}

// CreateWorktree creates a new git worktree for the run
func CreateWorktree(cfg *WorktreeConfig) (*WorktreeResult, error) {
	// Set defaults - use origin/main for remote-based workflow
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "origin/main"
	}

	// Generate branch name if not provided
	if cfg.Branch == "" {
		cfg.Branch = fmt.Sprintf("issue/%s/run-%s", cfg.IssueID, cfg.RunID)
	}

	if err := normalizeWorktreePath(cfg); err != nil {
		return nil, err
	}

	// Ensure worktree parent directory exists
	if err := os.MkdirAll(filepath.Dir(cfg.WorktreePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktree parent directory: %w", err)
	}

	// Check if worktree already exists
	if _, err := os.Stat(cfg.WorktreePath); err == nil {
		return nil, fmt.Errorf("worktree path already exists: %s", cfg.WorktreePath)
	}

	// Parse the base branch to extract remote and branch name
	remote, branchName := parseRemoteBranch(cfg.BaseBranch)

	// Fetch the remote branch to ensure it's up to date
	if remote != "" {
		// Fetch specific remote branch (e.g., "git fetch origin main")
		fetchCmd := execCommand("git", "-C", cfg.RepoRoot, "fetch", remote, branchName)
		fetchCmd.Stderr = os.Stderr
		_ = fetchCmd.Run() // Ignore error, might not have remote
	} else {
		// For local branches, fetch origin version if available
		fetchCmd := execCommand("git", "-C", cfg.RepoRoot, "fetch", "origin", branchName)
		fetchCmd.Stderr = os.Stderr
		_ = fetchCmd.Run() // Ignore error, might not have remote
	}

	// Determine the ref to use as base for the worktree
	// If remote branch specified (e.g., "origin/main"), use it directly
	// This ensures we branch from the remote state, not the local branch
	baseRef := cfg.BaseBranch

	// Create worktree with new branch
	args := []string{
		"-C", cfg.RepoRoot,
		"worktree", "add",
		"-b", cfg.Branch,
		cfg.WorktreePath,
		baseRef,
	}

	cmd := execCommand("git", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Try without -b if branch might exist
		args = []string{
			"-C", cfg.RepoRoot,
			"worktree", "add",
			cfg.WorktreePath,
			cfg.Branch,
		}
		cmd = execCommand("git", args...)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to create worktree: %w", err)
		}
	}

	return &WorktreeResult{
		WorktreePath: cfg.WorktreePath,
		Branch:       cfg.Branch,
		BaseBranch:   cfg.BaseBranch,
	}, nil
}

// CreateWorktreeFromBranch creates a worktree for an existing branch without creating a new branch.
func CreateWorktreeFromBranch(cfg *WorktreeConfig) (*WorktreeResult, error) {
	if cfg.RepoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}
	if cfg.Branch == "" {
		return nil, fmt.Errorf("branch is required")
	}

	if err := normalizeWorktreePath(cfg); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(cfg.WorktreePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktree parent directory: %w", err)
	}

	if _, err := os.Stat(cfg.WorktreePath); err == nil {
		return nil, fmt.Errorf("worktree path already exists: %s", cfg.WorktreePath)
	}

	args := []string{
		"-C", cfg.RepoRoot,
		"worktree", "add",
		cfg.WorktreePath,
		cfg.Branch,
	}
	cmd := execCommand("git", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	return &WorktreeResult{
		WorktreePath: cfg.WorktreePath,
		Branch:       cfg.Branch,
	}, nil
}

// RemoveWorktree removes a git worktree
func RemoveWorktree(repoRoot, worktreePath string) error {
	cmd := execCommand("git", "-C", repoRoot, "worktree", "remove", worktreePath, "--force")
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ListWorktreeInfos returns detailed worktree information for a repository.
func ListWorktreeInfos(repoRoot string) ([]WorktreeInfo, error) {
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot("")
		if err != nil {
			return nil, err
		}
	}

	cmd := execCommand("git", "-C", repoRoot, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var infos []WorktreeInfo
	var current *WorktreeInfo
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			if current != nil {
				infos = append(infos, *current)
			}
			current = &WorktreeInfo{Path: strings.TrimPrefix(line, "worktree ")}
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			if ref != "(detached)" {
				current.Branch = strings.TrimPrefix(ref, "refs/heads/")
			}
		}
	}
	if current != nil {
		infos = append(infos, *current)
	}

	return infos, nil
}

// FindWorktreesByBranch returns worktrees that have the specified branch checked out.
func FindWorktreesByBranch(repoRoot, branch string) ([]WorktreeInfo, error) {
	infos, err := ListWorktreeInfos(repoRoot)
	if err != nil {
		return nil, err
	}

	var matches []WorktreeInfo
	for _, info := range infos {
		if info.Branch == branch {
			matches = append(matches, info)
		}
	}
	return matches, nil
}

// ListWorktrees returns all worktrees for a repository
func ListWorktrees(repoRoot string) ([]string, error) {
	cmd := execCommand("git", "-C", repoRoot, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var worktrees []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			worktrees = append(worktrees, strings.TrimPrefix(line, "worktree "))
		}
	}

	return worktrees, nil
}

// GetCurrentBranch returns the current branch name
func GetCurrentBranch(repoPath string) (string, error) {
	cmd := execCommand("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
