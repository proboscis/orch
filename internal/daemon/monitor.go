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
	"github.com/s22625/orch/internal/runevents"
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

	// Worker-delegated runs are observed through capture_session leases,
	// which cost a worker round-trip — poll them less often than local
	// panes, and never block the monitor loop on a lease for long.
	remoteCaptureInterval     = 15 * time.Second
	remoteCaptureLeaseTimeout = 15 * time.Second
	remoteCaptureLines        = 200

	// A run that was never observed alive gets this long from StartedAt
	// before dead checks may conclude a verdict (unknown/failed): worker
	// delegation, worktree setup, and agent boot all happen before the
	// session becomes observable, and a premature verdict makes
	// `orch wait` fire spuriously on a run that is still booting.
	neverAliveVerdictGrace = 3 * time.Minute

	// How often to re-log the "never confirmed alive" wait for a run whose
	// session has not appeared (once per ~10 minutes at 5s ticks).
	neverAliveLogEvery = 120
)

type prOutcome string

const (
	prOutcomeUnknown prOutcome = "unknown"
	prOutcomeOpen    prOutcome = "open"
	prOutcomeMerged  prOutcome = "merged"
	prOutcomeClosed  prOutcome = "closed"
)

func (d *Daemon) monitorRun(run *model.Run, st store.Store, projectID, projectRoot string) error {
	if run.Status.IsTerminal() {
		return nil
	}

	state := d.getOrCreateState(run)
	state.LastCheckAt = time.Now()

	// Check terminal PR outcomes before liveness/dead checks. Closed PRs are
	// terminal too; leaving them in pr_open causes the watcher to poll forever.
	if run.Branch != "" || run.PRUrl != "" {
		outcome, prURL := d.checkPROutcome(run, st)
		switch outcome {
		case prOutcomeMerged:
			if prURL != "" {
				d.logger.Printf("%s#%s: detected merged PR (%s), transitioning to done", run.IssueID, run.RunID, prURL)
			} else {
				d.logger.Printf("%s#%s: detected merged PR, transitioning to done", run.IssueID, run.RunID)
			}
			return d.updateStatus(run, model.StatusDone, st)
		case prOutcomeClosed:
			if prURL != "" {
				d.logger.Printf("%s#%s: detected closed PR (%s), transitioning to canceled", run.IssueID, run.RunID, prURL)
			} else {
				d.logger.Printf("%s#%s: detected closed PR, transitioning to canceled", run.IssueID, run.RunID)
			}
			if err := d.recordPRClosedEvent(run, prURL, st); err != nil {
				return err
			}
			return d.updateStatus(run, model.StatusCanceled, st)
		}
	}

	mgr := agent.GetManager(run)

	// Runs executing on another host via the worker plane cannot be observed
	// with local multiplexer calls: IsAlive/CaptureOutput would always fail
	// and the run would sit in "agent not alive yet" forever. Route their
	// liveness + capture through a worker lease instead.
	if d.runIsWorkerDelegated(run) {
		return d.monitorRemoteRun(run, st, projectID, projectRoot, state, mgr)
	}

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
			// Agent was never confirmed alive. Check for clear completion
			// signals (merged/open PR, commits) before giving up — the daemon
			// may have started after the agent finished. A gone session must
			// never mask completed work, regardless of agent.
			if state.DeadCheckCount >= deadChecksBeforeFailed {
				inferredStatus := d.inferStatusFromGitState(run, st, false, projectRoot)
				if inferredStatus != "" {
					if inferredStatus != run.Status {
						if opencodeLogPath != "" {
							d.logger.Printf("%s#%s: agent never confirmed alive, but inferred status from git state: %s (opencode logs: %s)", run.IssueID, run.RunID, inferredStatus, opencodeLogPath)
						} else {
							d.logger.Printf("%s#%s: agent never confirmed alive, but inferred status from git state: %s", run.IssueID, run.RunID, inferredStatus)
						}
					}
					return d.updateStatus(run, inferredStatus, st)
				}
			}
			if state.DeadCheckCount <= deadChecksBeforeFailed || state.DeadCheckCount%neverAliveLogEvery == 0 {
				d.logger.Printf("%s#%s: agent not alive yet (never confirmed alive), waiting", run.IssueID, run.RunID)
			} else {
				d.debug("%s#%s: agent not alive yet (never confirmed alive), waiting", run.IssueID, run.RunID)
			}
			return nil
		}
		if state.DeadCheckCount < deadChecksBeforeFailed {
			d.logger.Printf("%s#%s: agent not alive (%d/%d checks), waiting", run.IssueID, run.RunID, state.DeadCheckCount, deadChecksBeforeFailed)
			return nil
		}
		inferredStatus := d.inferStatusFromGitState(run, st, true, projectRoot)
		if inferredStatus != "" {
			if inferredStatus != run.Status {
				if opencodeLogPath != "" {
					d.logger.Printf("%s#%s: agent session gone, inferred status from git state: %s (opencode logs: %s)", run.IssueID, run.RunID, inferredStatus, opencodeLogPath)
				} else {
					d.logger.Printf("%s#%s: agent session gone, inferred status from git state: %s", run.IssueID, run.RunID, inferredStatus)
				}
			}
			return d.updateStatus(run, inferredStatus, st)
		}
		if run.Agent == "opencode" {
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

	return d.processRunOutput(run, st, state, mgr, output)
}

// processRunOutput runs the host-agnostic part of the monitor cycle on a
// freshly captured session output: change hashing, prompt debouncing, PR-URL
// detection, and agent status inference. Shared by local and worker-delegated
// runs.
func (d *Daemon) processRunOutput(run *model.Run, st store.Store, state *RunState, mgr agent.AgentManager, output string) error {
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
		prevStatus := run.Status
		if err := d.updateStatus(run, newStatus, st); err != nil {
			return err
		}
		d.fireStatusChange(&runevents.StatusChangeEvent{
			Run:        run,
			From:       prevStatus,
			To:         newStatus,
			Source:     model.EventSourceDaemon,
			LastOutput: output,
			Store:      st,
		})
	}

	return nil
}

// runIsWorkerDelegated reports whether the run executes on another host via
// the worker plane, in which case local multiplexer liveness/capture cannot
// observe it.
func (d *Daemon) runIsWorkerDelegated(run *model.Run) bool {
	if d.socketServer == nil {
		return false
	}
	return d.socketServer.runRequiresWorkerDelegation(run, "")
}

// monitorRemoteRun observes a worker-hosted run by delegating capture_session
// to the worker on the run's execution host. A successful capture doubles as
// the liveness signal; the captured output then flows through the same
// processing as local runs (PR detection, prompt/status inference).
func (d *Daemon) monitorRemoteRun(run *model.Run, st store.Store, projectID, projectRoot string, state *RunState, mgr agent.AgentManager) error {
	now := time.Now()

	endpoint := "worker:" + remoteRunWorkerEndpoint(run)
	if state.shouldSkipCapture(endpoint, now) {
		return nil
	}
	if !state.RemoteCaptureAt.IsZero() && now.Sub(state.RemoteCaptureAt) < remoteCaptureInterval {
		return nil
	}

	capture := d.remoteCaptureFn
	if capture == nil {
		if d.socketServer == nil {
			return nil
		}
		capture = d.socketServer.captureRunOutputViaWorker
	}

	output, err := capture(run, projectID, projectRoot, remoteCaptureLines)
	if err != nil {
		if isRemoteSessionGone(err) {
			// The worker answered authoritatively: the lease round-trip
			// completed, so pace re-checks like successful captures instead
			// of retrying every monitor tick.
			state.RemoteCaptureAt = now
			state.DeadCheckCount++
			if state.DeadCheckCount < deadChecksBeforeFailed {
				d.logger.Printf("%s#%s: remote session not found on worker (check %d/%d): %v", run.IssueID, run.RunID, state.DeadCheckCount, deadChecksBeforeFailed, err)
				return nil
			}
			// The session is gone on the execution host. Same rule as local
			// runs: a gone session must never mask completed work, so infer
			// from PR/git state before falling back to unknown/failed.
			if inferred := d.inferStatusFromGitState(run, st, state.WasAlive, projectRoot); inferred != "" {
				if inferred != run.Status {
					d.logger.Printf("%s#%s: remote session gone, inferred status from git state: %s", run.IssueID, run.RunID, inferred)
				}
				return d.updateStatus(run, inferred, st)
			}
			if !state.WasAlive {
				if withinNeverAliveGrace(run) {
					d.debug("%s#%s: remote session not observable yet (within boot grace), waiting", run.IssueID, run.RunID)
					return nil
				}
				if run.Status != model.StatusUnknown {
					d.logger.Printf("%s#%s: remote agent never confirmed alive after %d checks, marking unknown", run.IssueID, run.RunID, state.DeadCheckCount)
				}
				return d.updateStatus(run, model.StatusUnknown, st)
			}
			d.logger.Printf("%s#%s: remote agent confirmed dead after %d checks, marking failed", run.IssueID, run.RunID, state.DeadCheckCount)
			return d.updateStatus(run, model.StatusFailed, st)
		}

		// Worker-plane infrastructure failure (worker offline, lease
		// timeout, missing project mapping): the session may be perfectly
		// healthy on its host, so back off without dead-check counting.
		retryAt, shouldLog, suppressed := state.recordCaptureFailure(endpoint, err, now)
		if shouldLog {
			retryIn := retryAt.Sub(now).Round(time.Second)
			if retryIn < time.Second {
				retryIn = time.Second
			}
			if suppressed > 0 {
				d.logger.Printf("%s#%s: failed to capture remote output from %s: %v (next retry in %s, suppressed %d similar errors)", run.IssueID, run.RunID, endpoint, err, retryIn, suppressed)
			} else {
				d.logger.Printf("%s#%s: failed to capture remote output from %s: %v (next retry in %s)", run.IssueID, run.RunID, endpoint, err, retryIn)
			}
		}
		return nil
	}

	state.resetCaptureFailure()
	state.RemoteCaptureAt = now
	state.WasAlive = true
	state.DeadCheckCount = 0

	return d.processRunOutput(run, st, state, mgr, output)
}

// remoteRunWorkerEndpoint identifies the worker/session pair a remote run is
// observed through, for capture-failure backoff bookkeeping.
func remoteRunWorkerEndpoint(run *model.Run) string {
	workerID := strings.TrimSpace(run.TargetWorkerID)
	if workerID == "" && strings.TrimSpace(run.TargetHost) != "" {
		workerID = HostWorkerID(run.TargetHost)
	}
	if workerID == "" {
		workerID = strings.TrimSpace(run.Target)
	}
	sessionName := run.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
	}
	return workerID + ":" + sessionName
}

// isRemoteSessionGone reports whether a worker-lease capture error means the
// session (or its run record) is genuinely gone on the execution host. Worker
// plane infrastructure errors must NOT count as agent death — default to
// false for anything ambiguous.
func isRemoteSessionGone(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no local project mapping") ||
		strings.Contains(msg, "no active worker available") ||
		strings.Contains(msg, "worker lease timed out") ||
		strings.Contains(msg, "lease not found") {
		return false
	}
	return msg == "not_found" ||
		strings.Contains(msg, "session_not_found") ||
		strings.Contains(msg, "not found (run may not be active)") ||
		strings.Contains(msg, "no server running") ||
		strings.Contains(msg, "can't find pane") ||
		strings.Contains(msg, "can't find session")
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
		// Re-affirming the current status is a no-op: appending duplicate
		// status events bloats the run record and churns UpdatedAt, which
		// breaks recency display/sorting for every client.
		if fromStatus == status {
			return nil
		}
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
//
// Skips emission when from == to to avoid leaking redundant updates
// caused by callers that re-affirm a status (e.g. PR detection running
// every cycle after RunState in-memory bookkeeping is reset by a
// daemon restart).
func (d *Daemon) publishRunEvent(run *model.Run, from, to model.Status, source model.EventSource) {
	if d == nil || d.socketServer == nil || run == nil {
		return
	}
	if from == to {
		return
	}
	fromProto, err := modelStatusToProto(from)
	if err != nil {
		panic(err)
	}
	toProto, err := modelStatusToProto(to)
	if err != nil {
		panic(err)
	}
	frame := &orchpb.RunEventFrame{
		RunId:           string(run.RunID),
		IssueId:         string(run.IssueID),
		ShortId:         string(model.GenerateShortID(run.IssueID, run.RunID)),
		FromStatus:      fromProto,
		ToStatus:        toProto,
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

func (d *Daemon) recordPRClosedEvent(run *model.Run, prURL string, st store.Store) error {
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	attrs := map[string]string{}
	if prURL != "" {
		attrs["url"] = prURL
	}
	return st.AppendEvent(ref, model.NewArtifactEvent("pr_closed", attrs))
}

func (d *Daemon) checkPROutcome(run *model.Run, st store.Store) (prOutcome, string) {
	if run.PRUrl != "" {
		prInfo, err := d.lookupPRInfoByURL(run.PRUrl)
		if err == nil && prInfo != nil {
			prURL := prInfo.URL
			if prURL == "" {
				prURL = run.PRUrl
			}
			return prOutcomeFromInfo(prInfo), prURL
		}
	}

	if run.Branch == "" {
		return prOutcomeUnknown, ""
	}

	var repoRoot string
	var err error
	if run.WorktreePath != "" {
		repoRoot, err = git.FindMainRepoRoot(run.WorktreePath)
	}
	if repoRoot == "" || err != nil {
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return prOutcomeUnknown, ""
		}
	}

	prInfo, err := d.lookupPRInfo(repoRoot, run.Branch)
	if err != nil || prInfo == nil {
		return prOutcomeUnknown, ""
	}

	if prInfo.URL != "" && run.PRUrl == "" && st != nil {
		if err := d.recordPRArtifact(run, prInfo.URL, st); err != nil {
			d.logger.Printf("%s#%s: failed to record discovered PR: %v", run.IssueID, run.RunID, err)
		}
	}

	return prOutcomeFromInfo(prInfo), prInfo.URL
}

func (d *Daemon) lookupPRInfo(repoRoot, branch string) (*pr.Info, error) {
	if d.lookupPRInfoFn != nil {
		return d.lookupPRInfoFn(repoRoot, branch)
	}
	return pr.LookupCachedInfo(repoRoot, branch)
}

func (d *Daemon) lookupPRInfoByURL(prURL string) (*pr.Info, error) {
	if d.lookupPRInfoByURLFn != nil {
		return d.lookupPRInfoByURLFn(prURL)
	}
	return pr.LookupCachedInfoByURL(prURL)
}

func prOutcomeFromInfo(prInfo *pr.Info) prOutcome {
	if prInfo == nil {
		return prOutcomeUnknown
	}
	switch strings.ToUpper(prInfo.State) {
	case "OPEN":
		return prOutcomeOpen
	case "MERGED":
		return prOutcomeMerged
	case "CLOSED":
		return prOutcomeClosed
	default:
		return prOutcomeUnknown
	}
}

func statusFromPROutcome(outcome prOutcome) model.Status {
	switch outcome {
	case prOutcomeOpen:
		return model.StatusPROpen
	case prOutcomeMerged:
		return model.StatusDone
	case prOutcomeClosed:
		return model.StatusCanceled
	default:
		return ""
	}
}

// inferStatusFromGitState infers a run's status from git state when the agent session
// is no longer reachable. wasAlive indicates whether the agent was ever confirmed running.
// When wasAlive is false and no work was done (0 commits, clean worktree), returns
// StatusFailed rather than StatusDone — the agent never started, not "completed."
//
// projectRoot is the repo root registered with the daemon for the run's
// project; it is the lookup root for worker-hosted runs whose worktree lives
// on the execution host and is not visible from this machine.
func (d *Daemon) inferStatusFromGitState(run *model.Run, st store.Store, wasAlive bool, projectRoot string) model.Status {
	if run.Branch == "" {
		d.debug("%s#%s: infer: skipping - branch=%q worktree=%q", run.IssueID, run.RunID, run.Branch, run.WorktreePath)
		return ""
	}

	repoRoot := ""
	if run.WorktreePath != "" {
		if root, err := git.FindMainRepoRoot(run.WorktreePath); err == nil {
			repoRoot = root
		}
	}
	if repoRoot == "" && projectRoot != "" {
		if root, err := git.FindMainRepoRoot(projectRoot); err == nil {
			repoRoot = root
		}
	}

	if repoRoot != "" {
		d.debug("%s#%s: infer: checking PR for branch %s", run.IssueID, run.RunID, run.Branch)
		prInfo, err := d.lookupPRInfo(repoRoot, run.Branch)
		if err == nil && prInfo != nil && prInfo.URL != "" {
			d.debug("%s#%s: infer: found PR %s (state=%s)", run.IssueID, run.RunID, prInfo.URL, prInfo.State)
			if run.PRUrl == "" {
				if err := d.recordPRArtifact(run, prInfo.URL, st); err != nil {
					d.logger.Printf("%s#%s: infer: failed to record PR: %v", run.IssueID, run.RunID, err)
				}
			}
			if status := statusFromPROutcome(prOutcomeFromInfo(prInfo)); status != "" {
				return status
			}
		}
		if err != nil {
			d.debug("%s#%s: infer: PR lookup error: %v", run.IssueID, run.RunID, err)
		} else {
			d.debug("%s#%s: infer: no PR found", run.IssueID, run.RunID)
		}
	}

	// If branch-based lookup failed but run already has a PR URL, check PR status by URL.
	// This handles cases where the local branch was deleted/rebased but PR still exists,
	// and worker-hosted runs whose worktree/repo is not visible from this host.
	if run.PRUrl != "" {
		d.debug("%s#%s: infer: branch lookup failed, checking existing PR URL: %s", run.IssueID, run.RunID, run.PRUrl)
		prInfo, err := d.lookupPRInfoByURL(run.PRUrl)
		if err == nil && prInfo != nil {
			d.debug("%s#%s: infer: PR %s state=%s (via URL lookup)", run.IssueID, run.RunID, prInfo.URL, prInfo.State)
			if status := statusFromPROutcome(prOutcomeFromInfo(prInfo)); status != "" {
				return status
			}
		}
		if err != nil {
			d.debug("%s#%s: infer: PR URL lookup error: %v", run.IssueID, run.RunID, err)
		}
		// If URL lookup also fails, preserve PR_OPEN status since we know a PR exists
		d.debug("%s#%s: infer: preserving pr_open status (PR URL exists but lookup failed)", run.IssueID, run.RunID)
		return model.StatusPROpen
	}

	if repoRoot == "" {
		d.debug("%s#%s: infer: cannot find repo root (worktree=%q project_root=%q)", run.IssueID, run.RunID, run.WorktreePath, projectRoot)
		return ""
	}

	baseBranch := "origin/main"
	if d.config != nil && d.config.BaseBranch != "" {
		remote, branch := git.ParseRemoteBranch(d.config.BaseBranch)
		baseBranch = git.RemoteBranchRef(remote, branch)
	}

	aheadCount, err := git.GetAheadCount(repoRoot, run.Branch, baseBranch)
	if err != nil {
		d.debug("%s#%s: infer: cannot get ahead count: %v", run.IssueID, run.RunID, err)
		return ""
	}

	hasUncommitted := git.HasUncommittedChanges(run.WorktreePath)

	d.debug("%s#%s: infer: commits ahead=%d, uncommitted=%v", run.IssueID, run.RunID, aheadCount, hasUncommitted)

	if aheadCount > 0 || hasUncommitted {
		return model.StatusWaiting
	}

	// No positive signals (no PR, no commits, clean worktree). The verdict
	// depends on agent lifecycle: opencode exits when finished, so a gone
	// session with no work means the run produced nothing. Interactive
	// agents (codex, claude) keep their session open while idle, so a gone
	// session with no work is not a completion signal — leave the verdict
	// to the caller.
	if run.Agent != "opencode" {
		return ""
	}
	if !wasAlive {
		if withinNeverAliveGrace(run) {
			d.debug("%s#%s: infer: within boot grace, no verdict yet", run.IssueID, run.RunID)
			return ""
		}
		return model.StatusFailed
	}
	return model.StatusDone
}

// withinNeverAliveGrace reports whether a run that has never been observed
// alive is still inside its boot grace window, during which dead checks must
// not conclude a verdict.
func withinNeverAliveGrace(run *model.Run) bool {
	return !run.StartedAt.IsZero() && time.Since(run.StartedAt) < neverAliveVerdictGrace
}
