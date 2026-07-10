package git

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// DiffStats represents git diff statistics for a worktree.
type DiffStats struct {
	Additions    int
	Deletions    int
	FilesChanged int
	Files        []string // List of changed file paths
}

// GetDiffStats calculates the diff stats for a worktree compared to its base branch.
// It returns the total additions and deletions across all files.
// Missing run metadata produces empty stats; filesystem and git failures are
// returned so callers cannot mistake unavailable evidence for a clean tree.
func GetDiffStats(worktreePath, branch, baseBranch string) (DiffStats, error) {
	if worktreePath == "" || branch == "" {
		return DiffStats{}, nil
	}

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); err != nil {
		return DiffStats{}, fmt.Errorf("get diff stats for worktree %q: stat failed: %w", worktreePath, err)
	}

	if baseBranch == "" {
		baseBranch = "main"
	}

	// Parse base branch to get remote/branch format
	remote, base := ParseRemoteBranch(baseBranch)
	remoteBranchRef := RemoteBranchRef(remote, base)

	// Try with remote ref first (three-dot syntax for merge-base comparison)
	stats, remoteErr := getDiffStatsInternal(worktreePath, remoteBranchRef, branch)
	if stats.Additions > 0 || stats.Deletions > 0 {
		return stats, nil
	}

	// Fallback: try with local base branch
	stats, localErr := getDiffStatsInternal(worktreePath, base, branch)
	if stats.Additions > 0 || stats.Deletions > 0 {
		return stats, nil
	}
	if remoteErr != nil && localErr != nil {
		return DiffStats{}, fmt.Errorf(
			"get diff stats for worktree %q: remote comparison failed: %v; local comparison failed: %v",
			worktreePath,
			remoteErr,
			localErr,
		)
	}

	// Final fallback: diff against HEAD (uncommitted changes)
	stats, err := getUncommittedDiffStats(worktreePath)
	if err != nil {
		return DiffStats{}, fmt.Errorf("get diff stats for worktree %q: %w", worktreePath, err)
	}
	return stats, nil
}

// getDiffStatsInternal runs git diff --numstat and parses the output.
func getDiffStatsInternal(worktreePath, from, to string) (DiffStats, error) {
	// Use three-dot syntax for merge-base comparison
	diffRange := from + "..." + to
	cmd := exec.Command("git", "-C", worktreePath, "diff", "--numstat", diffRange)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Try two-dot syntax as fallback
		threeDotErr := commandError(err, output)
		diffRange = from + ".." + to
		cmd = exec.Command("git", "-C", worktreePath, "diff", "--numstat", diffRange)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return DiffStats{}, fmt.Errorf(
				"git diff %q failed with three-dot (%v) and two-dot (%v)",
				from+" to "+to,
				threeDotErr,
				commandError(err, output),
			)
		}
	}

	return parseDiffNumstat(string(output)), nil
}

// getUncommittedDiffStats gets stats for uncommitted changes in the worktree.
func getUncommittedDiffStats(worktreePath string) (DiffStats, error) {
	// Get both staged and unstaged changes
	cmd := exec.Command("git", "-C", worktreePath, "diff", "--numstat", "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return DiffStats{}, fmt.Errorf("git diff against HEAD failed: %v", commandError(err, output))
	}

	return parseDiffNumstat(string(output)), nil
}

func commandError(err error, output []byte) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return err
	}
	if newline := strings.IndexByte(detail, '\n'); newline >= 0 {
		detail = detail[:newline]
	}
	return fmt.Errorf("%w: %s", err, detail)
}

func parseDiffNumstat(output string) DiffStats {
	var stats DiffStats

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		fileName := parts[2]
		stats.Files = append(stats.Files, fileName)
		stats.FilesChanged++

		if parts[0] != "-" {
			if add, err := strconv.Atoi(parts[0]); err == nil {
				stats.Additions += add
			}
		}
		if parts[1] != "-" {
			if del, err := strconv.Atoi(parts[1]); err == nil {
				stats.Deletions += del
			}
		}
	}

	return stats
}

// GetDiffStatsForRuns calculates diff stats for multiple worktrees in batch.
// Returns a map of worktree path to diff stats.
func GetDiffStatsForRuns(runs []struct {
	WorktreePath string
	Branch       string
	BaseBranch   string
}) (map[string]DiffStats, error) {
	results := make(map[string]DiffStats)

	for _, run := range runs {
		if run.WorktreePath == "" {
			continue
		}
		stats, err := GetDiffStats(run.WorktreePath, run.Branch, run.BaseBranch)
		if err != nil {
			return nil, err
		}
		results[run.WorktreePath] = stats
	}

	return results, nil
}
