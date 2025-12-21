package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var execCommand = exec.Command

// WorktreeConfig holds configuration for worktree creation
type WorktreeConfig struct {
	RepoRoot      string
	WorktreeRoot  string
	IssueID       string
	RunID         string
	BaseBranch    string
	Branch        string
	WorktreePath  string // Computed or provided
}

// WorktreeResult contains the result of worktree creation
type WorktreeResult struct {
	WorktreePath string
	Branch       string
	BaseBranch   string
}

// FindRepoRoot finds the git repository root from the current directory
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

// CreateWorktree creates a new git worktree for the run
func CreateWorktree(cfg *WorktreeConfig) (*WorktreeResult, error) {
	// Set defaults
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}

	// Generate branch name if not provided
	if cfg.Branch == "" {
		cfg.Branch = fmt.Sprintf("issue/%s/run-%s", cfg.IssueID, cfg.RunID)
	}

	// Generate worktree path if not provided
	if cfg.WorktreePath == "" {
		cfg.WorktreePath = filepath.Join(cfg.WorktreeRoot, cfg.IssueID, cfg.RunID)
	}

	// Ensure worktree parent directory exists
	if err := os.MkdirAll(filepath.Dir(cfg.WorktreePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktree parent directory: %w", err)
	}

	// Check if worktree already exists
	if _, err := os.Stat(cfg.WorktreePath); err == nil {
		return nil, fmt.Errorf("worktree path already exists: %s", cfg.WorktreePath)
	}

	// Fetch the base branch to ensure it's up to date
	fetchCmd := execCommand("git", "-C", cfg.RepoRoot, "fetch", "origin", cfg.BaseBranch)
	fetchCmd.Stderr = os.Stderr
	_ = fetchCmd.Run() // Ignore error, might not have remote

	// Create worktree with new branch
	args := []string{
		"-C", cfg.RepoRoot,
		"worktree", "add",
		"-b", cfg.Branch,
		cfg.WorktreePath,
		cfg.BaseBranch,
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

// RemoveWorktree removes a git worktree
func RemoveWorktree(repoRoot, worktreePath string) error {
	cmd := execCommand("git", "-C", repoRoot, "worktree", "remove", worktreePath, "--force")
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
