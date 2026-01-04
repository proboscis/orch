package git

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const (
	// DefaultFetchTimeout is the maximum time to wait for a fetch operation.
	DefaultFetchTimeout = 30 * time.Second
)

// Fetch performs a git fetch origin for the given repository.
// It uses a context with timeout to prevent hanging on network issues.
// If target is provided, only that branch is fetched for efficiency.
func Fetch(repoRoot, target string) error {
	return FetchWithTimeout(repoRoot, target, DefaultFetchTimeout)
}

// FetchWithTimeout performs a git fetch with a custom timeout.
func FetchWithTimeout(repoRoot, target string, timeout time.Duration) error {
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot("")
		if err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var cmd *exec.Cmd
	if target != "" {
		refspec := fmt.Sprintf("%s:refs/remotes/origin/%s", target, target)
		cmd = exec.CommandContext(ctx, "git", "-C", repoRoot, "fetch", "origin", refspec)
	} else {
		cmd = exec.CommandContext(ctx, "git", "-C", repoRoot, "fetch", "origin")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("git fetch timed out after %v", timeout)
		}
		return fmt.Errorf("git fetch origin: %w (output: %s)", err, string(output))
	}

	return nil
}
