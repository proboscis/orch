package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GetBranchCommitTimes returns the tip committer times for local branches.
func GetBranchCommitTimes(repoRoot string) (map[string]time.Time, error) {
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot("")
		if err != nil {
			return nil, err
		}
	}

	cmd := exec.Command(
		"git",
		"-C", repoRoot,
		"for-each-ref",
		"--format=%(refname:short) %(committerdate:unix)",
		"refs/heads",
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref refs/heads: %w", err)
	}

	commitTimes := make(map[string]time.Time)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("unexpected branch ref line: %q", line)
		}
		unixSeconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse commit time for %s: %w", fields[0], err)
		}
		commitTimes[fields[0]] = time.Unix(unixSeconds, 0)
	}

	return commitTimes, nil
}

// GetAheadCount returns the number of commits the branch is ahead of the target.
// equivalent to: git rev-list --count target..branch
func GetAheadCount(repoRoot, branch, target string) (int, error) {
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot("")
		if err != nil {
			return 0, err
		}
	}

	// Use -- to separate paths from revisions to handle branch names that look like flags/paths
	cmd := exec.Command(
		"git",
		"-C", repoRoot,
		"rev-list",
		"--count",
		fmt.Sprintf("%s..%s", target, branch),
		"--",
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("git rev-list count: %w", err)
	}

	countStr := strings.TrimSpace(string(output))
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0, fmt.Errorf("invalid commit count %q: %w", countStr, err)
	}

	return count, nil
}