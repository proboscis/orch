package daemon

import (
	"crypto/md5"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/pr"
	"github.com/s22625/orch/internal/store"
)

const deadChecksBeforeFailed = 3

const (
	captureErrorLogInterval        = 60 * time.Second
	captureBackoffInitial          = 5 * time.Second
	captureBackoffMax              = 60 * time.Second
	captureRefusedBackoffInitial   = 10 * time.Second
	captureRefusedBackoffMax       = 5 * time.Minute
	captureRefusedNegativeCacheTTL = 30 * time.Second
	waitingPromptStreakThreshold   = 2
)

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
		opencodeLogPath := ""
		if run.Agent == "opencode" {
			opencodeLogPath = opencodeServerLogPath(run.WorktreePath)
			if state.DeadCheckCount == 1 && opencodeLogPath != "" {
				d.logger.Printf("%s#%s: opencode bootstrap logs: %s", run.IssueID, run.RunID, opencodeLogPath)
			}
		}
		if !state.WasAlive {
			// Agent was never confirmed alive. For opencode runs, check if there are
			// clear completion signals (merged PR, clean worktree) before giving up.
			// This handles cases where the daemon started after the agent finished.
			if run.Agent == "opencode" && state.DeadCheckCount >= deadChecksBeforeFailed {
				inferredStatus := d.inferStatusFromGitState(run, st, false)
				if inferredStatus != "" {
					if opencodeLogPath != "" {
						d.logger.Printf("%s#%s: agent never confirmed alive, but inferred status from git state: %s (opencode logs: %s)", run.IssueID, run.RunID, inferredStatus, opencodeLogPath)
					} else {
						d.logger.Printf("%s#%s: agent never confirmed alive, but inferred status from git state: %s", run.IssueID, run.RunID, inferredStatus)
					}
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
			inferredStatus := d.inferStatusFromGitState(run, st, true)
			if inferredStatus != "" {
				if opencodeLogPath != "" {
					d.logger.Printf("%s#%s: opencode session gone, inferred status from git state: %s (opencode logs: %s)", run.IssueID, run.RunID, inferredStatus, opencodeLogPath)
				} else {
					d.logger.Printf("%s#%s: opencode session gone, inferred status from git state: %s", run.IssueID, run.RunID, inferredStatus)
				}
				return d.updateStatus(run, inferredStatus, st)
			}
			if opencodeLogPath != "" {
				d.logger.Printf("%s#%s: opencode session not found after %d checks, marking unknown (opencode logs: %s)", run.IssueID, run.RunID, state.DeadCheckCount, opencodeLogPath)
			} else {
				d.logger.Printf("%s#%s: opencode session not found after %d checks, marking unknown", run.IssueID, run.RunID, state.DeadCheckCount)
			}
			return d.updateStatus(run, model.StatusUnknown, st)
		}
		d.logger.Printf("%s#%s: agent confirmed dead after %d checks, marking failed", run.IssueID, run.RunID, state.DeadCheckCount)
		return d.updateStatus(run, model.StatusFailed, st)
	}

	now := time.Now()
	captureEndpoint := captureEndpointKey(run, mgr)
	if state.shouldSkipCapture(captureEndpoint, now) {
		return nil
	}

	output, err := mgr.CaptureOutput(run)
	if err != nil {
		retryAt, shouldLog, suppressed := state.recordCaptureFailure(captureEndpoint, err, now)
		if shouldLog {
			retryIn := retryAt.Sub(now).Round(time.Second)
			if retryIn < time.Second {
				retryIn = time.Second
			}
			if suppressed > 0 {
				d.logger.Printf("%s#%s: failed to capture output from %s: %v (next retry in %s, suppressed %d similar errors)", run.IssueID, run.RunID, captureEndpoint, err, retryIn, suppressed)
			} else {
				d.logger.Printf("%s#%s: failed to capture output from %s: %v (next retry in %s)", run.IssueID, run.RunID, captureEndpoint, err, retryIn)
			}
		}
		return nil
	}
	state.resetCaptureFailure()

	contentHash := hashContent(output)
	outputChanged := contentHash != state.OutputHash
	hasPrompt := mgr.DetectPrompt(output)
	hasStablePrompt := state.recordPromptSignal(hasPrompt)

	if outputChanged {
		state.OutputHash = contentHash
		state.LastOutput = output
		state.LastOutputAt = time.Now()
	}

	if len(contentHash) > 8 {
		d.debug("%s#%s: pane hash=%s changed=%t prompt=%t stable_prompt=%t streak=%d",
			run.IssueID, run.RunID, contentHash[:8], outputChanged, hasPrompt, hasStablePrompt, state.PromptStreak)
	} else {
		d.debug("%s#%s: pane hash=%s changed=%t prompt=%t stable_prompt=%t streak=%d",
			run.IssueID, run.RunID, contentHash, outputChanged, hasPrompt, hasStablePrompt, state.PromptStreak)
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
	newStatus := mgr.GetStatus(run, output, agentState, outputChanged, hasStablePrompt)

	if newStatus != "" && newStatus != run.Status {
		d.logger.Printf("%s#%s: status change %s -> %s", run.IssueID, run.RunID, run.Status, newStatus)
		if err := d.updateStatus(run, newStatus, st); err != nil {
			return err
		}
		d.notifyStatusChange(run, newStatus, output, st)
	}

	return nil
}

func captureEndpointKey(run *model.Run, mgr agent.AgentManager) string {
	if run.Agent == string(agent.AgentOpenCode) {
		if opencodeMgr, ok := mgr.(*agent.OpenCodeManager); ok && opencodeMgr.Port > 0 {
			return "opencode:" + strconv.Itoa(opencodeMgr.Port)
		}
		if run.ServerPort > 0 {
			return "opencode:" + strconv.Itoa(run.ServerPort)
		}
		return "opencode:unknown"
	}

	sessionName := run.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
	}
	agentName := run.Agent
	if agentName == "" {
		agentName = "unknown"
	}
	return agentName + ":" + sessionName
}

func (s *RunState) shouldSkipCapture(endpoint string, now time.Time) bool {
	if endpoint == "" {
		return false
	}

	if s.CaptureEndpoint != endpoint {
		s.resetCaptureFailure()
		s.CaptureEndpoint = endpoint
		return false
	}

	if s.CaptureRetryAt.IsZero() {
		return false
	}

	return now.Before(s.CaptureRetryAt)
}

func (s *RunState) recordCaptureFailure(endpoint string, err error, now time.Time) (time.Time, bool, int) {
	if endpoint == "" {
		endpoint = "unknown"
	}
	if s.CaptureEndpoint != endpoint {
		s.resetCaptureFailure()
		s.CaptureEndpoint = endpoint
	}

	s.CaptureFailureCount++
	backoff := captureBackoffDuration(err, s.CaptureFailureCount)
	s.CaptureRetryAt = now.Add(backoff)

	errorKey := captureErrorKey(endpoint, err)
	if s.CaptureErrorKey == errorKey && !s.CaptureErrorLogAt.IsZero() && now.Sub(s.CaptureErrorLogAt) < captureErrorLogInterval {
		s.SuppressedCaptureLogs++
		return s.CaptureRetryAt, false, 0
	}

	suppressed := s.SuppressedCaptureLogs
	s.SuppressedCaptureLogs = 0
	s.CaptureErrorKey = errorKey
	s.CaptureErrorLogAt = now
	return s.CaptureRetryAt, true, suppressed
}

func (s *RunState) resetCaptureFailure() {
	s.CaptureEndpoint = ""
	s.CaptureFailureCount = 0
	s.CaptureRetryAt = time.Time{}
	s.CaptureErrorKey = ""
	s.CaptureErrorLogAt = time.Time{}
	s.SuppressedCaptureLogs = 0
}

func (s *RunState) recordPromptSignal(hasPrompt bool) bool {
	if hasPrompt {
		s.PromptStreak++
	} else {
		s.PromptStreak = 0
	}

	return hasPrompt && s.PromptStreak >= waitingPromptStreakThreshold
}

func captureBackoffDuration(err error, failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}

	if isConnectionRefusedError(err) {
		backoff := exponentialBackoff(captureRefusedBackoffInitial, captureRefusedBackoffMax, failures)
		if backoff < captureRefusedNegativeCacheTTL {
			backoff = captureRefusedNegativeCacheTTL
		}
		return backoff
	}

	return exponentialBackoff(captureBackoffInitial, captureBackoffMax, failures)
}

func exponentialBackoff(initial, max time.Duration, failures int) time.Duration {
	if failures <= 1 {
		return initial
	}

	backoff := initial
	for i := 1; i < failures; i++ {
		if backoff >= max/2 {
			return max
		}
		backoff *= 2
	}
	if backoff > max {
		return max
	}
	return backoff
}

func captureErrorKey(endpoint string, err error) string {
	if isConnectionRefusedError(err) {
		return endpoint + "|connection-refused"
	}
	if err == nil {
		return endpoint + "|none"
	}
	return endpoint + "|" + err.Error()
}

func isConnectionRefusedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "econnrefused")
}

func (d *Daemon) updateStatus(run *model.Run, status model.Status, st store.Store) error {
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}

	// Check current status - daemon cannot overwrite terminal states
	var fromStatus model.Status
	if currentRun, err := st.GetRun(ref); err == nil && currentRun != nil {
		fromStatus = currentRun.Status
		if !model.CanTransitionStatus(currentRun.Status, status, model.EventSourceDaemon) {
			d.debug("%s#%s: daemon cannot transition from %s to %s", run.IssueID, run.RunID, currentRun.Status, status)
			return nil
		}
	}

	event := model.NewStatusEvent(status)
	if err := st.AppendEvent(ref, event); err != nil {
		return err
	}

	d.publishRunEvent(run, fromStatus, status, model.EventSourceDaemon)

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

// publishRunEvent broadcasts a status transition to subscribers of the
// daemon's run event bus. Safe to call when the socket server is not yet
// constructed (test daemons): the call becomes a no-op.
func (d *Daemon) publishRunEvent(run *model.Run, from, to model.Status, source model.EventSource) {
	if d == nil || d.socketServer == nil || run == nil {
		return
	}
	frame := &orchpb.RunEventFrame{
		RunId:           run.RunID,
		IssueId:         run.IssueID,
		FromStatus:      modelStatusToProto(from),
		ToStatus:        modelStatusToProto(to),
		TimestampUnixMs: time.Now().UnixMilli(),
		Source:          string(source),
	}
	d.socketServer.PublishRunEvent(frame)
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
		prInfo, err := pr.LookupCachedInfoByURL(run.PRUrl)
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

	prInfo, err := pr.LookupCachedInfo(repoRoot, run.Branch)
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
	if newStatus == model.StatusWaiting || newStatus == model.StatusRateLimited {
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

// inferStatusFromGitState infers a run's status from git state when the agent session
// is no longer reachable. wasAlive indicates whether the agent was ever confirmed running.
// When wasAlive is false and no work was done (0 commits, clean worktree), returns
// StatusFailed rather than StatusDone — the agent never started, not "completed."
func (d *Daemon) inferStatusFromGitState(run *model.Run, st store.Store, wasAlive bool) model.Status {
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
	prInfo, err := pr.LookupCachedInfo(repoRoot, run.Branch)
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
		prInfo, err := pr.LookupCachedInfoByURL(run.PRUrl)
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
		return model.StatusWaiting
	}

	if !wasAlive {
		return model.StatusFailed
	}
	return model.StatusDone
}
