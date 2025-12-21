package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// GetMergedBranches returns a set of local branches merged into target (default: main).
func GetMergedBranches(repoRoot, target string) (map[string]bool, error) {
	if target == "" {
		target = "main"
	}
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot("")
		if err != nil {
			return nil, err
		}
	}

	merged, err := getMergedBranches(repoRoot, target, true)
	if err != nil {
		return nil, err
	}

	return merged, nil
}

func getMergedBranches(repoRoot, target string, useFormat bool) (map[string]bool, error) {
	args := []string{"-C", repoRoot, "branch", "--merged", target}
	if useFormat {
		args = append(args, "--format=%(refname:short)")
	}

	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		if useFormat {
			return getMergedBranches(repoRoot, target, false)
		}
		return nil, fmt.Errorf("git branch --merged %s: %w", target, err)
	}

	merged := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "*") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		}
		merged[line] = true
	}

	return merged, nil
}

// CheckMergeConflict returns true if merging branch into target would conflict.
// It prefers git merge-tree --write-tree for speed, falling back to legacy merge-tree output parsing.
func CheckMergeConflict(repoRoot, branch, target string) (bool, error) {
	if branch == "" {
		return false, fmt.Errorf("branch is required")
	}
	if target == "" {
		target = "main"
	}
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot("")
		if err != nil {
			return false, err
		}
	}

	output, err := exec.Command(
		"git",
		"-C", repoRoot,
		"merge-tree",
		"--write-tree",
		"--messages",
		branch,
		target,
	).CombinedOutput()
	if err == nil {
		return false, nil
	}

	outStr := string(output)
	if hasMergeTreeConflict(outStr) {
		return true, nil
	}
	if shouldFallbackMergeTree(outStr) {
		return checkMergeConflictLegacy(repoRoot, branch, target)
	}

	return false, fmt.Errorf("git merge-tree %s %s: %w", branch, target, err)
}

func hasMergeTreeConflict(output string) bool {
	return strings.Contains(output, "CONFLICT") || strings.Contains(output, "<<<<<<<")
}

func shouldFallbackMergeTree(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "usage: git merge-tree") ||
		strings.Contains(lower, "unknown option") ||
		strings.Contains(lower, "unrecognized option")
}

func checkMergeConflictLegacy(repoRoot, branch, target string) (bool, error) {
	baseOutput, err := exec.Command(
		"git",
		"-C", repoRoot,
		"merge-base",
		branch,
		target,
	).Output()
	if err != nil {
		return false, fmt.Errorf("git merge-base %s %s: %w", branch, target, err)
	}

	base := strings.TrimSpace(string(baseOutput))
	if base == "" {
		return false, fmt.Errorf("git merge-base returned empty for %s and %s", branch, target)
	}

	output, err := exec.Command(
		"git",
		"-C", repoRoot,
		"merge-tree",
		base,
		branch,
		target,
	).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git merge-tree %s %s %s: %w", base, branch, target, err)
	}

	return hasMergeTreeConflict(string(output)), nil
}
