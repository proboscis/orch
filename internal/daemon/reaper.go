package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/config"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/multiplexer"
	"github.com/proboscis/orch/internal/store"
)

const (
	SessionReaperInterval = time.Minute
	reaperSnapshotLines   = 2000
)

type reapReason string

const (
	reapReasonTerminalGrace reapReason = "terminal_grace"
	reapReasonResolvedGrace reapReason = "resolved_grace"
	reapReasonIdleTTL       reapReason = "idle_ttl"
)

type reaperSessionObservation struct {
	Content        string
	SessionAlive   bool
	WorktreeExists bool
}

type sessionReaperDeps struct {
	Observe func(run *model.Run, projectID, projectRoot string) (reaperSessionObservation, error)
	Persist func(run *model.Run, content string, now time.Time) (string, error)
	Kill    func(run *model.Run, projectID, projectRoot string) error
}

type reapRunOutcome struct {
	Due        bool
	Reaped     bool
	Reason     reapReason
	KeptReason string
}

type reaperMultiplexer interface {
	HasSession(name string) bool
	CapturePane(session string, lines int) (string, error)
	KillSession(session string) error
}

func (d *Daemon) safeReapAll() {
	defer func() {
		if r := recover(); r != nil {
			logAndRepanic(d.logger, "reapAll", r)
		}
	}()
	d.reapAllAt(time.Now())
}

func (d *Daemon) reapAllAt(now time.Time) {
	if d.socketServer == nil {
		return
	}

	for _, repoCtx := range d.socketServer.GetAllRepoContexts() {
		if repoCtx == nil || repoCtx.Store == nil {
			continue
		}
		if !d.claimReaperPass(repoCtx.RepoID, now) {
			continue
		}

		reaperCfg, err := d.reaperConfigForContext(repoCtx)
		if err != nil {
			d.logReaper("session reaper disabled for project %s: config load failed: %v", repoCtx.RepoID, err)
			continue
		}
		if !reaperCfg.Enabled {
			continue
		}

		runs, err := repoCtx.Store.ListRuns(&store.ListRunsFilter{})
		if err != nil {
			d.logReaper("session reaper failed to list all runs for project %s: %v", repoCtx.RepoID, err)
			continue
		}

		issueStatuses := make(map[model.IssueID]model.IssueStatus)
		issues, issueErr := repoCtx.Store.ListIssues()
		if issueErr != nil {
			d.logReaper("session reaper failed to list issues for project %s; resolved-issue policy unavailable this pass: %v", repoCtx.RepoID, issueErr)
		} else {
			for _, issue := range issues {
				if issue != nil {
					issueStatuses[issue.ID] = issue.Status
				}
			}
		}

		deps := sessionReaperDeps{
			Observe: d.observeSessionForReap,
			Persist: persistSessionSnapshot,
			Kill:    d.killSessionForReap,
		}
		for _, listedRun := range runs {
			if listedRun == nil {
				continue
			}
			// ListRuns may be served by an index/projection that omits the run
			// document path. Re-read the authoritative run before persisting a
			// sidecar and before evaluating the newest event-log interlocks.
			run, err := repoCtx.Store.GetRun(listedRun.Ref())
			if err != nil {
				d.logReaper("session reaper failed to load authoritative run %s: %v", listedRun.Ref().String(), err)
				continue
			}
			outcome, err := reapRun(run, issueStatuses[run.IssueID], repoCtx.Store, repoCtx.RepoID, repoCtx.ProjectRoot, reaperCfg, now, deps)
			if err != nil {
				d.logReaper("session reaper failed for %s: %v", run.Ref().String(), err)
				continue
			}
			if outcome.KeptReason != "" {
				d.logReaper("session reaper kept %s (%s): %s", run.Ref().String(), outcome.Reason, outcome.KeptReason)
			} else if outcome.Reaped {
				d.logReaper("session reaper reaped %s (%s)", run.Ref().String(), outcome.Reason)
			}
		}
	}
}

func (d *Daemon) claimReaperPass(repoID string, now time.Time) bool {
	if strings.TrimSpace(repoID) == "" {
		repoID = "default"
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastReapAt == nil {
		d.lastReapAt = make(map[string]time.Time)
	}
	if last, ok := d.lastReapAt[repoID]; ok && now.Sub(last) < SessionReaperInterval {
		return false
	}
	d.lastReapAt[repoID] = now
	return true
}

func (d *Daemon) reaperConfigForContext(repoCtx *RepoContext) (config.ReaperConfig, error) {
	if repoCtx != nil && strings.TrimSpace(repoCtx.ProjectRoot) != "" {
		cfg, err := config.LoadFromProjectRoot(repoCtx.ProjectRoot)
		if err != nil {
			return config.ReaperConfig{}, err
		}
		return cfg.Reaper, nil
	}
	if d.config != nil {
		return d.config.Reaper, nil
	}
	return config.DefaultReaperConfig(), nil
}

func (d *Daemon) logReaper(format string, args ...interface{}) {
	if d.logger != nil {
		d.logger.Printf(format, args...)
	}
}

func reapRun(
	run *model.Run,
	issueStatus model.IssueStatus,
	st store.Store,
	projectID, projectRoot string,
	reaperCfg config.ReaperConfig,
	now time.Time,
	deps sessionReaperDeps,
) (reapRunOutcome, error) {
	reason, pendingKill := pendingReapReason(run)
	due := pendingKill
	if !pendingKill {
		reason, due = reapReasonForRun(run, issueStatus, reaperCfg, now)
	}
	outcome := reapRunOutcome{Due: due, Reason: reason}
	if !due {
		return outcome, nil
	}
	if st == nil {
		return outcome, fmt.Errorf("store required")
	}
	if deps.Kill == nil {
		return outcome, fmt.Errorf("session reaper dependencies incomplete")
	}

	if keptReason := reapInterlockProblem(run); keptReason != "" {
		outcome.KeptReason = keptReason
		return outcome, nil
	}
	if err := validateReaperMultiplexer(run); err != nil {
		return outcome, err
	}
	if pendingKill {
		if err := attemptReaperKill(run, st, projectID, projectRoot, deps.Kill); err != nil {
			return outcome, err
		}
		outcome.Reaped = true
		return outcome, nil
	}
	if deps.Observe == nil || deps.Persist == nil {
		return outcome, fmt.Errorf("session reaper dependencies incomplete")
	}

	observation, err := deps.Observe(run, projectID, projectRoot)
	if err != nil {
		return outcome, err
	}
	if !observation.WorktreeExists {
		outcome.KeptReason = fmt.Sprintf("worktree does not exist: %s", run.WorktreePath)
		return outcome, nil
	}
	if !observation.SessionAlive {
		return outcome, nil
	}

	snapshotPath, err := deps.Persist(run, observation.Content, now)
	if err != nil {
		return outcome, fmt.Errorf("persist final session snapshot: %w", err)
	}
	if err := st.AppendEvent(run.Ref(), model.NewArtifactEvent("session_snapshot", map[string]string{"path": snapshotPath})); err != nil {
		return outcome, fmt.Errorf("record session_snapshot artifact: %w", err)
	}

	sessionName := model.GenerateSessionName(run.IssueID, run.RunID)
	note := model.NewDaemonNoticeEvent("session_reaped", map[string]string{
		"generation":   strconv.Itoa(run.AgentSessionGeneration),
		"session_name": sessionName,
		"reason":       string(reason),
	})
	if err := st.AppendEvent(run.Ref(), note); err != nil {
		return outcome, fmt.Errorf("record session_reaped note: %w", err)
	}

	if err := attemptReaperKill(run, st, projectID, projectRoot, deps.Kill); err != nil {
		return outcome, err
	}

	outcome.Reaped = true
	return outcome, nil
}

func attemptReaperKill(
	run *model.Run,
	st store.Store,
	projectID, projectRoot string,
	kill func(run *model.Run, projectID, projectRoot string) error,
) error {
	sessionName := model.GenerateSessionName(run.IssueID, run.RunID)
	if err := kill(run, projectID, projectRoot); err != nil {
		killErr := fmt.Errorf("kill session %s for %s: %w", sessionName, run.Ref().String(), err)
		// The returned error is logged on every pass. Keep only the first
		// durable copy of a repeated failure so a broken multiplexer cannot
		// grow the event ledger without bound once the reap note is pending.
		if mostRecentErrorArtifactMessage(run) == killErr.Error() {
			return killErr
		}
		if artifactErr := st.AppendEvent(run.Ref(), model.NewErrorArtifactEvent(killErr.Error())); artifactErr != nil {
			return errors.Join(killErr, fmt.Errorf("record kill error artifact: %w", artifactErr))
		}
		return killErr
	}
	return nil
}

func mostRecentErrorArtifactMessage(run *model.Run) string {
	if run == nil {
		return ""
	}
	for i := len(run.Events) - 1; i >= 0; i-- {
		event := run.Events[i]
		if event != nil && event.Type == model.EventTypeArtifact && event.Name == "error" {
			return event.Attrs["message"]
		}
	}
	return ""
}

func reapReasonForRun(run *model.Run, issueStatus model.IssueStatus, cfg config.ReaperConfig, now time.Time) (reapReason, bool) {
	if run == nil {
		return "", false
	}
	if reason, ok := pendingReapReason(run); ok {
		return reason, true
	}
	if run.UpdatedAt.IsZero() {
		return "", false
	}
	if terminalAt, terminal := terminalStatusTimestamp(run); terminal && !now.Before(terminalAt.Add(time.Duration(cfg.TerminalGraceMinutes)*time.Minute)) {
		return reapReasonTerminalGrace, true
	}
	if issueStatus == model.IssueStatusResolved && !now.Before(run.UpdatedAt.Add(time.Duration(cfg.ResolvedIssueGraceMinutes)*time.Minute)) {
		return reapReasonResolvedGrace, true
	}
	if !now.Before(run.UpdatedAt.Add(time.Duration(cfg.IdleTTLHours) * time.Hour)) {
		return reapReasonIdleTTL, true
	}
	return "", false
}

func terminalStatusTimestamp(run *model.Run) (time.Time, bool) {
	if run == nil || !run.Status.IsTerminal() {
		return time.Time{}, false
	}
	for i := len(run.Events) - 1; i >= 0; i-- {
		event := run.Events[i]
		if event == nil || event.Type != model.EventTypeStatus {
			continue
		}
		status, err := model.NormalizeStatus(event.Name)
		if err == nil && status == run.Status {
			return event.Timestamp, true
		}
	}
	if !run.UpdatedAt.IsZero() {
		return run.UpdatedAt, true
	}
	return time.Time{}, false
}

func pendingReapReason(run *model.Run) (reapReason, bool) {
	if run == nil {
		return "", false
	}
	for i := len(run.Events) - 1; i >= 0; i-- {
		event := run.Events[i]
		if event == nil || event.Type != model.EventTypeNote || event.Name != model.DaemonNoticeEventName || event.Attrs["kind"] != "session_reaped" {
			continue
		}
		generation, err := strconv.Atoi(event.Attrs["generation"])
		if err != nil || generation != run.AgentSessionGeneration {
			continue
		}
		reason := reapReason(event.Attrs["reason"])
		switch reason {
		case reapReasonTerminalGrace, reapReasonResolvedGrace, reapReasonIdleTTL:
			return reason, true
		default:
			return "", false
		}
	}
	return "", false
}

func reapInterlockProblem(run *model.Run) string {
	if run == nil {
		return "run is nil"
	}
	expectedSession := model.GenerateSessionName(run.IssueID, run.RunID)
	if run.SessionName != "" && run.SessionName != expectedSession {
		return fmt.Sprintf("session name %q does not match exact run session %q", run.SessionName, expectedSession)
	}
	if strings.TrimSpace(run.AgentSessionID) == "" && run.Agent != string(agent.AgentOpenCode) {
		return "agent_session identity is not recorded"
	}
	if run.Agent != string(agent.AgentOpenCode) && run.AgentSessionGeneration <= 0 {
		return fmt.Sprintf("agent_session generation is not positive: %d", run.AgentSessionGeneration)
	}
	if run.Agent == string(agent.AgentOpenCode) && !run.Status.IsTerminal() {
		return "non-terminal opencode reaping is outside ADR-0005 Stage 2 semantics"
	}
	if strings.TrimSpace(run.WorktreePath) == "" {
		return "worktree path is not recorded"
	}
	if runHasWorktreeRemovedNote(run) {
		return "worktree_removed note is recorded"
	}
	return ""
}

func runHasWorktreeRemovedNote(run *model.Run) bool {
	if run == nil {
		return false
	}
	for _, event := range run.Events {
		if event == nil || event.Type != model.EventTypeNote {
			continue
		}
		if event.Name == "worktree_removed" || (event.Name == model.DaemonNoticeEventName && event.Attrs["kind"] == "worktree_removed") {
			return true
		}
	}
	return false
}

func (d *Daemon) observeSessionForReap(run *model.Run, projectID, projectRoot string) (reaperSessionObservation, error) {
	if d.socketServer != nil && d.socketServer.runRequiresWorkerDelegation(run, "") {
		return d.observeRemoteSessionForReap(run, projectID, projectRoot)
	}
	return observeLocalSessionForReap(run)
}

func observeLocalSessionForReap(run *model.Run) (reaperSessionObservation, error) {
	observation := reaperSessionObservation{}
	exists, err := worktreeDirectoryExists(run.WorktreePath)
	if err != nil {
		return observation, fmt.Errorf("check worktree %s: %w", run.WorktreePath, err)
	}
	observation.WorktreeExists = exists
	if !exists {
		return observation, nil
	}

	mux, err := reaperMultiplexerForRun(run)
	if err != nil {
		return observation, err
	}
	sessionName := model.GenerateSessionName(run.IssueID, run.RunID)
	if !mux.HasSession(sessionName) {
		return observation, nil
	}
	content, err := mux.CapturePane(sessionName, reaperSnapshotLines)
	if err != nil {
		return observation, fmt.Errorf("capture final pane snapshot from %s: %w", sessionName, err)
	}
	observation.Content = content
	observation.SessionAlive = true
	return observation, nil
}

func (d *Daemon) observeRemoteSessionForReap(run *model.Run, projectID, projectRoot string) (reaperSessionObservation, error) {
	observation := reaperSessionObservation{}
	if d.socketServer == nil {
		return observation, fmt.Errorf("socket server unavailable for worker-hosted run %s", run.Ref().String())
	}
	target, err := resolveWorkerTargetForRunFields(run, projectRoot)
	if err != nil {
		return observation, err
	}
	payload := &WorkerEffectPayload{
		CaptureSession: &CaptureSessionPayload{
			Lines:          reaperSnapshotLines,
			Target:         strings.TrimSpace(run.Target),
			TargetHost:     target.Host,
			TargetWorkerID: target.WorkerID,
			RunSnapshot:    newRunSnapshot(run),
			CheckWorktree:  true,
		},
	}
	lease, err := d.socketServer.acquireWorkerLease(projectID, "capture_session", string(run.IssueID), string(run.RunID), payload)
	if err != nil {
		return observation, err
	}
	completed, err := d.socketServer.waitForWorkerLeaseCompletion(lease.LeaseID, remoteCaptureLeaseTimeout)
	if err != nil {
		return observation, err
	}
	result, err := decodeWorkerEffectResult(completed.ResultJSON)
	if err != nil {
		return observation, err
	}
	if result.CaptureResult == nil {
		return observation, fmt.Errorf("worker lease completed without capture_result")
	}
	if !result.CaptureResult.WorktreeChecked {
		return observation, fmt.Errorf("worker capture did not report the required worktree interlock")
	}
	observation.WorktreeExists = result.CaptureResult.WorktreeExists
	if !observation.WorktreeExists || result.CaptureResult.Gone != nil {
		return observation, nil
	}
	observation.Content = result.CaptureResult.Content
	observation.SessionAlive = true
	return observation, nil
}

func (d *Daemon) killSessionForReap(run *model.Run, projectID, projectRoot string) error {
	if d.socketServer != nil && d.socketServer.runRequiresWorkerDelegation(run, "") {
		return d.killRemoteSessionForReap(run, projectID, projectRoot)
	}
	return killLocalSessionForReap(run)
}

func killLocalSessionForReap(run *model.Run) error {
	mux, err := reaperMultiplexerForRun(run)
	if err != nil {
		return err
	}
	sessionName := model.GenerateSessionName(run.IssueID, run.RunID)
	if !mux.HasSession(sessionName) {
		return nil
	}
	if err := mux.KillSession(sessionName); err != nil {
		return fmt.Errorf("multiplexer %s: %w", run.Multiplexer, err)
	}
	return nil
}

func (d *Daemon) killRemoteSessionForReap(run *model.Run, projectID, projectRoot string) error {
	if d.socketServer == nil {
		return fmt.Errorf("socket server unavailable for worker-hosted run %s", run.Ref().String())
	}
	return d.socketServer.killRemoteSessionForReap(run, projectID, projectRoot)
}

func (s *SocketServer) killRemoteSessionForReap(run *model.Run, projectID, projectRoot string) error {
	if s == nil {
		return fmt.Errorf("socket server unavailable for worker-hosted run %s", run.Ref().String())
	}
	target, err := resolveWorkerTargetForRunFields(run, projectRoot)
	if err != nil {
		return err
	}
	payload := &WorkerEffectPayload{
		StopRun: &StopRunPayload{
			ProjectRoot:    projectRoot,
			Target:         strings.TrimSpace(run.Target),
			TargetHost:     target.Host,
			TargetWorkerID: target.WorkerID,
			RunSnapshot:    newRunSnapshot(run),
			ReapSession:    true,
		},
	}
	if _, err := s.withWorkerLease(projectID, "stop_run", string(run.IssueID), string(run.RunID), payload); err != nil {
		executionHost := strings.TrimSpace(target.Host)
		if executionHost == "" {
			executionHost = strings.TrimSpace(target.WorkerID)
		}
		return fmt.Errorf("execution host %s: %w", executionHost, err)
	}
	return nil
}

func reaperMultiplexerForRun(run *model.Run) (reaperMultiplexer, error) {
	if err := validateReaperMultiplexer(run); err != nil {
		return nil, err
	}
	muxType, _ := multiplexer.ParseType(strings.TrimSpace(run.Multiplexer))
	mux, err := multiplexer.GetMultiplexer(muxType)
	if err != nil {
		return nil, fmt.Errorf("resolve multiplexer %q for run %s: %w", run.Multiplexer, run.Ref().String(), err)
	}
	if mux == nil {
		return nil, fmt.Errorf("resolve multiplexer %q for run %s: no multiplexer returned", run.Multiplexer, run.Ref().String())
	}
	return mux, nil
}

func validateReaperMultiplexer(run *model.Run) error {
	if run == nil {
		return fmt.Errorf("run required")
	}
	raw := strings.TrimSpace(run.Multiplexer)
	if raw == "" {
		return fmt.Errorf("run %s has empty multiplexer; refusing session reap", run.Ref().String())
	}
	muxType, err := multiplexer.ParseType(raw)
	if err != nil {
		return fmt.Errorf("run %s has invalid multiplexer %q: %w", run.Ref().String(), raw, err)
	}
	if muxType == multiplexer.TypeAuto {
		return fmt.Errorf("run %s has non-concrete multiplexer %q; refusing session reap", run.Ref().String(), raw)
	}
	return nil
}

func worktreeDirectoryExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

func persistSessionSnapshot(run *model.Run, content string, now time.Time) (string, error) {
	if run == nil {
		return "", fmt.Errorf("run required")
	}
	if strings.TrimSpace(run.Path) == "" {
		return "", fmt.Errorf("run %s has no run document path", run.Ref().String())
	}
	logDir := strings.TrimSuffix(run.Path, ".md") + ".log"
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("create run log directory %s: %w", logDir, err)
	}
	name := fmt.Sprintf("session-snapshot-g%d-%d.txt", run.AgentSessionGeneration, now.UTC().UnixNano())
	path := filepath.Join(logDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write session snapshot %s: %w", path, err)
	}
	return path, nil
}
