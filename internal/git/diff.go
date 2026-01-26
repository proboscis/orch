package git

import (
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
// If the calculation fails, it returns zero stats (non-fatal).
func GetDiffStats(worktreePath, branch, baseBranch string) DiffStats {
	if worktreePath == "" || branch == "" {
		return DiffStats{}
	}

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return DiffStats{}
	}

	if baseBranch == "" {
		baseBranch = "main"
	}

	// Parse base branch to get remote/branch format
	remote, base := ParseRemoteBranch(baseBranch)
	remoteBranchRef := RemoteBranchRef(remote, base)

	// Try with remote ref first (three-dot syntax for merge-base comparison)
	stats := getDiffStatsInternal(worktreePath, remoteBranchRef, branch)
	if stats.Additions > 0 || stats.Deletions > 0 {
		return stats
	}

	// Fallback: try with local base branch
	stats = getDiffStatsInternal(worktreePath, base, branch)
	if stats.Additions > 0 || stats.Deletions > 0 {
		return stats
	}

	// Final fallback: diff against HEAD (uncommitted changes)
	return getUncommittedDiffStats(worktreePath)
}

// getDiffStatsInternal runs git diff --numstat and parses the output.
func getDiffStatsInternal(worktreePath, from, to string) DiffStats {
	// Use three-dot syntax for merge-base comparison
	diffRange := from + "..." + to
	cmd := exec.Command("git", "-C", worktreePath, "diff", "--numstat", diffRange)
	output, err := cmd.Output()
	if err != nil {
		// Try two-dot syntax as fallback
		diffRange = from + ".." + to
		cmd = exec.Command("git", "-C", worktreePath, "diff", "--numstat", diffRange)
		output, err = cmd.Output()
		if err != nil {
			return DiffStats{}
		}
	}

	return parseDiffNumstat(string(output))
}

// getUncommittedDiffStats gets stats for uncommitted changes in the worktree.
func getUncommittedDiffStats(worktreePath string) DiffStats {
	// Get both staged and unstaged changes
	cmd := exec.Command("git", "-C", worktreePath, "diff", "--numstat", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return DiffStats{}
	}

	return parseDiffNumstat(string(output))
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
}) map[string]DiffStats {
	results := make(map[string]DiffStats)

	for _, run := range runs {
		if run.WorktreePath == "" {
			continue
		}
		results[run.WorktreePath] = GetDiffStats(run.WorktreePath, run.Branch, run.BaseBranch)
	}

	return results
}
