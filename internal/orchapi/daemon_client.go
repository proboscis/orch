package orchapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/daemon"
	"github.com/proboscis/orch/internal/model"
)

type DaemonClient struct {
	proto      *daemon.ProtoClient
	daemonAddr string
}

const (
	daemonRepairTimeout = 2 * time.Second
	daemonRetryInterval = 100 * time.Millisecond
)

func NewDaemonClient(projectRoot string) *DaemonClient {
	return NewDaemonClientWithAddress(projectRoot, "")
}

func NewDaemonClientWithAddress(projectRoot, daemonAddr string) *DaemonClient {
	return &DaemonClient{
		proto:      daemon.NewProtoClientWithAddress(projectRoot, daemonAddr),
		daemonAddr: strings.TrimSpace(daemonAddr),
	}
}

func (c *DaemonClient) isRemote() bool {
	return c.daemonAddr != ""
}

func (c *DaemonClient) IsAvailable() bool {
	return c.proto.IsAvailable()
}

func (c *DaemonClient) Ping(ctx context.Context) error {
	return c.proto.Ping()
}

func (c *DaemonClient) PingStatus(ctx context.Context) (*PingStatus, error) {
	resp, err := c.proto.PingStatus()
	if err != nil {
		return nil, err
	}
	return &PingStatus{
		OK:      resp.OK,
		Version: resp.Version,
	}, nil
}

func (c *DaemonClient) EnsureDaemonHealthy(ctx context.Context) error {
	if c.isRemote() {
		if err := c.proto.Ping(); err != nil {
			return fmt.Errorf("failed to reach remote daemon at %s: %w", c.daemonAddr, err)
		}
		return nil
	}

	if err := c.proto.Ping(); err == nil {
		return nil
	}

	if daemon.IsRunning("") {
		if err := daemon.KillDaemon(""); err != nil {
			return fmt.Errorf("failed to kill stale daemon: %w", err)
		}
		time.Sleep(daemonRetryInterval)
	}

	if _, err := daemon.StartInBackground(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	deadline := time.Now().Add(daemonRepairTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		time.Sleep(daemonRetryInterval)
		if err := c.proto.Ping(); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("daemon did not become healthy within %v: %w", daemonRepairTimeout, lastErr)
	}
	return fmt.Errorf("daemon did not become healthy within %v", daemonRepairTimeout)
}

func (c *DaemonClient) GetIssue(ctx context.Context, issueID model.IssueID) (*Issue, error) {
	resp, err := c.proto.GetIssue(string(issueID))
	if err != nil {
		if isNotFoundError(err) {
			return nil, IssueNotFound(string(issueID))
		}
		return nil, err
	}
	return issueFromDaemon(resp.Issue), nil
}

func (c *DaemonClient) ListIssues(ctx context.Context, filter *ListIssuesFilter) (*ListIssuesResult, error) {
	var statuses []string
	var limit int
	var cursor string

	if filter != nil {
		for _, s := range filter.Status {
			statuses = append(statuses, string(s))
		}
		limit = filter.Limit
		cursor = filter.Cursor
	}

	resp, err := c.proto.ListIssues(statuses, limit, cursor)
	if err != nil {
		return nil, err
	}

	issues := make([]*Issue, len(resp.Issues))
	for i, iss := range resp.Issues {
		issues[i] = issueSummaryToIssue(iss)
	}

	var next string
	if resp.NextCursor != nil {
		next = *resp.NextCursor
	}

	return &ListIssuesResult{
		Issues:     issues,
		Total:      resp.Total,
		NextCursor: next,
	}, nil
}

func (c *DaemonClient) CreateIssue(ctx context.Context, req *CreateIssueRequest) (*Issue, error) {
	resp, err := c.proto.CreateIssue(string(req.ID), req.Title, "", req.Body, req.Tags, req.BaseBranch)
	if err != nil {
		return nil, err
	}
	return &Issue{
		ID:         model.IssueID(resp.IssueID),
		Path:       resp.Path,
		Title:      req.Title,
		Body:       req.Body,
		BaseBranch: req.BaseBranch,
	}, nil
}

func (c *DaemonClient) SetIssueStatus(ctx context.Context, issueID model.IssueID, status IssueStatus) error {
	if status == IssueStatusClosed || status == IssueStatusResolved {
		_, err := c.proto.CloseIssue(string(issueID), "")
		return err
	}
	return errors.New("only close/resolved status supported via daemon")
}

func (c *DaemonClient) CloseIssue(ctx context.Context, issueID model.IssueID) error {
	_, err := c.proto.CloseIssue(string(issueID), "")
	return err
}

func (c *DaemonClient) ResolveRun(ctx context.Context, ref RunRef) (*Run, error) {
	if ref.ShortID != "" {
		resp, err := c.proto.GetRunByShortID(string(ref.ShortID))
		if err != nil {
			if isNotFoundError(err) {
				return nil, RunNotFound(string(ref.ShortID))
			}
			return nil, mapAmbiguousRefError(err)
		}
		return runFromDaemonFull(resp.Run), nil
	}

	if ref.RunID != "" {
		resp, err := c.proto.GetRun(string(ref.IssueID), string(ref.RunID))
		if err != nil {
			if isNotFoundError(err) {
				return nil, RunNotFound(ref.String())
			}
			return nil, mapAmbiguousRefError(err)
		}
		return runFromDaemonFull(resp.Run), nil
	}

	runs, err := c.proto.ListRuns(&daemon.ListRunsFilter{IssueID: string(ref.IssueID), Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(runs.Runs) == 0 {
		return nil, RunNotFound(string(ref.IssueID))
	}
	resp, err := c.proto.GetRun(runs.Runs[0].IssueID, runs.Runs[0].RunID)
	if err != nil {
		return nil, mapAmbiguousRefError(err)
	}
	return runFromDaemonFull(resp.Run), nil
}

func (c *DaemonClient) GetRun(ctx context.Context, issueID model.IssueID, runID model.RunID) (*Run, error) {
	resp, err := c.proto.GetRun(string(issueID), string(runID))
	if err != nil {
		if isNotFoundError(err) {
			return nil, RunNotFound(string(issueID) + "#" + string(runID))
		}
		return nil, mapAmbiguousRefError(err)
	}
	return runFromDaemonFull(resp.Run), nil
}

func (c *DaemonClient) GetLatestRun(ctx context.Context, issueID model.IssueID) (*Run, error) {
	runs, err := c.proto.ListRuns(&daemon.ListRunsFilter{IssueID: string(issueID), Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(runs.Runs) == 0 {
		return nil, RunNotFound(string(issueID))
	}
	resp, err := c.proto.GetRun(runs.Runs[0].IssueID, runs.Runs[0].RunID)
	if err != nil {
		return nil, err
	}
	return runFromDaemonFull(resp.Run), nil
}

func (c *DaemonClient) ListRuns(ctx context.Context, filter *ListRunsFilter) (*ListRunsResult, error) {
	var protoFilter *daemon.ListRunsFilter
	if filter != nil {
		statuses := make([]string, len(filter.Status))
		for i, s := range filter.Status {
			statuses[i] = string(s)
		}
		protoFilter = &daemon.ListRunsFilter{
			IssueID:   string(filter.IssueID),
			Status:    statuses,
			Limit:     filter.Limit,
			Cursor:    filter.Cursor,
			OlderThan: filter.OlderThan,
		}
	}

	resp, err := c.proto.ListRuns(protoFilter)
	if err != nil {
		return nil, err
	}

	runs := make([]*Run, len(resp.Runs))
	for i, r := range resp.Runs {
		runs[i] = runFromDaemonSummary(r)
	}

	var next string
	if resp.NextCursor != nil {
		next = *resp.NextCursor
	}

	return &ListRunsResult{
		Runs:       runs,
		Total:      resp.Total,
		NextCursor: next,
	}, nil
}

func (c *DaemonClient) StartRun(ctx context.Context, req *StartRunRequest) (*StartRunResult, error) {
	resp, err := c.proto.StartRun(&daemon.StartRunOptions{
		IssueID:        req.IssueID,
		RunID:          req.RunID,
		Agent:          req.Agent,
		AgentCmd:       req.AgentCmd,
		AgentProfile:   req.AgentProfile,
		CodexProfile:   req.CodexProfile,
		Model:          req.Model,
		ModelVariant:   req.ModelVariant,
		Preset:         req.Preset,
		BaseBranch:     req.BaseBranch,
		Branch:         req.Branch,
		WorktreeDir:    req.WorktreeDir,
		NoPR:           req.NoPR,
		PromptTemplate: req.PromptTemplate,
		PRTargetBranch: req.PRTargetBranch,
		DryRun:         req.DryRun,
		Reuse:          req.Reuse,
		Multiplexer:    req.Multiplexer,
		Target:         req.Target,
		NoSession:      req.NoSession,
	})
	if err != nil {
		return nil, err
	}
	return &StartRunResult{
		RunID:        model.RunID(resp.RunID),
		Branch:       resp.Branch,
		WorktreePath: resp.WorktreePath,
		SessionName:  resp.SessionName,
		Status:       resp.Status,
	}, nil
}

func (c *DaemonClient) StopRun(ctx context.Context, ref RunRef) error {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return err
	}
	_, err = c.proto.StopRun(string(run.IssueID), string(run.RunID), false)
	return err
}

func (c *DaemonClient) MarkRunResolved(ctx context.Context, ref RunRef) error {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return err
	}
	_, err = c.proto.ResolveRun(string(run.IssueID), string(run.RunID))
	return err
}

func (c *DaemonClient) AppendEvent(ctx context.Context, ref RunRef, event *Event) (*AppendEventResult, error) {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return nil, err
	}
	resp, err := c.proto.AppendEvent(string(run.IssueID), string(run.RunID), event.Type, event.Name, event.Attrs, "cli")
	if err != nil {
		return nil, err
	}
	return &AppendEventResult{
		Skipped: resp.Skipped,
		Reason:  resp.Reason,
	}, nil
}

func (c *DaemonClient) WaitForRuns(ctx context.Context, refs []string, timeoutSeconds int) (*WaitForRunsResult, error) {
	resp, err := c.proto.WaitForRuns(refs, timeoutSeconds)
	if err != nil {
		if isTimeoutError(err) {
			return nil, fmt.Errorf("timed out waiting for runs: %w", ErrTimeout)
		}
		return nil, mapAmbiguousRefError(err)
	}

	status, err := NormalizeRunStatus(resp.Status)
	if err != nil {
		return nil, fmt.Errorf("invalid wait status from daemon: %w", err)
	}

	return &WaitForRunsResult{
		RunID:   model.RunID(resp.RunID),
		Status:  status,
		IssueID: model.IssueID(resp.Issue),
		PRURL:   resp.PRURL,
	}, nil
}

func (c *DaemonClient) GetAttachInfo(ctx context.Context, ref RunRef) (*AttachInfo, error) {
	resp, err := c.proto.GetAttachInfo(string(ref.IssueID), string(ref.RunID), string(ref.ShortID))
	if err != nil {
		if isNotFoundError(err) {
			return nil, RunNotFound(ref.String())
		}
		return nil, mapAmbiguousRefError(err)
	}
	if !resp.OK {
		if resp.Error == "session_not_found" || resp.Error == "no sessions" {
			return &AttachInfo{
				IssueID:       model.IssueID(resp.IssueID),
				RunID:         model.RunID(resp.RunID),
				SessionName:   resp.SessionName,
				Multiplexer:   Multiplexer(resp.Multiplexer),
				WorktreePath:  resp.WorktreePath,
				TargetHost:    resp.TargetHost,
				SessionExists: false,
			}, nil
		}
		return nil, errors.New(resp.Error)
	}
	return &AttachInfo{
		IssueID:           model.IssueID(resp.IssueID),
		RunID:             model.RunID(resp.RunID),
		Agent:             resp.Agent,
		SessionName:       resp.SessionName,
		Multiplexer:       Multiplexer(resp.Multiplexer),
		WorktreePath:      resp.WorktreePath,
		ServerPort:        resp.ServerPort,
		OpenCodeSessionID: resp.OpenCodeSessionID,
		Branch:            resp.Branch,
		TargetHost:        resp.TargetHost,
		SessionExists:     true,
	}, nil
}

func (c *DaemonClient) CaptureSession(ctx context.Context, ref RunRef, lines int) (*CaptureResult, error) {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return nil, err
	}

	resp, err := c.proto.CaptureSession(string(run.IssueID), string(run.RunID), lines)
	if err != nil {
		return nil, err
	}

	return &CaptureResult{
		Content:   resp.Content,
		Timestamp: time.Unix(resp.Timestamp, 0),
		Source:    resp.Source,
	}, nil
}

func (c *DaemonClient) SendMessage(ctx context.Context, ref RunRef, message string, noEnter bool) error {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return err
	}

	return c.proto.SendMessage(string(run.IssueID), string(run.RunID), message, noEnter)
}

func (c *DaemonClient) InjectInitialPrompt(ctx context.Context, ref RunRef, prompt string) error {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return err
	}

	return c.proto.InjectInitialPrompt(string(run.IssueID), string(run.RunID), prompt)
}

func (c *DaemonClient) GetDiffStats(ctx context.Context, ref RunRef) (*DiffStats, error) {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return nil, err
	}

	resp, err := c.proto.GetDiffStats(string(run.IssueID), string(run.RunID))
	if err != nil {
		return nil, err
	}

	return &DiffStats{
		Additions:    resp.Additions,
		Deletions:    resp.Deletions,
		FilesChanged: resp.FilesChanged,
		Files:        resp.Files,
	}, nil
}

func (c *DaemonClient) GetBranchState(ctx context.Context, ref RunRef) (BranchState, error) {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return "", err
	}

	state, err := c.proto.GetBranchState(string(run.IssueID), string(run.RunID))
	if err != nil {
		return "", err
	}

	return BranchState(state), nil
}

func (c *DaemonClient) GetDiff(ctx context.Context, ref RunRef) (string, error) {
	run, err := c.ResolveRun(ctx, ref)
	if err != nil {
		return "", err
	}

	return c.proto.GetDiff(string(run.IssueID), string(run.RunID))
}

func (c *DaemonClient) ResolveIssue(ctx context.Context, issueID model.IssueID, force bool) error {
	_, err := c.proto.ResolveIssue(string(issueID), force)
	return err
}

func (c *DaemonClient) EnsureOpenCodeServer(ctx context.Context) (*OpenCodeServerInfo, error) {
	resp, err := c.proto.GetOpenCodeServer()
	if err != nil {
		return nil, err
	}
	return &OpenCodeServerInfo{
		Port:    resp.Port,
		Healthy: resp.Healthy,
	}, nil
}

func (c *DaemonClient) RegisterMonitor(ctx context.Context, pid int, monitorType, view, project, sessionName string) (*MonitorRegistration, error) {
	resp, err := c.proto.RegisterMonitor(pid, monitorType, view, project, sessionName)
	if err != nil {
		return nil, err
	}
	return &MonitorRegistration{MonitorID: resp.MonitorID}, nil
}

func (c *DaemonClient) MonitorHeartbeat(ctx context.Context, monitorID string) error {
	return c.proto.MonitorHeartbeat(monitorID)
}

func (c *DaemonClient) UnregisterMonitor(ctx context.Context, monitorID string) error {
	return c.proto.UnregisterMonitor(monitorID)
}

func (c *DaemonClient) QueryOpenCodeServer(ctx context.Context, port int) (*QueryOpenCodeServerResult, error) {
	resp, err := c.proto.QueryOpenCodeServer(port)
	if err != nil {
		return nil, err
	}
	providers := make([]ProviderInfo, 0, len(resp.Providers))
	for _, p := range resp.Providers {
		models := make([]ModelInfo, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, ModelInfo{
				ID:       m.ID,
				Name:     m.Name,
				Variants: m.Variants,
			})
		}
		providers = append(providers, ProviderInfo{
			ID:     p.ID,
			Name:   p.Name,
			Models: models,
		})
	}
	return &QueryOpenCodeServerResult{
		ServerRunning: resp.ServerRunning,
		Providers:     providers,
	}, nil
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return s == "daemon error: not_found" || s == "daemon error: issue_not_found" || s == "daemon error: run_not_found" || strings.HasPrefix(s, "daemon error: run not found:")
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "daemon error: timeout"
}

func isAmbiguousRefError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "ambiguous short id") ||
		strings.Contains(s, "ambiguous run ref") ||
		strings.Contains(s, "ambiguous_short_id") ||
		strings.Contains(s, "ambiguous_run_ref")
}

func mapAmbiguousRefError(err error) error {
	if isAmbiguousRefError(err) {
		return ErrAmbiguousRef
	}
	return err
}

func issueFromDaemon(iss *daemon.IssueFull) *Issue {
	if iss == nil {
		return nil
	}
	return &Issue{
		ID:          model.IssueID(iss.ID),
		Title:       iss.Title,
		Topic:       iss.Topic,
		Summary:     iss.Summary,
		Status:      IssueStatus(iss.Status),
		Tags:        iss.Tags,
		Body:        iss.Body,
		BaseBranch:  iss.BaseBranch,
		Frontmatter: iss.Frontmatter,
	}
}

func issueSummaryToIssue(iss *daemon.IssueSummary) *Issue {
	if iss == nil {
		return nil
	}
	var modTime time.Time
	if iss.ModifiedAt != "" {
		modTime, _ = time.Parse(time.RFC3339, iss.ModifiedAt)
	}
	return &Issue{
		ID:         model.IssueID(iss.ID),
		Title:      iss.Title,
		Topic:      iss.Topic,
		Summary:    iss.Summary,
		Status:     IssueStatus(iss.Status),
		Tags:       iss.Tags,
		ModifiedAt: modTime,
	}
}

func runFromDaemonFull(r *daemon.RunFull) *Run {
	if r == nil {
		return nil
	}
	var startedAt, updatedAt time.Time
	if r.StartedAt != "" {
		startedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
	}
	if r.UpdatedAt != "" {
		updatedAt, _ = time.Parse(time.RFC3339, r.UpdatedAt)
	}
	var diffStats *DiffStats
	if r.DiffStats != nil {
		diffStats = &DiffStats{
			Additions:    r.DiffStats.Additions,
			Deletions:    r.DiffStats.Deletions,
			FilesChanged: r.DiffStats.FilesChanged,
			Files:        r.DiffStats.Files,
		}
	}
	events := make([]*Event, len(r.Events))
	for i, e := range r.Events {
		var ts time.Time
		if e.Timestamp != "" {
			ts, _ = time.Parse(time.RFC3339, e.Timestamp)
		}
		events[i] = &Event{
			Timestamp: ts,
			Type:      e.Type,
			Name:      e.Name,
			Attrs:     e.Attrs,
		}
	}
	return &Run{
		IssueID:           model.IssueID(r.IssueID),
		RunID:             model.RunID(r.RunID),
		ShortID:           model.ShortID(r.ShortID),
		IssueStatus:       r.IssueStatus,
		IssueTopic:        r.IssueTopic,
		Status:            RunStatus(r.Status),
		IsActive:          r.IsActive,
		IsTerminal:        r.IsTerminal,
		Phase:             r.Phase,
		Agent:             r.Agent,
		Profile:           r.Profile,
		Model:             r.Model,
		ModelVariant:      r.ModelVariant,
		Branch:            r.Branch,
		WorktreePath:      r.WorktreePath,
		Target:            r.Target,
		TargetHost:        r.TargetHost,
		SessionName:       r.SessionName,
		Multiplexer:       Multiplexer(r.Multiplexer),
		PRUrl:             r.PRUrl,
		ServerPort:        r.ServerPort,
		OpenCodeSessionID: r.OpenCodeSessionID,
		ContinuedFrom:     r.ContinuedFrom,
		DiffStats:         diffStats,
		BranchState:       BranchState(r.BranchState),
		ElapsedSeconds:    r.ElapsedSeconds,
		ElapsedDisplay:    r.ElapsedDisplay,
		Alive:             r.Alive,
		AliveKnown:        r.AliveKnown,
		WorktreeExists:    r.WorktreeExists,
		StartedAt:         startedAt,
		UpdatedAt:         updatedAt,
		Events:            events,
	}
}

func runFromDaemonSummary(r *daemon.RunSummary) *Run {
	if r == nil {
		return nil
	}
	var startedAt, updatedAt time.Time
	if r.StartedAt != "" {
		startedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
	}
	if r.UpdatedAt != "" {
		updatedAt, _ = time.Parse(time.RFC3339, r.UpdatedAt)
	}
	var diffStats *DiffStats
	if r.DiffStats != nil {
		diffStats = &DiffStats{
			Additions:    r.DiffStats.Additions,
			Deletions:    r.DiffStats.Deletions,
			FilesChanged: r.DiffStats.FilesChanged,
		}
	}
	return &Run{
		IssueID:           model.IssueID(r.IssueID),
		RunID:             model.RunID(r.RunID),
		ShortID:           model.ShortID(r.ShortID),
		IssueStatus:       r.IssueStatus,
		IssueTopic:        r.IssueTopic,
		Status:            RunStatus(r.Status),
		IsActive:          r.IsActive,
		IsTerminal:        r.IsTerminal,
		Phase:             r.Phase,
		Agent:             r.Agent,
		Profile:           r.Profile,
		Model:             r.Model,
		Branch:            r.Branch,
		WorktreePath:      r.WorktreePath,
		Target:            r.Target,
		TargetHost:        r.TargetHost,
		SessionName:       r.SessionName,
		Multiplexer:       Multiplexer(r.Multiplexer),
		PRUrl:             r.PRUrl,
		ServerPort:        r.ServerPort,
		OpenCodeSessionID: r.OpenCodeSessionID,
		DiffStats:         diffStats,
		BranchState:       BranchState(r.BranchState),
		ElapsedSeconds:    r.ElapsedSeconds,
		ElapsedDisplay:    r.ElapsedDisplay,
		Alive:             r.Alive,
		AliveKnown:        r.AliveKnown,
		WorktreeExists:    r.WorktreeExists,
		StartedAt:         startedAt,
		UpdatedAt:         updatedAt,
	}
}

func (c *DaemonClient) DeleteRun(ctx context.Context, ref RunRef, opts *DeleteRunOptions) (*DeleteRunResult, error) {
	if opts == nil {
		opts = &DeleteRunOptions{}
	}
	resp, err := c.proto.DeleteRun(string(ref.IssueID), string(ref.RunID), string(ref.ShortID), opts.WithWorktree, opts.WithBranch, opts.Force)
	if err != nil {
		return nil, err
	}
	return &DeleteRunResult{
		IssueID:         model.IssueID(resp.IssueID),
		RunID:           model.RunID(resp.RunID),
		ShortID:         model.ShortID(resp.ShortID),
		WorktreeRemoved: resp.WorktreeRemoved,
		BranchRemoved:   resp.BranchRemoved,
		SessionKilled:   resp.SessionKilled,
	}, nil
}

func (c *DaemonClient) CleanRunWorktree(ctx context.Context, ref RunRef) (*CleanRunWorktreeResult, error) {
	resp, err := c.proto.CleanRunWorktree(string(ref.IssueID), string(ref.RunID), string(ref.ShortID))
	if err != nil {
		return nil, err
	}
	return &CleanRunWorktreeResult{
		IssueID:         model.IssueID(resp.IssueID),
		RunID:           model.RunID(resp.RunID),
		ShortID:         model.ShortID(resp.ShortID),
		WorktreePath:    resp.WorktreePath,
		WorktreeRemoved: resp.WorktreeRemoved,
		Skipped:         resp.Skipped,
		Reason:          resp.Reason,
	}, nil
}

func (c *DaemonClient) UpdateIssue(ctx context.Context, issueID model.IssueID, req *UpdateIssueRequest) (*Issue, error) {
	resp, err := c.proto.UpdateIssue(string(issueID), req.Title, req.Summary, req.Body, string(req.Status))
	if err != nil {
		return nil, err
	}
	return issueFromDaemon(resp), nil
}

func (c *DaemonClient) ValidateIssueFiles(ctx context.Context, issueID model.IssueID) (*ValidateIssueFilesResult, error) {
	resp, err := c.proto.ValidateIssueFiles(string(issueID))
	if err != nil {
		return nil, err
	}
	result := &ValidateIssueFilesResult{
		Total: resp.Total,
		Valid: resp.Valid,
	}
	for _, e := range resp.Errors {
		vr := &ValidationResult{
			File:    e.File,
			IssueID: model.IssueID(e.IssueID),
		}
		for _, issue := range e.Errors {
			vr.Errors = append(vr.Errors, ValidationIssue{
				Code:    issue.Code,
				Message: issue.Message,
				Line:    issue.Line,
				Level:   issue.Level,
			})
		}
		for _, issue := range e.Warnings {
			vr.Warnings = append(vr.Warnings, ValidationIssue{
				Code:    issue.Code,
				Message: issue.Message,
				Line:    issue.Line,
				Level:   issue.Level,
			})
		}
		result.Errors = append(result.Errors, vr)
	}
	for _, w := range resp.Warnings {
		vr := &ValidationResult{
			File:    w.File,
			IssueID: model.IssueID(w.IssueID),
		}
		for _, issue := range w.Warnings {
			vr.Warnings = append(vr.Warnings, ValidationIssue{
				Code:    issue.Code,
				Message: issue.Message,
				Line:    issue.Line,
				Level:   issue.Level,
			})
		}
		result.Warnings = append(result.Warnings, vr)
	}
	for _, d := range resp.Duplicates {
		result.Duplicates = append(result.Duplicates, DuplicateID{
			ID:    model.IssueID(d.ID),
			Files: d.Files,
		})
	}
	return result, nil
}

func (c *DaemonClient) WriteAgentPrompt(ctx context.Context, ref RunRef, content string) error {
	return c.proto.WriteAgentPrompt(string(ref.IssueID), string(ref.RunID), string(ref.ShortID), content)
}

func (c *DaemonClient) ReadAgentPrompt(ctx context.Context, ref RunRef) (string, error) {
	return c.proto.ReadAgentPrompt(string(ref.IssueID), string(ref.RunID), string(ref.ShortID))
}

func (c *DaemonClient) RepairState(ctx context.Context, opts *RepairOptions) (*RepairResult, error) {
	if opts == nil {
		opts = &RepairOptions{}
	}
	resp, err := c.proto.RepairState(opts.DryRun, opts.Force)
	if err != nil {
		return nil, err
	}
	return &RepairResult{
		ProblemsFound: resp.ProblemsFound,
		ProblemsFixed: resp.ProblemsFixed,
		Details:       resp.Details,
	}, nil
}

func (c *DaemonClient) GetDaemonLog(ctx context.Context, lines int) (string, error) {
	return c.proto.GetDaemonLog(lines)
}

func (c *DaemonClient) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return c.proto.ReadFile(path)
}

func (c *DaemonClient) WriteFile(ctx context.Context, path string, content []byte, perm uint32) error {
	return c.proto.WriteFile(path, content, perm)
}

func (c *DaemonClient) CreateRun(ctx context.Context, req *CreateRunRequest) (*CreateRunResult, error) {
	resp, err := c.proto.CreateRun(string(req.IssueID), string(req.RunID), req.Metadata)
	if err != nil {
		return nil, err
	}
	return &CreateRunResult{
		IssueID: model.IssueID(resp.IssueId),
		RunID:   model.RunID(resp.RunId),
		Path:    resp.Path,
	}, nil
}

func (c *DaemonClient) GetConfig(ctx context.Context) (*Config, error) {
	resp, err := c.proto.GetConfig()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Agent:               resp.Agent,
		Model:               resp.Model,
		ModelVariant:        resp.ModelVariant,
		WorktreeDir:         resp.WorktreeDir,
		BaseBranch:          resp.BaseBranch,
		PRTargetBranch:      resp.PRTargetBranch,
		LogLevel:            resp.LogLevel,
		PromptTemplate:      resp.PromptTemplate,
		Multiplexer:         resp.Multiplexer,
		AgentMultiplexer:    resp.AgentMultiplexer,
		NoPR:                resp.NoPR,
		DefaultPreset:       resp.DefaultPreset,
		ControlAgent:        resp.ControlAgent,
		ControlModel:        resp.ControlModel,
		ControlModelVariant: resp.ControlModelVariant,
		DiffTool:            resp.DiffTool,
		PS: PSConfig{
			DefaultStatuses: resp.PS.DefaultStatuses,
		},
		OpenCode: OpenCodeConfig{
			DefaultModel:     resp.OpenCode.DefaultModel,
			DefaultVariant:   resp.OpenCode.DefaultVariant,
			PromptTemplate:   resp.OpenCode.PromptTemplate,
			ExtraArgs:        resp.OpenCode.ExtraArgs,
			ControlExtraArgs: resp.OpenCode.ControlExtraArgs,
		},
		Claude: ClaudeConfig{
			PromptTemplate:   resp.Claude.PromptTemplate,
			ExtraArgs:        resp.Claude.ExtraArgs,
			ControlExtraArgs: resp.Claude.ControlExtraArgs,
		},
		Codex: CodexConfig{
			PromptTemplate:   resp.Codex.PromptTemplate,
			ExtraArgs:        resp.Codex.ExtraArgs,
			ControlExtraArgs: resp.Codex.ControlExtraArgs,
		},
		Gemini: GeminiConfig{
			PromptTemplate:   resp.Gemini.PromptTemplate,
			ExtraArgs:        resp.Gemini.ExtraArgs,
			ControlExtraArgs: resp.Gemini.ControlExtraArgs,
		},
		Slack: SlackConfig{
			Enabled:    resp.Slack.Enabled,
			WebhookURL: resp.Slack.WebhookURL,
			BotToken:   resp.Slack.BotToken,
			Channel:    resp.Slack.Channel,
			NotifyOn:   resp.Slack.NotifyOn,
		},
		Issues: IssuesConfig{
			Backend: resp.Issues.Backend,
			Path:    resp.Issues.Path,
		},
		GitHub: GitHubConfig{
			Owner:        resp.GitHub.Owner,
			Repo:         resp.GitHub.Repo,
			LabelFilter:  resp.GitHub.LabelFilter,
			PollInterval: resp.GitHub.PollInterval,
			StatusLabels: resp.GitHub.StatusLabels,
		},
	}
	for _, p := range resp.Presets {
		cfg.Presets = append(cfg.Presets, Preset{
			Name:    p.Name,
			Backend: p.Backend,
			Model:   p.Model,
			Variant: p.Variant,
			Profile: p.Profile,
		})
	}
	return cfg, nil
}

func (c *DaemonClient) GetDaemonStatus(ctx context.Context) (*DaemonStatus, error) {
	resp, err := c.proto.GetDaemonStatus()
	if err != nil {
		return nil, err
	}
	return &DaemonStatus{
		Running: resp.Running,
		PID:     resp.PID,
		LogPath: resp.LogPath,
		Version: resp.Version,
	}, nil
}

func (c *DaemonClient) GetControlAgentConfig(ctx context.Context) (*ControlAgentConfig, error) {
	resp, err := c.proto.GetControlAgentConfig()
	if err != nil {
		return nil, err
	}
	return &ControlAgentConfig{
		PromptContent: resp.PromptContent,
		Agent:         resp.Agent,
		Model:         resp.Model,
		ModelVariant:  resp.ModelVariant,
		ExtraArgs:     resp.ExtraArgs,
		CodexHome:     resp.CodexHome,
	}, nil
}

func (c *DaemonClient) ContinueRun(ctx context.Context, req *ContinueRunRequest) (*ContinueRunResult, error) {
	resp, err := c.proto.ContinueRun(&daemon.ContinueRunOptions{
		IssueID:        req.IssueID,
		RunID:          req.RunID,
		ShortID:        req.ShortID,
		Branch:         req.Branch,
		Agent:          req.Agent,
		AgentCmd:       req.AgentCmd,
		AgentProfile:   req.AgentProfile,
		CodexProfile:   req.CodexProfile,
		WorktreeDir:    req.WorktreeDir,
		NoPR:           req.NoPR,
		PromptTemplate: req.PromptTemplate,
		PRTargetBranch: req.PRTargetBranch,
		Multiplexer:    req.Multiplexer,
		SessionName:    req.SessionName,
		NoSession:      req.NoSession,
	})
	if err != nil {
		return nil, err
	}
	return &ContinueRunResult{
		RunID:         model.RunID(resp.RunID),
		Branch:        resp.Branch,
		WorktreePath:  resp.WorktreePath,
		SessionName:   resp.SessionName,
		Status:        resp.Status,
		ContinuedFrom: resp.ContinuedFrom,
		IssueID:       model.IssueID(resp.IssueID),
	}, nil
}

var _ OrchAPI = (*DaemonClient)(nil)
