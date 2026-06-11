package daemon

// Monitor-plane shells: gather observations about a run (PR cache, session
// liveness, captured output, git evidence), feed them to the pure transition
// function stepRun (step.go), and execute the returned effects. Transition
// POLICY does not live in this file — only observation gathering, effect
// execution, and scheduling concerns (capture pacing, lease backoff).
// See docs/design/run-state-machine.md.

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
		outcome, prURL, discovered := d.gatherPROutcome(run)
		effects, err := d.applyStep(run, st, state, runObservation{
			Kind:            obsPRState,
			PROutcome:       outcome,
			PRURL:           prURL,
			DiscoveredPRURL: discovered,
		}, projectRoot)
		if err != nil {
			return err
		}
		if _, transitioned := statusEffectOf(effects); transitioned {
			return nil
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

	if !mgr.IsAlive(run) {
		if run.Agent == "opencode" && state.DeadCheckCount == 0 {
			if logPath := opencodeServerLogPath(run.WorktreePath); logPath != "" {
				d.logger.Printf("%s#%s: opencode bootstrap logs: %s", run.IssueID, run.RunID, logPath)
			}
		}
		return d.applyGoneObservation(run, st, state, projectRoot, false)
	}

	if _, err := d.applyStep(run, st, state, runObservation{Kind: obsSessionAlive}, projectRoot); err != nil {
		return err
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

// processRunOutput feeds a freshly captured session output through stepRun:
// change hashing, prompt debouncing, PR-URL detection, and agent status
// inference all happen in the pure core. Shared by local and
// worker-delegated runs.
func (d *Daemon) processRunOutput(run *model.Run, st store.Store, state *RunState, mgr agent.AgentManager, output string) error {
	signal := gatherAgentSignal(run, mgr, output)
	_, err := d.applyStep(run, st, state, runObservation{
		Kind:   obsCaptured,
		Output: output,
		Signal: signal,
	}, "")
	return err
}

// gatherAgentSignal produces the agent-specific reading of a captured output
// for stepRun. OpenCode resolves its verdict through the server API (impure,
// so it must happen here); mux agents reduce to pure string heuristics whose
// precedence stepRun applies together with hash/streak state.
func gatherAgentSignal(run *model.Run, mgr agent.AgentManager, output string) agentSignal {
	if _, ok := mgr.(*agent.OpenCodeManager); ok {
		status := mgr.GetStatus(run, output, &agent.RunState{}, false, false)
		return agentSignal{Resolved: true, Status: status}
	}
	return agentSignal{
		Exited:        agent.IsAgentExited(output),
		Completed:     agent.IsCompleted(output),
		APILimited:    agent.IsAPILimited(output),
		Failed:        agent.IsFailed(output),
		PromptShowing: mgr.DetectPrompt(output),
	}
}

// applyStep runs one observation through the pure transition function and
// executes the resulting effects. The new core is committed even when an
// effect fails (executors veto individual flags where a failed write must
// retry — see applyRunEffects).
func (d *Daemon) applyStep(run *model.Run, st store.Store, state *RunState, obs runObservation, projectRoot string) ([]runEffect, error) {
	view := runViewOf(run)
	core, effects := stepRun(view, state.runCore, obs, time.Now())
	core, err := d.applyRunEffects(run, st, state.runCore, core, effects)
	state.runCore = core
	return effects, err
}

// applyRunEffects executes stepRun effects in order. effectGatherGitEvidence
// is a shell-loop concern (applyGoneObservation) and is skipped here.
func (d *Daemon) applyRunEffects(run *model.Run, st store.Store, oldCore, core runCore, effects []runEffect) (runCore, error) {
	var firstErr error
	statusWriteFailed := false
	for _, e := range effects {
		switch e.Kind {
		case effectLog:
			d.logger.Printf("%s", e.Msg)
		case effectDebugLog:
			d.debug("%s", e.Msg)
		case effectRecordPR:
			if err := d.recordPRArtifact(run, e.PRURL, st); err != nil {
				d.logger.Printf("%s#%s: failed to record PR artifact: %v", run.IssueID, run.RunID, err)
				// The PRRecorded flag must reflect the store, not the
				// intent: revert so the next capture retries the write.
				core.PRRecorded = oldCore.PRRecorded
			}
		case effectRecordPRClosed:
			if err := d.recordPRClosedEvent(run, e.PRURL, st); err != nil {
				return core, err
			}
		case effectSetStatus:
			if err := d.updateStatus(run, e.Status, st); err != nil {
				statusWriteFailed = true
				if firstErr == nil {
					firstErr = err
				}
			}
		case effectFireStatusChange:
			if statusWriteFailed {
				continue
			}
			d.fireStatusChange(&runevents.StatusChangeEvent{
				Run:        run,
				From:       e.From,
				To:         e.Status,
				Source:     model.EventSourceDaemon,
				LastOutput: e.Output,
				Store:      st,
			})
		case effectGatherGitEvidence:
			// handled by applyGoneObservation
		}
	}
	return core, firstErr
}

// applyGoneObservation feeds one dead check through stepRun and, when the
// policy requests it, gathers git/PR evidence and feeds that back as a
// second observation. The shell never decides whether evidence is needed.
func (d *Daemon) applyGoneObservation(run *model.Run, st store.Store, state *RunState, projectRoot string, remote bool) error {
	effects, err := d.applyStep(run, st, state, runObservation{Kind: obsSessionGone, Remote: remote}, projectRoot)
	if err != nil {
		return err
	}
	if !effectsContain(effects, effectGatherGitEvidence) {
		return nil
	}
	evidence := d.gatherGitEvidence(run, projectRoot)
	_, err = d.applyStep(run, st, state, runObservation{
		Kind:     obsGitEvidence,
		Evidence: evidence,
		Remote:   remote,
	}, projectRoot)
	return err
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
// stepRun processing as local runs.
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
			return d.applyGoneObservation(run, st, state, projectRoot, true)
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

	if _, err := d.applyStep(run, st, state, runObservation{Kind: obsSessionAlive}, projectRoot); err != nil {
		return err
	}

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

// recordPromptStreak is the pure prompt-debounce rule shared by stepRun and
// RunState.recordPromptSignal: a prompt must persist for
// waitingPromptStreakThreshold consecutive captures before it counts.
func recordPromptStreak(streak int, hasPrompt bool) (int, bool) {
	if hasPrompt {
		streak++
	} else {
		streak = 0
	}
	return streak, hasPrompt && streak >= waitingPromptStreakThreshold
}

func (s *RunState) recordPromptSignal(hasPrompt bool) bool {
	streak, stable := recordPromptStreak(s.PromptStreak, hasPrompt)
	s.PromptStreak = streak
	return stable
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

// updateStatus is the single executor for status transitions (effectSetStatus).
// It re-checks the authoritative store state beneath the stepRun matrix:
// terminal protection, transition legality, and the same-status no-op.
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

	// The sanctioned single writer for monitor-plane status transitions:
	// every transition decided by step() is committed here, and only here
	// (docs/design/run-state-machine.md). All other status writers are
	// frozen legacy, enumerated by `nosemgrep: run-status-write-surface`
	// annotations, and shrink toward zero (coupling-core roadmap Phase B).
	event := model.NewStatusEvent(status) // nosemgrep: run-status-write-surface
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

// detectPRURL scans output for PR creation URLs and returns the first PR URL
// found, or empty string if none.
func detectPRURL(output string) string {
	return prURLRegex.FindString(output)
}

func (d *Daemon) detectPRCreation(output string) string {
	return detectPRURL(output)
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

// gatherPROutcome looks up the run's PR outcome (URL first, then branch).
// Observation only: when a PR is discovered via branch lookup on a run with
// no recorded PR URL, the URL is returned as discoveredURL and stepRun
// decides whether to record it.
func (d *Daemon) gatherPROutcome(run *model.Run) (outcome prOutcome, prURL, discoveredURL string) {
	if run.PRUrl != "" {
		prInfo, err := d.lookupPRInfoByURL(run.PRUrl)
		if err == nil && prInfo != nil {
			url := prInfo.URL
			if url == "" {
				url = run.PRUrl
			}
			return prOutcomeFromInfo(prInfo), url, ""
		}
	}

	if run.Branch == "" {
		return prOutcomeUnknown, "", ""
	}

	var repoRoot string
	var err error
	if run.WorktreePath != "" {
		repoRoot, err = git.FindMainRepoRoot(run.WorktreePath)
	}
	if repoRoot == "" || err != nil {
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return prOutcomeUnknown, "", ""
		}
	}

	prInfo, err := d.lookupPRInfo(repoRoot, run.Branch)
	if err != nil || prInfo == nil {
		return prOutcomeUnknown, "", ""
	}

	if prInfo.URL != "" && run.PRUrl == "" {
		discoveredURL = prInfo.URL
	}

	return prOutcomeFromInfo(prInfo), prInfo.URL, discoveredURL
}

// checkPROutcome preserves the historical gather+record behavior for callers
// outside the stepRun flow (tests): it gathers the PR outcome and records a
// discovered PR immediately.
func (d *Daemon) checkPROutcome(run *model.Run, st store.Store) (prOutcome, string) {
	outcome, prURL, discovered := d.gatherPROutcome(run)
	if discovered != "" && st != nil {
		if err := d.recordPRArtifact(run, discovered, st); err != nil {
			d.logger.Printf("%s#%s: failed to record discovered PR: %v", run.IssueID, run.RunID, err)
		}
	}
	return outcome, prURL
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

// gatherGitEvidence collects the raw PR/git facts for a dead-session verdict
// (obsGitEvidence). It mirrors the historical laziness of
// inferStatusFromGitState: lookups stop as soon as the gitVerdict ladder
// cannot need further facts.
//
// projectRoot is the repo root registered with the daemon for the run's
// project; it is the lookup root for worker-hosted runs whose worktree lives
// on the execution host and is not visible from this machine.
func (d *Daemon) gatherGitEvidence(run *model.Run, projectRoot string) gitEvidence {
	ev := gitEvidence{}
	if run.Branch == "" {
		return ev
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
	ev.RepoRootFound = repoRoot != ""

	if repoRoot != "" {
		prInfo, err := d.lookupPRInfo(repoRoot, run.Branch)
		if err == nil && prInfo != nil && prInfo.URL != "" {
			ev.BranchPRURL = prInfo.URL
			ev.BranchPROutcome = prOutcomeFromInfo(prInfo)
			if statusFromPROutcome(ev.BranchPROutcome) != "" {
				return ev
			}
		}
		if err != nil {
			d.debug("%s#%s: infer: PR lookup error: %v", run.IssueID, run.RunID, err)
		}
	}

	if run.PRUrl != "" {
		prInfo, err := d.lookupPRInfoByURL(run.PRUrl)
		if err == nil && prInfo != nil {
			ev.URLPRFound = true
			ev.URLPROutcome = prOutcomeFromInfo(prInfo)
		}
		if err != nil {
			d.debug("%s#%s: infer: PR URL lookup error: %v", run.IssueID, run.RunID, err)
		}
		// The verdict ladder ends at the PR-URL rung regardless of lookup
		// success (it preserves pr_open) — no further facts needed.
		return ev
	}

	if repoRoot == "" {
		return ev
	}

	baseBranch := "origin/main"
	if d.config != nil && d.config.BaseBranch != "" {
		remote, branch := git.ParseRemoteBranch(d.config.BaseBranch)
		baseBranch = git.RemoteBranchRef(remote, branch)
	}

	aheadCount, err := git.GetAheadCount(repoRoot, run.Branch, baseBranch)
	if err == nil {
		ev.AheadKnown = true
		ev.AheadCount = aheadCount
	}
	ev.HasUncommitted = git.HasUncommittedChanges(run.WorktreePath)

	return ev
}

// inferStatusFromGitState infers a run's status from git state when the agent
// session is no longer reachable. It is the historical entry point preserved
// for callers outside the stepRun flow (tests): the decision ladder itself
// lives in gitVerdict (step.go) so it cannot drift from the monitor plane.
func (d *Daemon) inferStatusFromGitState(run *model.Run, st store.Store, wasAlive bool, projectRoot string) model.Status {
	evidence := d.gatherGitEvidence(run, projectRoot)
	status, effects := gitVerdict(runViewOf(run), runCore{WasAlive: wasAlive}, evidence, time.Now())
	for _, e := range effects {
		switch e.Kind {
		case effectRecordPR:
			if err := d.recordPRArtifact(run, e.PRURL, st); err != nil {
				d.logger.Printf("%s#%s: infer: failed to record PR: %v", run.IssueID, run.RunID, err)
			}
		case effectLog:
			d.logger.Printf("%s", e.Msg)
		case effectDebugLog:
			d.debug("%s", e.Msg)
		}
	}
	return status
}

// withinNeverAliveGrace reports whether a run that has never been observed
// alive is still inside its boot grace window, during which dead checks must
// not conclude a verdict.
func withinNeverAliveGrace(run *model.Run) bool {
	return withinNeverAliveGraceAt(run.StartedAt, time.Now())
}
