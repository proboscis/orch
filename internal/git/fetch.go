package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
		cmd = exec.CommandContext(ctx, "git", "-C", repoRoot, "fetch", "--quiet", "origin", refspec)
	} else {
		cmd = exec.CommandContext(ctx, "git", "-C", repoRoot, "fetch", "--quiet", "origin")
	}

	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(output))
		if len(outStr) > 500 {
			outStr = outStr[:500] + "..."
		}
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("git fetch %s timed out after %v", repoRoot, timeout)
		}
		return fmt.Errorf("git fetch %s: %w (output: %s)", repoRoot, err, outStr)
	}

	return nil
}
