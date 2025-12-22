package git

import (
	"fmt"
	"os/exec"
	"strings"
)

var defaultBranchCandidates = []string{"main", "master", "trunk", "develop"}

// MergedBranchesForTarget resolves a usable merge target and returns merged branches for it.
func MergedBranchesForTarget(repoRoot, target string) (string, map[string]bool, error) {
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot("")
		if err != nil {
			return "", nil, err
		}
	}

	candidates := mergeTargetCandidates(target)
	var lastErr error
	for _, candidate := range candidates {
		if !revExists(repoRoot, candidate) {
			continue
		}
		merged, err := GetMergedBranches(repoRoot, candidate)
		if err == nil {
			return candidate, merged, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no valid merge target found")
	}
	return "", nil, lastErr
}

func mergeTargetCandidates(target string) []string {
	seen := make(map[string]bool)
	var candidates []string

	add := func(ref string) {
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		candidates = append(candidates, ref)
	}

	if target != "" {
		if strings.HasPrefix(target, "origin/") {
			add(target)
			if target != "origin/HEAD" {
				add(strings.TrimPrefix(target, "origin/"))
			}
		} else {
			add("origin/" + target)
			add(target)
		}
	}

	add("origin/HEAD")

	for _, name := range defaultBranchCandidates {
		add("origin/" + name)
		add(name)
	}

	return candidates
}

func revExists(repoRoot, rev string) bool {
	if rev == "" {
		return false
	}
	cmd := exec.Command(
		"git",
		"-C", repoRoot,
		"rev-parse",
		"--verify",
		"--quiet",
		rev+"^{commit}",
	)
	return cmd.Run() == nil
}
