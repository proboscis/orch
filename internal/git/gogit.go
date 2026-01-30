package git

import (
	"os"
	"os/exec"
	"sync"
)

type BranchState int

const (
	BranchStateUnspecified BranchState = iota
	BranchStateDirty
	BranchStateMerged
	BranchStateClean
)

type WorktreeStatus struct {
	State     BranchState
	DiffStats DiffStats
}

func GetWorktreeStatusBatch(worktrees []struct {
	Path       string
	Branch     string
	BaseBranch string
}) map[string]WorktreeStatus {
	results := make(map[string]WorktreeStatus)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, wt := range worktrees {
		if wt.Path == "" {
			continue
		}
		wg.Add(1)
		go func(path, branch, baseBranch string) {
			defer wg.Done()
			info := GetWorktreeStatus(path, branch, baseBranch)
			mu.Lock()
			results[path] = info
			mu.Unlock()
		}(wt.Path, wt.Branch, wt.BaseBranch)
	}

	wg.Wait()
	return results
}

func GetWorktreeStatus(worktreePath, branch, baseBranch string) WorktreeStatus {
	if worktreePath == "" {
		return WorktreeStatus{State: BranchStateUnspecified}
	}

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return WorktreeStatus{State: BranchStateUnspecified}
	}

	status := WorktreeStatus{State: BranchStateClean}

	if isDirtyFast(worktreePath) {
		status.State = BranchStateDirty
		status.DiffStats = getDiffStatsFast(worktreePath, baseBranch)
		return status
	}

	if isMergedFast(worktreePath, branch, baseBranch) {
		status.State = BranchStateMerged
		return status
	}

	status.DiffStats = getDiffStatsFast(worktreePath, baseBranch)
	return status
}

func isMergedFast(worktreePath, branch, baseBranch string) bool {
	if baseBranch == "" {
		baseBranch = "main"
	}
	cmd := exec.Command("git", "-C", worktreePath, "branch", "--merged", "origin/"+baseBranch, "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("git", "-C", worktreePath, "branch", "--merged", baseBranch, "--format=%(refname:short)")
		output, err = cmd.Output()
		if err != nil {
			return false
		}
	}
	for _, line := range splitLinesBytes(output) {
		if line == branch {
			return true
		}
	}
	return false
}

func getDiffStatsFast(worktreePath, baseBranch string) DiffStats {
	if baseBranch == "" {
		baseBranch = "main"
	}
	cmd := exec.Command("git", "-C", worktreePath, "diff", "--numstat", "origin/"+baseBranch+"...HEAD")
	output, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("git", "-C", worktreePath, "diff", "--numstat", baseBranch+"...HEAD")
		output, err = cmd.Output()
		if err != nil {
			return DiffStats{}
		}
	}
	return parseDiffNumstat(string(output))
}

func splitLinesBytes(b []byte) []string {
	var lines []string
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			if i > start {
				lines = append(lines, string(b[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(b) {
		lines = append(lines, string(b[start:]))
	}
	return lines
}

func isDirtyFast(worktreePath string) bool {
	cmd := exec.Command("git", "-C", worktreePath, "diff-index", "--quiet", "HEAD", "--")
	err := cmd.Run()
	return err != nil
}
