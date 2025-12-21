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
