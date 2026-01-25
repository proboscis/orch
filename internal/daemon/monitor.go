package daemon

import (
	"crypto/md5"
	"encoding/hex"
	"regexp"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/pr"
	"github.com/s22625/orch/internal/store"
)

const deadChecksBeforeFailed = 3

func (d *Daemon) monitorRun(run *model.Run, st store.Store) error {
	if run.Status == model.StatusCanceled {
		return nil
	}

	state := d.getOrCreateState(run)
	state.LastCheckAt = time.Now()

	// orch-358: check for merged PR before dead checks
	if run.Branch != "" {
		if merged, prURL := d.checkPRMergedWithURL(run, st); merged {
			if prURL != "" {
				d.logger.Printf("%s#%s: detected merged PR (%s), transitioning to done", run.IssueID, run.RunID, prURL)
			} else {
				d.logger.Printf("%s#%s: detected merged PR, transitioning to done", run.IssueID, run.RunID)
			}
			return d.updateStatus(run, model.StatusDone, st)
		}
	}

	mgr := agent.GetManager(run)

	if mgr.IsAlive(run) {
		state.WasAlive = true
		state.DeadCheckCount = 0
	} else {
		state.DeadCheckCount++
		if !state.WasAlive {
			// Agent was never confirmed alive. For opencode runs, check if there are
			// clear completion signals (merged PR, clean worktree) before giving up.
			// This handles cases where the daemon started after the agent finished.
			if run.Agent == "opencode" && state.DeadCheckCount >= deadChecksBeforeFailed {
				inferredStatus := d.inferStatusFromGitState(run, st)
				if inferredStatus != "" {
					d.logger.Printf("%s#%s: agent never confirmed alive, but inferred status from git state: %s", run.IssueID, run.RunID, inferredStatus)
					return d.updateStatus(run, inferredStatus, st)
				}
			}
			d.logger.Printf("%s#%s: agent not alive yet (never confirmed alive), waiting", run.IssueID, run.RunID)
			return nil
		}
		if state.DeadCheckCount < deadChecksBeforeFailed {
			d.logger.Printf("%s#%s: agent not alive (%d/%d checks), waiting", run.IssueID, run.RunID, state.DeadCheckCount, deadChecksBeforeFailed)
			return nil
		}
		if run.Agent == "opencode" {
			inferredStatus := d.inferStatusFromGitState(run, st)
			if inferredStatus != "" {
				d.logger.Printf("%s#%s: opencode session gone, inferred status from git state: %s", run.IssueID, run.RunID, inferredStatus)
				return d.updateStatus(run, inferredStatus, st)
			}
			d.logger.Printf("%s#%s: opencode session not found after %d checks, marking unknown", run.IssueID, run.RunID, state.DeadCheckCount)
			return d.updateStatus(run, model.StatusUnknown, st)
		}
		d.logger.Printf("%s#%s: agent confirmed dead after %d checks, marking failed", run.IssueID, run.RunID, state.DeadCheckCount)
		return d.updateStatus(run, model.StatusFailed, st)
	}

	output, err := mgr.CaptureOutput(run)
	if err != nil {
		d.logger.Printf("%s#%s: failed to capture output: %v", run.IssueID, run.RunID, err)
		return nil
	}

	contentHash := hashContent(output)
	outputChanged := contentHash != state.OutputHash
	hasPrompt := mgr.DetectPrompt(output)

	if outputChanged {
		state.OutputHash = contentHash
		state.LastOutput = output
		state.LastOutputAt = time.Now()
	}

	if prURL := d.detectPRCreation(output); prURL != "" {
		if !state.PRRecorded {
			d.logger.Printf("%s#%s: PR created: %s", run.IssueID, run.RunID, prURL)
			if err := d.recordPRArtifact(run, prURL, st); err != nil {
				d.logger.Printf("%s#%s: failed to record PR artifact: %v", run.IssueID, run.RunID, err)
			} else {
				state.PRRecorded = true
				if err := d.updateStatus(run, model.StatusPROpen, st); err != nil {
					d.logger.Printf("%s#%s: failed to update status to pr_open: %v", run.IssueID, run.RunID, err)
				}
				return nil
			}
		}
	}

	agentState := &agent.RunState{
		LastOutput:   state.LastOutput,
		LastOutputAt: state.LastOutputAt,
		LastCheckAt:  state.LastCheckAt,
		OutputHash:   state.OutputHash,
		PRRecorded:   state.PRRecorded,
	}
	newStatus := mgr.GetStatus(run, output, agentState, outputChanged, hasPrompt)

	if newStatus != "" && newStatus != run.Status {
		d.logger.Printf("%s#%s: status change %s -> %s", run.IssueID, run.RunID, run.Status, newStatus)
		if err := d.updateStatus(run, newStatus, st); err != nil {
			return err
		}
		d.notifyStatusChange(run, newStatus, output, st)
	}

	return nil
}

func (d *Daemon) updateStatus(run *model.Run, status model.Status, st store.Store) error {
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}

	// Check current status - daemon cannot overwrite terminal states
	if currentRun, err := st.GetRun(ref); err == nil && currentRun != nil {
		if !model.CanTransitionStatus(currentRun.Status, status, model.EventSourceDaemon) {
			d.debug("%s#%s: daemon cannot transition from %s to %s", run.IssueID, run.RunID, currentRun.Status, status)
			return nil
		}
	}

	event := model.NewStatusEvent(status)
	if err := st.AppendEvent(ref, event); err != nil {
		return err
	}

	// Auto-resolve issue when run is done
	if status == model.StatusDone {
		// Check current issue status to avoid overwriting closed issues
		issue, err := st.ResolveIssue(run.IssueID)
		if err != nil {
			d.logger.Printf("%s#%s: failed to resolve issue for auto-resolve: %v", run.IssueID, run.RunID, err)
		} else if issue.Status == model.IssueStatusClosed {
			d.debug("%s#%s: skipping auto-resolve, issue already closed", run.IssueID, run.RunID)
		} else if issue.Status == model.IssueStatusResolved {
			d.debug("%s#%s: skipping auto-resolve, issue already resolved", run.IssueID, run.RunID)
		} else {
			if err := st.SetIssueStatus(run.IssueID, model.IssueStatusResolved); err != nil {
				d.logger.Printf("%s#%s: failed to auto-resolve issue: %v", run.IssueID, run.RunID, err)
			} else {
				d.debug("%s#%s: auto-resolved issue", run.IssueID, run.RunID)
			}
		}
	}

	return nil
}

// hashString returns a simple hash of a string
func hashString(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// hashContent hashes the main content area, excluding the status bar
// This prevents token counter updates from causing false "output changed" signals
func hashContent(output string) string {
	lines := strings.Split(output, "\n")
	// Skip the last 5 lines (status bar area: tokens, shortcuts, prompts)
	if len(lines) > 5 {
		lines = lines[:len(lines)-5]
	}
	return hashString(strings.Join(lines, "\n"))
}

// prURLRegex matches GitHub/GitLab PR URLs
var prURLRegex = regexp.MustCompile(`https://(?:github\.com|gitlab\.com)/[^\s]+/pull/\d+|https://(?:github\.com|gitlab\.com)/[^\s]+/merge_requests/\d+`)

// detectPRCreation scans output for PR creation URLs
// Returns the first PR URL found, or empty string if none
func (d *Daemon) detectPRCreation(output string) string {
	// Look for GitHub/GitLab PR URLs in the output
	match := prURLRegex.FindString(output)
	if match != "" {
		return match
	}
	return ""
}

func (d *Daemon) recordPRArtifact(run *model.Run, prURL string, st store.Store) error {
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	event := model.NewArtifactEvent("pr", map[string]string{
		"url": prURL,
	})
	return st.AppendEvent(ref, event)
}

func (d *Daemon) checkPRMerged(run *model.Run) bool {
	merged, _ := d.checkPRMergedWithURL(run, nil)
	return merged
}

func (d *Daemon) checkPRMergedWithURL(run *model.Run, st store.Store) (merged bool, prURL string) {
	if run.PRUrl != "" {
		prInfo, err := pr.LookupInfoByURL(run.PRUrl)
		if err == nil && prInfo != nil && prInfo.State == "MERGED" {
			return true, run.PRUrl
		}
	}

	if run.Branch == "" {
		return false, ""
	}

	var repoRoot string
	var err error
	if run.WorktreePath != "" {
		repoRoot, err = git.FindMainRepoRoot(run.WorktreePath)
	}
	if repoRoot == "" || err != nil {
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return false, ""
		}
	}

	prInfo, err := pr.LookupInfo(repoRoot, run.Branch)
	if err != nil || prInfo == nil {
		return false, ""
	}

	if prInfo.URL != "" && run.PRUrl == "" && st != nil {
		if err := d.recordPRArtifact(run, prInfo.URL, st); err != nil {
			d.logger.Printf("%s#%s: failed to record discovered PR: %v", run.IssueID, run.RunID, err)
		}
	}

	if prInfo.State == "MERGED" {
		return true, prInfo.URL
	}
	return false, ""
}

func (d *Daemon) notifyStatusChange(run *model.Run, newStatus model.Status, lastOutput string, st store.Store) {
	if d.slackNotifier == nil || d.config == nil {
		return
	}

	if !d.config.Slack.ShouldNotify(string(newStatus)) {
		return
	}

	issueTitle := run.IssueID
	if st != nil {
		if issue, err := st.ResolveIssue(run.IssueID); err == nil && issue != nil {
			if issue.Title != "" {
				issueTitle = issue.Title
			}
		}
	}

	var err error
	if newStatus == model.StatusBlocked || newStatus == model.StatusBlockedAPI {
		err = d.slackNotifier.NotifyBlocked(run, issueTitle, lastOutput)
	} else {
		err = d.slackNotifier.NotifyStatusChange(run, issueTitle, newStatus)
	}

	if err != nil {
		d.logger.Printf("%s#%s: failed to send slack notification: %v", run.IssueID, run.RunID, err)
	} else {
		d.logger.Printf("%s#%s: sent slack notification for status %s", run.IssueID, run.RunID, newStatus)
	}
}

func (d *Daemon) inferStatusFromGitState(run *model.Run, st store.Store) model.Status {
	if run.Branch == "" || run.WorktreePath == "" {
		d.debug("%s#%s: infer: skipping - branch=%q worktree=%q", run.IssueID, run.RunID, run.Branch, run.WorktreePath)
		return ""
	}

	repoRoot, err := git.FindMainRepoRoot(run.WorktreePath)
	if err != nil {
		d.logger.Printf("%s#%s: infer: cannot find repo root: %v", run.IssueID, run.RunID, err)
		return ""
	}

	d.debug("%s#%s: infer: checking PR for branch %s", run.IssueID, run.RunID, run.Branch)
	prInfo, err := pr.LookupInfo(repoRoot, run.Branch)
	if err == nil && prInfo != nil && prInfo.URL != "" {
		d.logger.Printf("%s#%s: infer: found PR %s (state=%s)", run.IssueID, run.RunID, prInfo.URL, prInfo.State)
		if run.PRUrl == "" {
			if err := d.recordPRArtifact(run, prInfo.URL, st); err != nil {
				d.logger.Printf("%s#%s: infer: failed to record PR: %v", run.IssueID, run.RunID, err)
			}
		}
		if prInfo.State == "MERGED" {
			return model.StatusDone
		}
		return model.StatusPROpen
	}
	if err != nil {
		d.debug("%s#%s: infer: PR lookup error: %v", run.IssueID, run.RunID, err)
	} else {
		d.debug("%s#%s: infer: no PR found", run.IssueID, run.RunID)
	}

	// If branch-based lookup failed but run already has a PR URL, check PR status by URL
	// This handles cases where the local branch was deleted/rebased but PR still exists
	if run.PRUrl != "" {
		d.debug("%s#%s: infer: branch lookup failed, checking existing PR URL: %s", run.IssueID, run.RunID, run.PRUrl)
		prInfo, err := pr.LookupInfoByURL(run.PRUrl)
		if err == nil && prInfo != nil {
			d.logger.Printf("%s#%s: infer: PR %s state=%s (via URL lookup)", run.IssueID, run.RunID, prInfo.URL, prInfo.State)
			if prInfo.State == "MERGED" {
				return model.StatusDone
			}
			return model.StatusPROpen
		}
		if err != nil {
			d.debug("%s#%s: infer: PR URL lookup error: %v", run.IssueID, run.RunID, err)
		}
		// If URL lookup also fails, preserve PR_OPEN status since we know a PR exists
		d.debug("%s#%s: infer: preserving pr_open status (PR URL exists but lookup failed)", run.IssueID, run.RunID)
		return model.StatusPROpen
	}

	baseBranch := "origin/main"
	if d.config != nil && d.config.BaseBranch != "" {
		remote, branch := git.ParseRemoteBranch(d.config.BaseBranch)
		baseBranch = git.RemoteBranchRef(remote, branch)
	}

	aheadCount, err := git.GetAheadCount(repoRoot, run.Branch, baseBranch)
	if err != nil {
		d.logger.Printf("%s#%s: infer: cannot get ahead count: %v", run.IssueID, run.RunID, err)
		return ""
	}

	hasUncommitted := git.HasUncommittedChanges(run.WorktreePath)

	d.logger.Printf("%s#%s: infer: commits ahead=%d, uncommitted=%v", run.IssueID, run.RunID, aheadCount, hasUncommitted)

	if aheadCount > 0 || hasUncommitted {
		return model.StatusBlocked
	}

	return model.StatusDone
}
