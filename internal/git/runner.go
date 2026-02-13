package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner defines git command execution boundaries used by higher layers.
type Runner interface {
	StatusPorcelain(ctx context.Context, workDir string) (string, error)
	CurrentBranch(ctx context.Context, workDir string) (string, error)
	MergedBranches(ctx context.Context, workDir, baseBranch string) ([]string, error)
	Diff(ctx context.Context, workDir string, args ...string) (string, error)
	DeleteBranch(ctx context.Context, repoRoot, branch string) error
}

type commandRunner struct{}

func NewRunner() Runner {
	return &commandRunner{}
}

func (r *commandRunner) StatusPorcelain(ctx context.Context, workDir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", workDir, "status", "--porcelain").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (r *commandRunner) CurrentBranch(ctx context.Context, workDir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", workDir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *commandRunner) MergedBranches(ctx context.Context, workDir, baseBranch string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", workDir, "branch", "--merged", baseBranch, "--format=%(refname:short)").Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(out), "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line == "" {
			continue
		}
		branches = append(branches, line)
	}
	return branches, nil
}

func (r *commandRunner) Diff(ctx context.Context, workDir string, args ...string) (string, error) {
	cmdArgs := []string{"-C", workDir, "diff"}
	cmdArgs = append(cmdArgs, args...)
	out, err := exec.CommandContext(ctx, "git", cmdArgs...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (r *commandRunner) DeleteBranch(ctx context.Context, repoRoot, branch string) error {
	if strings.TrimSpace(branch) == "" {
		return fmt.Errorf("branch required")
	}
	return exec.CommandContext(ctx, "git", "-C", repoRoot, "branch", "-D", branch).Run()
}
