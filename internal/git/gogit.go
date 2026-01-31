package git

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type BranchState int

const (
	BranchStateUnspecified BranchState = iota
	BranchStateDirty
	BranchStateMerged
	BranchStateClean
	BranchStateAhead
	BranchStateBehind
	BranchStateDiverged
	BranchStateConflict
	BranchStateSynced
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

	if isDirtyFast(worktreePath) {
		return WorktreeStatus{
			State:     BranchStateDirty,
			DiffStats: getDiffStatsFast(worktreePath, baseBranch),
		}
	}

	if isMergedFast(worktreePath, branch, baseBranch) {
		return WorktreeStatus{State: BranchStateMerged}
	}

	ahead, behind := getAheadBehind(worktreePath, baseBranch)

	if ahead == 0 && behind == 0 {
		return WorktreeStatus{State: BranchStateSynced}
	}

	if ahead == 0 && behind > 0 {
		return WorktreeStatus{State: BranchStateBehind}
	}

	diffStats := getDiffStatsFast(worktreePath, baseBranch)

	if ahead > 0 && behind > 0 {
		if hasConflicts(worktreePath, baseBranch) {
			return WorktreeStatus{State: BranchStateConflict, DiffStats: diffStats}
		}
		return WorktreeStatus{State: BranchStateDiverged, DiffStats: diffStats}
	}

	if hasConflicts(worktreePath, baseBranch) {
		return WorktreeStatus{State: BranchStateConflict, DiffStats: diffStats}
	}

	return WorktreeStatus{State: BranchStateAhead, DiffStats: diffStats}
}

func isMergedFast(worktreePath, branch, baseBranch string) bool {
	if baseBranch == "" {
		baseBranch = "main"
	}

	targetRef := "origin/" + baseBranch
	cmd := exec.Command("git", "-C", worktreePath, "branch", "--merged", targetRef, "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		targetRef = baseBranch
		cmd = exec.Command("git", "-C", worktreePath, "branch", "--merged", targetRef, "--format=%(refname:short)")
		output, err = cmd.Output()
		if err != nil {
			return false
		}
	}

	inMergedList := false
	for _, line := range splitLinesBytes(output) {
		if line == branch {
			inMergedList = true
			break
		}
	}
	if !inMergedList {
		return false
	}

	branchHead := GetBranchHead(worktreePath, branch)
	targetHead := GetBranchHead(worktreePath, targetRef)
	if branchHead != "" && branchHead == targetHead {
		return false
	}
	return true
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

func getAheadBehind(worktreePath, baseBranch string) (ahead, behind int) {
	if baseBranch == "" {
		baseBranch = "main"
	}

	target := "origin/" + baseBranch
	cmd := exec.Command("git", "-C", worktreePath, "rev-list", "--left-right", "--count", target+"...HEAD")
	output, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("git", "-C", worktreePath, "rev-list", "--left-right", "--count", baseBranch+"...HEAD")
		output, err = cmd.Output()
		if err != nil {
			return 0, 0
		}
	}

	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) != 2 {
		return 0, 0
	}

	behind, _ = strconv.Atoi(parts[0])
	ahead, _ = strconv.Atoi(parts[1])
	return ahead, behind
}

func hasConflicts(worktreePath, baseBranch string) bool {
	if baseBranch == "" {
		baseBranch = "main"
	}

	target := "origin/" + baseBranch
	targetCommit := getCommitHash(worktreePath, target)
	if targetCommit == "" {
		targetCommit = getCommitHash(worktreePath, baseBranch)
	}
	if targetCommit == "" {
		return false
	}

	headCommit := getCommitHash(worktreePath, "HEAD")
	if headCommit == "" {
		return false
	}

	baseCommit := getMergeBase(worktreePath, targetCommit, headCommit)
	if baseCommit == "" {
		return false
	}

	cmd := exec.Command("git", "-C", worktreePath, "merge-tree", baseCommit, targetCommit, headCommit)
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.Contains(string(output), "<<<<<<")
}

func getCommitHash(worktreePath, ref string) string {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", ref)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func getMergeBase(worktreePath, commit1, commit2 string) string {
	cmd := exec.Command("git", "-C", worktreePath, "merge-base", commit1, commit2)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
