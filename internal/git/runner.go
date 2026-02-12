package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner defines git command boundaries used by higher layers.
type Runner interface {
	Status(ctx context.Context, repoDir string) (string, error)
	Branch(ctx context.Context, repoDir string) (string, error)
	Diff(ctx context.Context, repoDir string, args ...string) (string, error)
	ListMergedBranches(ctx context.Context, repoDir, baseBranch string) ([]string, error)
	DeleteBranch(ctx context.Context, repoDir, branch string) error
}

// CommandRunner executes git commands via the git binary.
type CommandRunner struct{}

func NewRunner() Runner {
	return &CommandRunner{}
}

func (r *CommandRunner) Status(ctx context.Context, repoDir string) (string, error) {
	return r.runOutput(ctx, repoDir, "status", "--porcelain")
}

func (r *CommandRunner) Branch(ctx context.Context, repoDir string) (string, error) {
	out, err := r.runOutput(ctx, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *CommandRunner) Diff(ctx context.Context, repoDir string, args ...string) (string, error) {
	gitArgs := append([]string{"diff"}, args...)
	return r.runOutput(ctx, repoDir, gitArgs...)
}

func (r *CommandRunner) ListMergedBranches(ctx context.Context, repoDir, baseBranch string) ([]string, error) {
	out, err := r.runOutput(ctx, repoDir, "branch", "--merged", baseBranch, "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(out, "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		branches = append(branches, trimmed)
	}

	return branches, nil
}

func (r *CommandRunner) DeleteBranch(ctx context.Context, repoDir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "branch", "-D", branch)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git branch -D %s: %w", branch, err)
	}
	return nil
}

func (r *CommandRunner) runOutput(ctx context.Context, repoDir string, gitArgs ...string) (string, error) {
	args := append([]string{"-C", repoDir}, gitArgs...)
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(gitArgs, " "), err)
	}
	return string(out), nil
}
