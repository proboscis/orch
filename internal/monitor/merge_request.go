package monitor

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/pr"
)

var prURLRegex = regexp.MustCompile(`https://(?:github\.com|gitlab\.com)/[^\s]+/(?:pull|merge_requests)/\d+`)

// RequestMerge creates or references a PR to merge the run's branch into the base branch.
func (m *Monitor) RequestMerge(run *model.Run) (string, error) {
	if run == nil {
		return "", fmt.Errorf("run not found")
	}
	if run.WorktreePath == "" {
		return "", fmt.Errorf("run has no worktree")
	}
	if _, err := os.Stat(run.WorktreePath); err != nil {
		return "", fmt.Errorf("worktree does not exist: %s", run.WorktreePath)
	}

	branch := strings.TrimSpace(run.Branch)
	if branch == "" {
		currentBranch, err := git.GetCurrentBranch(run.WorktreePath)
		if err != nil {
			return "", fmt.Errorf("failed to determine branch: %w", err)
		}
		branch = strings.TrimSpace(currentBranch)
	}
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("run has no branch to merge")
	}

	base := "main"
	if cfg, err := config.Load(); err == nil && cfg.BaseBranch != "" {
		base = cfg.BaseBranch
	}
	if branch == base {
		return fmt.Sprintf("branch %s already matches base %s", branch, base), nil
	}

	repoRoot, err := git.FindMainRepoRoot(run.WorktreePath)
	if err != nil {
		return "", fmt.Errorf("failed to locate repo root: %w", err)
	}

	if run.PRUrl != "" {
		return fmt.Sprintf("PR already recorded: %s", run.PRUrl), nil
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not available; cannot request merge")
	}

	if info, err := pr.LookupInfo(repoRoot, branch); err == nil && info != nil && info.URL != "" {
		if err := m.recordPRStatus(run, info.URL, info.State); err != nil {
			return "", err
		}
		return fmt.Sprintf("PR already exists: %s", info.URL), nil
	}

	if ahead, err := git.GetAheadCount(repoRoot, branch, base); err == nil && ahead == 0 {
		return fmt.Sprintf("no changes to merge from %s into %s", branch, base), nil
	}

	title, body := m.buildPRMetadata(run)
	output, err := m.createPR(run.WorktreePath, base, branch, title, body)
	if err != nil {
		return output, err
	}

	if info, err := pr.LookupInfo(repoRoot, branch); err == nil && info != nil && info.URL != "" {
		if err := m.recordPRStatus(run, info.URL, info.State); err != nil {
			return "", err
		}
		return fmt.Sprintf("PR created: %s", info.URL), nil
	}

	if url := extractPRURL(output); url != "" {
		if err := m.recordPRStatus(run, url, "OPEN"); err != nil {
			return "", err
		}
		return fmt.Sprintf("PR created: %s", url), nil
	}

	if strings.TrimSpace(output) == "" {
		output = "merge requested"
	}
	return output, nil
}

func (m *Monitor) buildPRMetadata(run *model.Run) (string, string) {
	issueID := strings.TrimSpace(run.IssueID)
	title := "Request merge"
	bodyLines := []string{"Requested via orch monitor."}

	if issueID != "" {
		title = issueID
		bodyLines = []string{fmt.Sprintf("Issue: %s", issueID)}
	}

	if issueID != "" {
		if issue, err := m.store.ResolveIssue(issueID); err == nil && issue != nil {
			summary := strings.TrimSpace(issue.Summary)
			if summary == "" {
				summary = strings.TrimSpace(issue.Title)
			}
			if summary != "" {
				title = fmt.Sprintf("%s: %s", issueID, summary)
				bodyLines = append(bodyLines, "", fmt.Sprintf("Summary: %s", summary))
			}
		}
	}

	return title, strings.Join(bodyLines, "\n")
}

func (m *Monitor) createPR(worktreePath, base, branch, title, body string) (string, error) {
	args := []string{
		"pr", "create",
		"--base", base,
		"--head", branch,
		"--title", title,
		"--body", body,
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = worktreePath

	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(output))
	if err != nil {
		if outStr == "" {
			outStr = err.Error()
		}
		return outStr, err
	}
	return outStr, nil
}

func (m *Monitor) recordPRStatus(run *model.Run, url, state string) error {
	if run == nil {
		return fmt.Errorf("run not found")
	}
	if url != "" && run.PRUrl != url {
		if err := m.store.AppendEvent(run.Ref(), model.NewArtifactEvent("pr", map[string]string{"url": url})); err != nil {
			return fmt.Errorf("failed to record PR artifact: %w", err)
		}
	}
	if strings.EqualFold(state, "OPEN") && run.Status != model.StatusPROpen {
		if err := m.store.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusPROpen)); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	}
	return nil
}

func extractPRURL(output string) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}
	return prURLRegex.FindString(output)
}
