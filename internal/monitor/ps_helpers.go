package monitor

import (
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

func gitStatesForRuns(runs []*model.Run, target string) map[string]string {
	repoRoot, err := git.FindRepoRoot("")
	if err != nil {
		return nil
	}

	targetRef, merged, err := mergedBranchesForTarget(repoRoot, target)
	if err != nil {
		return nil
	}

	commitTimes, err := git.GetBranchCommitTimes(repoRoot)
	if err != nil {
		return nil
	}

	states := make(map[string]string)

	for _, r := range runs {
		if r == nil || r.Branch == "" {
			continue
		}

		isMerged := merged[r.Branch]

		commitTime, hasCommitTime := commitTimes[r.Branch]
		isNewWork := false
		if hasCommitTime && (r.StartedAt.IsZero() || !commitTime.Before(r.StartedAt)) {
			isNewWork = true
		}

		if isMerged {
			if isNewWork {
				states[r.RunID] = "merged"
			} else {
				states[r.RunID] = "no change"
			}
			continue
		}

		conflict, _ := git.CheckMergeConflict(repoRoot, r.Branch, targetRef)

		ahead, _ := git.GetAheadCount(repoRoot, r.Branch, targetRef)
		if ahead == 0 {
			states[r.RunID] = "no change"
			continue
		}

		if conflict {
			states[r.RunID] = "conflict"
		} else {
			states[r.RunID] = "clean"
		}
	}

	return states
}

func mergedBranchesForTarget(repoRoot, target string) (string, map[string]bool, error) {
	if target == "" {
		target = "main"
	}
	if strings.HasPrefix(target, "origin/") {
		merged, err := git.GetMergedBranches(repoRoot, target)
		if err == nil {
			return target, merged, nil
		}
		trimmed := strings.TrimPrefix(target, "origin/")
		merged, err = git.GetMergedBranches(repoRoot, trimmed)
		if err != nil {
			return "", nil, err
		}
		return trimmed, merged, nil
	}

	merged, err := git.GetMergedBranches(repoRoot, "origin/"+target)
	if err == nil {
		return "origin/" + target, merged, nil
	}

	merged, err = git.GetMergedBranches(repoRoot, target)
	if err != nil {
		return "", nil, err
	}
	return target, merged, nil
}
