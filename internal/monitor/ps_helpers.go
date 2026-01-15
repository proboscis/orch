package monitor

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
)

const (
	summaryMaxLen = 40
	topicMaxLen   = 30
	topicMaxWords = 5
)

func formatIssueTopic(issue *model.Issue) string {
	if issue == nil {
		return ""
	}

	topic := formatTopic(issue.Topic)
	if topic != "" {
		return topic
	}

	summary := strings.TrimSpace(issue.Summary)
	if summary == "" {
		return ""
	}
	return truncateWithEllipsis(summary, summaryMaxLen)
}

func formatTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return ""
	}

	words := strings.Fields(topic)
	if len(words) > topicMaxWords {
		topic = strings.Join(words[:topicMaxWords], " ") + "..."
	}

	if len(topic) > topicMaxLen {
		topic = truncateWithEllipsis(topic, topicMaxLen)
	}

	return topic
}

func truncateWithEllipsis(text string, max int) string {
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func formatBranchDisplay(branch string, max int) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "-"
	}
	if max <= 0 {
		return branch
	}
	return truncateWithEllipsis(branch, max)
}

func formatWorktreeDisplay(path string, max int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "-"
	}
	if max <= 0 {
		return path
	}
	path = abbreviateHome(path)
	short := shortenPath(path)
	return truncateLeading(short, max)
}

func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	homePrefix := home + string(os.PathSeparator)
	if strings.HasPrefix(path, homePrefix) {
		return "~" + path[len(home):]
	}
	return path
}

func shortenPath(path string) string {
	cleaned := filepath.Clean(path)
	sep := string(os.PathSeparator)
	parts := strings.Split(cleaned, sep)
	if len(parts) < 2 {
		return cleaned
	}
	suffix := filepath.Join(parts[len(parts)-2], parts[len(parts)-1])
	if suffix == cleaned {
		return cleaned
	}
	return "..." + sep + suffix
}

func truncateLeading(text string, max int) string {
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return "..." + text[len(text)-(max-3):]
}

func gitStatesForRuns(runs []*model.Run, target string) map[string]string {
	branchToRunID := make(map[string]string)
	runIDToWorktree := make(map[string]string)
	var branches []string
	var worktreePaths []string

	for _, r := range runs {
		if r == nil || r.Branch == "" {
			continue
		}
		branchToRunID[r.Branch] = r.RunID
		branches = append(branches, r.Branch)
		if r.WorktreePath != "" {
			runIDToWorktree[r.RunID] = r.WorktreePath
			worktreePaths = append(worktreePaths, r.WorktreePath)
		}
	}

	branchStates := git.GetBranchMergeStates("", target, branches)
	if branchStates == nil {
		branchStates = make(map[string]string)
	}

	dirtyWorktrees := git.GetWorktreeDirtyStates(worktreePaths)

	states := make(map[string]string)
	for branch, runID := range branchToRunID {
		if state, ok := branchStates[branch]; ok {
			states[runID] = state
		}
	}

	for runID, worktreePath := range runIDToWorktree {
		if dirtyWorktrees[worktreePath] {
			states[runID] = git.MergeStateUncommit
		}
	}

	return states
}
