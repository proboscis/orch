package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/git"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/pr"
)

// MonitorConnection tracks a connected monitor instance
type MonitorConnection struct {
	ID          string    `json:"id"`
	PID         int       `json:"pid"`
	Type        string    `json:"type"` // "go" or "python"
	View        string    `json:"view"` // "runs", "issues", "dashboard"
	StartedAt   time.Time `json:"started_at"`
	LastSeen    time.Time `json:"last_seen"`
	Project     string    `json:"project"`
	SessionName string    `json:"session_name,omitempty"`
}

// MonitorRequest is the base request for monitor operations
type MonitorRequest struct {
	Type        string `json:"type"`
	MonitorID   string `json:"monitor_id,omitempty"`
	PID         int    `json:"pid,omitempty"`
	MonitorType string `json:"monitor_type,omitempty"` // "go" or "python"
	View        string `json:"view,omitempty"`
	Project     string `json:"project,omitempty"`
	SessionName string `json:"session_name,omitempty"`
	All         bool   `json:"all,omitempty"`
	Global      bool   `json:"global,omitempty"`
}

// RegisterMonitorResponse is the response for register_monitor
type RegisterMonitorResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	MonitorID string `json:"monitor_id,omitempty"`
}

// ListMonitorsResponse is the response for list_monitors
type ListMonitorsResponse struct {
	OK       bool                 `json:"ok"`
	Error    string               `json:"error,omitempty"`
	Monitors []*MonitorConnection `json:"monitors,omitempty"`
}

// KillMonitorResponse is the response for kill_monitor
type KillMonitorResponse struct {
	OK          bool     `json:"ok"`
	Error       string   `json:"error,omitempty"`
	KilledIDs   []string `json:"killed_ids,omitempty"`
	KilledCount int      `json:"killed_count"`
	FailedIDs   []string `json:"failed_ids,omitempty"`
	FailedCount int      `json:"failed_count"`
}

// HeartbeatResponse is the response for monitor_heartbeat
type HeartbeatResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type RegisterWorkerOptions struct {
	WorkerID   string
	WorkerType string
	Host       string
	Mode       string
}

type RegisterWorkerResponse struct {
	OK                  bool   `json:"ok"`
	Error               string `json:"error,omitempty"`
	WorkerID            string `json:"worker_id,omitempty"`
	HeartbeatTTLSeconds int64  `json:"heartbeat_ttl_seconds,omitempty"`
}

type ListWorkersResponse struct {
	OK      bool                  `json:"ok"`
	Error   string                `json:"error,omitempty"`
	Workers []*WorkerRegistration `json:"workers,omitempty"`
}

type LeaseWorkResponse struct {
	OK    bool         `json:"ok"`
	Error string       `json:"error,omitempty"`
	Lease *WorkerLease `json:"lease,omitempty"`
}

// ListRunsFilter contains optional filters for listing runs
type ListRunsFilter struct {
	IssueID   string
	Status    []string
	Limit     int
	Cursor    string
	OlderThan string
}

// ListRunsResponse is the response for list_runs
type ListRunsResponse struct {
	OK         bool          `json:"ok"`
	Error      string        `json:"error,omitempty"`
	Runs       []*RunSummary `json:"runs,omitempty"`
	NextCursor *string       `json:"next_cursor"`
	Total      int           `json:"total"`
}

// DiffStatsJSON is the JSON representation of diff statistics
type DiffStatsJSON struct {
	Additions    int      `json:"additions"`
	Deletions    int      `json:"deletions"`
	FilesChanged int      `json:"files_changed"`
	Files        []string `json:"files,omitempty"`
}

// RunSummary is a summary view of a run for list operations
type RunSummary struct {
	IssueID           string         `json:"issue_id"`
	RunID             string         `json:"run_id"`
	ShortID           string         `json:"short_id"`
	Status            string         `json:"status"`
	IsActive          bool           `json:"is_active"`
	IsTerminal        bool           `json:"is_terminal"`
	Phase             string         `json:"phase,omitempty"`
	Agent             string         `json:"agent"`
	Profile           string         `json:"profile,omitempty"`
	Model             string         `json:"model,omitempty"`
	Branch            string         `json:"branch,omitempty"`
	WorktreePath      string         `json:"worktree_path,omitempty"`
	Target            string         `json:"target,omitempty"`
	TargetHost        string         `json:"target_host,omitempty"`
	SessionName       string         `json:"session_name,omitempty"`
	Multiplexer       string         `json:"multiplexer,omitempty"`
	PRUrl             string         `json:"pr_url,omitempty"`
	PRNumber          int            `json:"pr_number,omitempty"`
	PRState           string         `json:"pr_state,omitempty"`
	ServerPort        int            `json:"server_port,omitempty"`
	OpenCodeSessionID string         `json:"opencode_session_id,omitempty"`
	AgentSessionID    string         `json:"agent_session_id,omitempty"`
	AgentSessionGen   int            `json:"agent_session_generation,omitempty"`
	IssueStatus       string         `json:"issue_status,omitempty"`
	IssueTopic        string         `json:"issue_topic,omitempty"`
	Additions         int            `json:"additions"`
	Deletions         int            `json:"deletions"`
	DiffStats         *DiffStatsJSON `json:"diff_stats,omitempty"`
	BranchState       string         `json:"branch_state,omitempty"`
	ElapsedSeconds    int            `json:"elapsed_seconds,omitempty"`
	ElapsedDisplay    string         `json:"elapsed_display,omitempty"`
	Alive             bool           `json:"alive"`
	AliveKnown        bool           `json:"alive_known"`
	WorktreeExists    bool           `json:"worktree_exists"`
	StartedAt         string         `json:"started_at"`
	UpdatedAt         string         `json:"updated_at"`
	URI               string         `json:"uri"`
}

// GetRunResponse is the response for get_run
type GetRunResponse struct {
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Run   *RunFull `json:"run,omitempty"`
}

type WaitForRunsResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	RunID  string `json:"run_id,omitempty"`
	Status string `json:"status,omitempty"`
	Issue  string `json:"issue,omitempty"`
	PRURL  string `json:"pr_url,omitempty"`
}

// RunFull is the full view of a run including events
type RunFull struct {
	IssueID           string         `json:"issue_id"`
	RunID             string         `json:"run_id"`
	ShortID           string         `json:"short_id"`
	Status            string         `json:"status"`
	IsActive          bool           `json:"is_active"`
	IsTerminal        bool           `json:"is_terminal"`
	Phase             string         `json:"phase,omitempty"`
	Agent             string         `json:"agent"`
	Profile           string         `json:"profile,omitempty"`
	Model             string         `json:"model,omitempty"`
	ModelVariant      string         `json:"model_variant,omitempty"`
	Branch            string         `json:"branch,omitempty"`
	WorktreePath      string         `json:"worktree_path,omitempty"`
	Target            string         `json:"target,omitempty"`
	TargetHost        string         `json:"target_host,omitempty"`
	SessionName       string         `json:"session_name,omitempty"`
	Multiplexer       string         `json:"multiplexer,omitempty"`
	PRUrl             string         `json:"pr_url,omitempty"`
	PRNumber          int            `json:"pr_number,omitempty"`
	PRState           string         `json:"pr_state,omitempty"`
	ServerPort        int            `json:"server_port,omitempty"`
	OpenCodeSessionID string         `json:"opencode_session_id,omitempty"`
	AgentSessionID    string         `json:"agent_session_id,omitempty"`
	AgentSessionGen   int            `json:"agent_session_generation,omitempty"`
	IssueStatus       string         `json:"issue_status,omitempty"`
	IssueTopic        string         `json:"issue_topic,omitempty"`
	ContinuedFrom     string         `json:"continued_from,omitempty"`
	DiffStats         *DiffStatsJSON `json:"diff_stats,omitempty"`
	BranchState       string         `json:"branch_state,omitempty"`
	ElapsedSeconds    int            `json:"elapsed_seconds,omitempty"`
	ElapsedDisplay    string         `json:"elapsed_display,omitempty"`
	Alive             bool           `json:"alive"`
	AliveKnown        bool           `json:"alive_known"`
	WorktreeExists    bool           `json:"worktree_exists"`
	StartedAt         string         `json:"started_at"`
	UpdatedAt         string         `json:"updated_at"`
	URI               string         `json:"uri"`
	Events            []*EventJSON   `json:"events"`
}

// EventJSON is the JSON representation of an event
type EventJSON struct {
	Timestamp string            `json:"timestamp"`
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// ListIssuesResponse is the response for list_issues
type ListIssuesResponse struct {
	OK         bool            `json:"ok"`
	Error      string          `json:"error,omitempty"`
	Issues     []*IssueSummary `json:"issues,omitempty"`
	NextCursor *string         `json:"next_cursor"`
	Total      int             `json:"total"`
}

// IssueSummary is a summary view of an issue for list operations
type IssueSummary struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Topic      string   `json:"topic,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	Status     string   `json:"status"`
	Tags       []string `json:"tags,omitempty"`
	URI        string   `json:"uri"`
	ModifiedAt string   `json:"modified_at,omitempty"`
}

// GetIssueResponse is the response for get_issue
type GetIssueResponse struct {
	OK    bool       `json:"ok"`
	Error string     `json:"error,omitempty"`
	Issue *IssueFull `json:"issue,omitempty"`
}

// IssueFull is the full view of an issue including body
type IssueFull struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Topic       string            `json:"topic,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Status      string            `json:"status"`
	Body        string            `json:"body"`
	Tags        []string          `json:"tags,omitempty"`
	BaseBranch  string            `json:"base_branch,omitempty"`
	URI         string            `json:"uri"`
	Frontmatter map[string]string `json:"frontmatter,omitempty"`
}

// Pagination cursor structure
type cursor struct {
	Offset int `json:"offset"`
}

// EncodeCursor encodes a pagination cursor
func EncodeCursor(offset int) string {
	c := cursor{Offset: offset}
	data, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(data)
}

func DecodeCursor(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return 0, fmt.Errorf("invalid_cursor")
	}
	var c cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return 0, fmt.Errorf("invalid_cursor")
	}
	if c.Offset < 0 {
		return 0, fmt.Errorf("invalid_cursor")
	}
	return c.Offset, nil
}

func FileURI(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return "file://" + path
}

func computeElapsed(run *model.Run) (int, string) {
	if run.StartedAt.IsZero() {
		return 0, ""
	}

	var elapsed time.Duration
	if isActiveStatus(run.Status) {
		elapsed = time.Since(run.StartedAt)
	} else if !run.UpdatedAt.IsZero() {
		elapsed = run.UpdatedAt.Sub(run.StartedAt)
	} else {
		elapsed = time.Since(run.StartedAt)
	}

	seconds := int(elapsed.Seconds())
	return seconds, formatDuration(elapsed)
}

func isActiveStatus(status model.Status) bool {
	return status.IsActive()
}

func formatDuration(d time.Duration) string {
	totalSeconds := int(d.Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func computeBranchStateString(run *model.Run) string {
	if run.WorktreePath == "" {
		return ""
	}

	status := git.GetWorktreeStatus(run.WorktreePath, run.Branch, "main")
	switch status.State {
	case git.BranchStateDirty:
		return "dirty"
	case git.BranchStateMerged:
		return "merged"
	case git.BranchStateClean:
		return "clean"
	case git.BranchStateAhead:
		return "ahead"
	case git.BranchStateBehind:
		return "behind"
	case git.BranchStateDiverged:
		return "diverged"
	case git.BranchStateConflict:
		return "conflict"
	case git.BranchStateSynced:
		return "synced"
	default:
		return ""
	}
}

func computeWorktreeExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// RunToSummary converts a model.Run to a RunSummary
func RunToSummary(run *model.Run) (*RunSummary, error) {
	diffStats, err := git.GetDiffStats(run.WorktreePath, run.Branch, "main")
	if err != nil {
		return nil, err
	}

	elapsedSeconds, elapsedDisplay := computeElapsed(run)
	branchState := computeBranchStateString(run)
	prNumber, prState := lookupPRInfo(run)

	return &RunSummary{
		IssueID:           string(run.IssueID),
		RunID:             string(run.RunID),
		ShortID:           string(run.ShortID()),
		Status:            string(run.Status),
		IsActive:          run.Status.IsActive(),
		IsTerminal:        run.Status.IsTerminal(),
		Phase:             string(run.Phase),
		Agent:             run.Agent,
		Profile:           run.Profile,
		Model:             run.Model,
		Branch:            run.Branch,
		WorktreePath:      run.WorktreePath,
		Target:            run.Target,
		TargetHost:        run.TargetHost,
		SessionName:       run.SessionName,
		Multiplexer:       run.Multiplexer,
		PRUrl:             run.PRUrl,
		PRNumber:          prNumber,
		PRState:           prState,
		ServerPort:        run.ServerPort,
		OpenCodeSessionID: run.OpenCodeSessionID,
		AgentSessionID:    run.AgentSessionID,
		AgentSessionGen:   run.AgentSessionGeneration,
		Additions:         diffStats.Additions,
		Deletions:         diffStats.Deletions,
		DiffStats: &DiffStatsJSON{
			Additions:    diffStats.Additions,
			Deletions:    diffStats.Deletions,
			FilesChanged: diffStats.FilesChanged,
		},
		BranchState:    branchState,
		ElapsedSeconds: elapsedSeconds,
		ElapsedDisplay: elapsedDisplay,
		Alive:          false,
		AliveKnown:     false,
		WorktreeExists: computeWorktreeExists(run.WorktreePath),
		StartedAt:      formatTime(run.StartedAt),
		UpdatedAt:      formatTime(run.UpdatedAt),
		URI:            FileURI(run.Path),
	}, nil
}

// RunToSummaryWithAlive converts a model.Run to a RunSummary with alive status computed.
// This function requires the agent package and should be called when alive status is needed.
func RunToSummaryWithAlive(run *model.Run, computeAlive func(*model.Run) bool) (*RunSummary, error) {
	summary, err := RunToSummary(run)
	if err != nil {
		return nil, err
	}
	if computeAlive != nil {
		summary.Alive = computeAlive(run)
		summary.AliveKnown = true
	}
	return summary, nil
}

func RunToFull(run *model.Run) (*RunFull, error) {
	events := make([]*EventJSON, len(run.Events))
	for i, e := range run.Events {
		events[i] = &EventJSON{
			Timestamp: e.Timestamp.Format(time.RFC3339),
			Type:      string(e.Type),
			Name:      e.Name,
			Attrs:     e.Attrs,
		}
	}

	diffStats, err := git.GetDiffStats(run.WorktreePath, run.Branch, "main")
	if err != nil {
		return nil, err
	}
	elapsedSeconds, elapsedDisplay := computeElapsed(run)
	branchState := computeBranchStateString(run)
	prNumber, prState := lookupPRInfo(run)

	return &RunFull{
		IssueID:           string(run.IssueID),
		RunID:             string(run.RunID),
		ShortID:           string(run.ShortID()),
		Status:            string(run.Status),
		IsActive:          run.Status.IsActive(),
		IsTerminal:        run.Status.IsTerminal(),
		Phase:             string(run.Phase),
		Agent:             run.Agent,
		Profile:           run.Profile,
		Model:             run.Model,
		ModelVariant:      run.ModelVariant,
		Branch:            run.Branch,
		WorktreePath:      run.WorktreePath,
		Target:            run.Target,
		TargetHost:        run.TargetHost,
		SessionName:       run.SessionName,
		Multiplexer:       run.Multiplexer,
		PRUrl:             run.PRUrl,
		PRNumber:          prNumber,
		PRState:           prState,
		ServerPort:        run.ServerPort,
		OpenCodeSessionID: run.OpenCodeSessionID,
		AgentSessionID:    run.AgentSessionID,
		AgentSessionGen:   run.AgentSessionGeneration,
		ContinuedFrom:     run.ContinuedFrom,
		DiffStats: &DiffStatsJSON{
			Additions:    diffStats.Additions,
			Deletions:    diffStats.Deletions,
			FilesChanged: diffStats.FilesChanged,
			Files:        diffStats.Files,
		},
		BranchState:    branchState,
		ElapsedSeconds: elapsedSeconds,
		ElapsedDisplay: elapsedDisplay,
		Alive:          false,
		AliveKnown:     false,
		WorktreeExists: computeWorktreeExists(run.WorktreePath),
		StartedAt:      formatTime(run.StartedAt),
		UpdatedAt:      formatTime(run.UpdatedAt),
		URI:            FileURI(run.Path),
		Events:         events,
	}, nil
}

// RunToFullWithAlive converts a model.Run to a RunFull with alive status computed.
func RunToFullWithAlive(run *model.Run, computeAlive func(*model.Run) bool) (*RunFull, error) {
	full, err := RunToFull(run)
	if err != nil {
		return nil, err
	}
	if computeAlive != nil {
		full.Alive = computeAlive(run)
		full.AliveKnown = true
	}
	return full, nil
}

// IssueToSummary converts a model.Issue to an IssueSummary
func IssueToSummary(issue *model.Issue) *IssueSummary {
	return &IssueSummary{
		ID:         string(issue.ID),
		Title:      issue.Title,
		Topic:      issue.Topic,
		Summary:    issue.Summary,
		Status:     string(issue.Status),
		Tags:       issue.Tags,
		URI:        FileURI(issue.Path),
		ModifiedAt: formatTime(issue.ModifiedAt),
	}
}

// IssueToFull converts a model.Issue to an IssueFull
func IssueToFull(issue *model.Issue) *IssueFull {
	return &IssueFull{
		ID:          string(issue.ID),
		Title:       issue.Title,
		Topic:       issue.Topic,
		Summary:     issue.Summary,
		Status:      string(issue.Status),
		Body:        issue.Body,
		Tags:        issue.Tags,
		URI:         FileURI(issue.Path),
		Frontmatter: issue.Frontmatter,
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// GetOpenCodeServerRequest is the request for get_opencode_server
type GetOpenCodeServerRequest struct {
	Type        string `json:"type"`
	ProjectRoot string `json:"project_root"`
}

// GetOpenCodeServerResponse is the response for get_opencode_server
type GetOpenCodeServerResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Port      int    `json:"port"`
	SessionID string `json:"session_id,omitempty"`
	Healthy   bool   `json:"healthy"`
}

// OpenCodeServerInfo tracks a managed opencode server
type OpenCodeServerInfo struct {
	ProjectRoot string    `json:"project_root"`
	Port        int       `json:"port"`
	PID         int       `json:"pid"`
	StartTime   time.Time `json:"start_time"`
	LastHealthy time.Time `json:"last_healthy"`
}

// SummaryToRun converts a RunSummary back to a model.Run
// This is used by orch ps to convert daemon API responses to model.Run for display
func SummaryToRun(s *RunSummary) (*model.Run, error) {
	if s == nil {
		return nil, nil
	}

	startedAt, _ := time.Parse(time.RFC3339, s.StartedAt)
	updatedAt, _ := time.Parse(time.RFC3339, s.UpdatedAt)
	status, err := model.NormalizeStatus(s.Status)
	if err != nil {
		return nil, fmt.Errorf("invalid run summary status for %s#%s: %w", s.IssueID, s.RunID, err)
	}

	return &model.Run{
		IssueID:      model.IssueID(s.IssueID),
		RunID:        model.RunID(s.RunID),
		Status:       status,
		Phase:        model.Phase(s.Phase),
		Agent:        s.Agent,
		Profile:      s.Profile,
		Model:        s.Model,
		Branch:       s.Branch,
		WorktreePath: s.WorktreePath,
		SessionName:  s.SessionName,
		Multiplexer:  s.Multiplexer,
		PRUrl:        s.PRUrl,
		StartedAt:    startedAt,
		UpdatedAt:    updatedAt,
	}, nil
}

// SummaryAliveInfo extracts alive info from a RunSummary
func SummaryAliveInfo(s *RunSummary) (alive bool, known bool) {
	if s == nil {
		return false, false
	}
	return s.Alive, s.AliveKnown
}

type StartRunOptions struct {
	IssueID model.IssueID
	// IssueSnapshot is the issue resolved by the MASTER (the issue-store SSOT) and
	// carried in the worker payload so the worker never reads its own issue from a
	// local store it may not have (e.g. a worker pinned to a different host than the
	// master). The worker-delegation path in handleProtoStartRun always sets this.
	IssueSnapshot  *model.Issue
	RunID          model.RunID
	Agent          string
	AgentCmd       string
	AgentProfile   string
	CodexProfile   string
	Model          string
	ModelVariant   string
	Preset         string
	BaseBranch     string
	Branch         string
	WorktreeDir    string
	NoPR           bool
	PromptTemplate string
	PRTargetBranch string
	DryRun         bool
	Reuse          bool
	Multiplexer    string
	Target         string
	TargetHost     string
	TargetWorkerID string
	// NoSession skips multiplexer session creation and prompt injection: the
	// run record, worktree, and branch are prepared but no agent is launched
	// (CLI --tmux=false).
	NoSession bool
	// CodexHome is the CODEX_HOME for the selected codex profile, verbatim as
	// configured (~ expands on the execution host at launch). Empty means use
	// the agent default (~/.codex).
	CodexHome string
	// ClaudeConfigDir is the CLAUDE_CONFIG_DIR for the selected claude
	// profile, verbatim as configured (~ expands on the execution host at
	// launch). Empty means use the agent default (~/.claude).
	ClaudeConfigDir string
}

type StartRunResponse struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	IssueID      string `json:"issue_id,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	ShortID      string `json:"short_id,omitempty"`
	Branch       string `json:"branch,omitempty"`
	WorktreePath string `json:"worktree,omitempty"`
	SessionName  string `json:"session_name,omitempty"`
	Status       string `json:"status,omitempty"`
}

// AgentSessionResult carries the ADR-0005 R1 agent-native session identity
// fact (or its recorded miss) from the execution host's launch ladder back
// to the master projection, the same way OpenCodeSessionID travels.
type AgentSessionResult struct {
	Backend    string
	ID         string
	Generation int
	// Unresolved is the agent_session_unresolved message already recorded as
	// an error artifact on the execution host; the master mirrors it so the
	// miss is loud on the store of record too. Empty when ID is set.
	Unresolved string
}

// StartRunResult holds the success data from a start_run operation (no OK/Error).
type StartRunResult struct {
	RunID             model.RunID
	Branch            string
	WorktreePath      string
	SessionName       string
	Status            string
	Multiplexer       string
	SessionHost       string
	WorkerID          string
	ServerPort        int
	OpenCodeSessionID string
	AgentSession      *AgentSessionResult
}

type ContinueRunOptions struct {
	IssueID model.IssueID
	// IssueSnapshot is the issue resolved by the MASTER (the issue-store SSOT) and
	// carried in the worker payload so the worker never reads its own issue from a
	// local store it may not have (e.g. a worker pinned to a different host than the
	// master). The worker-delegation path in handleProtoContinueRun always sets this.
	IssueSnapshot  *model.Issue
	RunSnapshot    *RunSnapshot
	RunID          model.RunID
	ShortID        model.ShortID
	Branch         string
	Agent          string
	AgentCmd       string
	AgentProfile   string
	CodexProfile   string
	Model          string
	ModelVariant   string
	WorktreeDir    string
	NoPR           bool
	PromptTemplate string
	PRTargetBranch string
	Multiplexer    string
	SessionName    string
	Target         string
	TargetHost     string
	TargetWorkerID string
	// NoSession skips multiplexer session creation and prompt injection: the
	// run record, worktree, and branch are prepared but no agent is launched
	// (CLI --tmux=false).
	NoSession bool
	// CodexHome is the CODEX_HOME for the selected codex profile, verbatim as
	// configured (~ expands on the execution host at launch). Empty means use
	// the agent default (~/.codex).
	CodexHome string
	// ClaudeConfigDir is the CLAUDE_CONFIG_DIR for the selected claude
	// profile, verbatim as configured (~ expands on the execution host at
	// launch). Empty means use the agent default (~/.claude).
	ClaudeConfigDir string
}

type ContinueRunResponse struct {
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	Branch        string `json:"branch,omitempty"`
	WorktreePath  string `json:"worktree,omitempty"`
	SessionName   string `json:"session_name,omitempty"`
	Status        string `json:"status,omitempty"`
	ContinuedFrom string `json:"continued_from,omitempty"`
	IssueID       string `json:"issue_id,omitempty"`
}

// ContinueRunResult holds the success data from a continue_run operation (no OK/Error).
type ContinueRunResult struct {
	RunID             model.RunID
	Branch            string
	WorktreePath      string
	SessionName       string
	Status            string
	ContinuedFrom     string
	IssueID           model.IssueID
	Multiplexer       string
	SessionHost       string
	WorkerID          string
	ServerPort        int
	OpenCodeSessionID string
	AgentSession      *AgentSessionResult
}

type RunSnapshot struct {
	IssueID                model.IssueID
	RunID                  model.RunID
	Status                 model.Status
	Phase                  model.Phase
	Agent                  string
	Profile                string
	Model                  string
	ModelVariant           string
	Branch                 string
	WorktreePath           string
	Target                 string
	TargetHost             string
	TargetWorkerID         string
	SessionName            string
	Multiplexer            string
	ServerPort             int
	OpenCodeSessionID      string
	ContinuedFrom          string
	AgentSessionID         string
	AgentSessionGeneration int
}

type StopRunResponse struct {
	OK           bool     `json:"ok"`
	Error        string   `json:"error,omitempty"`
	StoppedRuns  []string `json:"stopped_runs,omitempty"`
	StoppedCount int      `json:"stopped_count"`
}

type ResolveRunResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type ResolveIssueResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	IssueID string `json:"issue_id,omitempty"`
}

type AppendEventResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type CreateIssueResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	IssueID string `json:"issue_id,omitempty"`
	Path    string `json:"path,omitempty"`
}

type CreateIssueResult struct {
	IssueID string
	Path    string
}

type CloseIssueResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	IssueID string `json:"issue_id,omitempty"`
}

type GetAttachInfoResponse struct {
	OK                bool   `json:"ok"`
	Error             string `json:"error,omitempty"`
	IssueID           string `json:"issue_id,omitempty"`
	RunID             string `json:"run_id,omitempty"`
	Agent             string `json:"agent,omitempty"`
	SessionName       string `json:"session_name,omitempty"`
	Multiplexer       string `json:"multiplexer,omitempty"`
	WorktreePath      string `json:"worktree_path,omitempty"`
	ServerPort        int    `json:"server_port,omitempty"`
	OpenCodeSessionID string `json:"opencode_session_id,omitempty"`
	Branch            string `json:"branch,omitempty"`
	TargetHost        string `json:"target_host,omitempty"`
}

type GetControlAgentLaunchResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Command    string `json:"command,omitempty"`
	PromptFile string `json:"prompt_file,omitempty"`
	Port       int    `json:"port,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Agent      string `json:"agent,omitempty"`
}

type ControlAgentLaunchParams struct {
	ProjectRoot string
	Agent       string
	NewSession  bool
	// ClientHost is the hostname of the client that will exec the control
	// agent. Empty (old client) -> the codex profile's AllowedTargets are
	// enforced against the daemon host (back-compat).
	ClientHost string
}

type ControlAgentLaunchResult struct {
	Command    string
	PromptFile string
	Port       int
	SessionID  string
	Agent      string
	Resumed    bool
}

type GetControlAgentConfigResponse struct {
	OK            bool     `json:"ok"`
	Error         string   `json:"error,omitempty"`
	PromptContent string   `json:"prompt_content,omitempty"`
	Agent         string   `json:"agent,omitempty"`
	Model         string   `json:"model,omitempty"`
	ModelVariant  string   `json:"model_variant,omitempty"`
	ExtraArgs     []string `json:"extra_args,omitempty"`
	CodexHome     string   `json:"codex_home,omitempty"`
}

type ControlAgentConfigResult struct {
	PromptContent string
	Agent         string
	Model         string
	ModelVariant  string
	ExtraArgs     []string
	// CodexHome is the resolved CODEX_HOME for a codex control agent (from the
	// project's default codex profile), VERBATIM as configured: a leading ~ is
	// expanded by the CLIENT against its own HOME at use time, never by the
	// daemon. Empty means the agent default (~/.codex).
	CodexHome string
}

type SendMessageParams struct {
	IssueID        string
	RunID          string
	Message        string
	NoEnter        bool
	TargetWorkerID string
}

type CreateIssueParams struct {
	IssueID    string
	Title      string
	Body       string
	Summary    string
	Tags       []string
	BaseBranch string
}

type CaptureSessionResponse struct {
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
	Source    string `json:"source"`
}

type GetDiffStatsResponse struct {
	Additions    int      `json:"additions"`
	Deletions    int      `json:"deletions"`
	FilesChanged int      `json:"files_changed"`
	Files        []string `json:"files"`
}

func lookupPRInfo(run *model.Run) (prNumber int, prState string) {
	if run.PRUrl != "" {
		prInfo, err := pr.LookupCachedInfoByURL(run.PRUrl)
		if err == nil && prInfo != nil {
			return prInfo.Number, strings.ToLower(prInfo.State)
		}
	}

	if run.Branch == "" {
		return 0, ""
	}

	var repoRoot string
	var err error
	if run.WorktreePath != "" {
		repoRoot, err = git.FindMainRepoRoot(run.WorktreePath)
	}
	if repoRoot == "" || err != nil {
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return 0, ""
		}
	}

	prInfo, err := pr.LookupCachedInfo(repoRoot, run.Branch)
	if err != nil || prInfo == nil {
		return 0, ""
	}

	return prInfo.Number, strings.ToLower(prInfo.State)
}

type SlackConfigResponse struct {
	Enabled    bool
	WebhookURL string
	BotToken   string
	Channel    string
	NotifyOn   []string
}

type OpenCodeConfigResponse struct {
	DefaultModel     string
	DefaultVariant   string
	PromptTemplate   string
	ExtraArgs        []string
	ControlExtraArgs []string
}

type ClaudeConfigResponse struct {
	PromptTemplate   string
	ExtraArgs        []string
	ControlExtraArgs []string
}

type CodexConfigResponse struct {
	PromptTemplate   string
	ExtraArgs        []string
	ControlExtraArgs []string
}

type GeminiConfigResponse struct {
	PromptTemplate   string
	ExtraArgs        []string
	ControlExtraArgs []string
}

type PresetConfig struct {
	Name    string
	Backend string
	Model   string
	Variant string
	Profile string
}

type IssuesConfigResponse struct {
	Backend string
	Path    string
}

type GitHubConfigResponse struct {
	Owner        string
	Repo         string
	LabelFilter  string
	PollInterval int
	StatusLabels map[string]string
}

type PSConfigResponse struct {
	DefaultStatuses []string
}

type ConfigResponse struct {
	Agent               string
	Model               string
	ModelVariant        string
	WorktreeDir         string
	BaseBranch          string
	PRTargetBranch      string
	LogLevel            string
	PromptTemplate      string
	Multiplexer         string
	AgentMultiplexer    string
	NoPR                bool
	DefaultPreset       string
	ControlAgent        string
	ControlModel        string
	ControlModelVariant string
	DiffTool            string
	PS                  PSConfigResponse
	Presets             []PresetConfig
	OpenCode            OpenCodeConfigResponse
	Claude              ClaudeConfigResponse
	Codex               CodexConfigResponse
	Gemini              GeminiConfigResponse
	Slack               SlackConfigResponse
	Issues              IssuesConfigResponse
	GitHub              GitHubConfigResponse
}

type DaemonStatusResponse struct {
	Running bool
	PID     int
	LogPath string
	Version string
}

type PingResponse struct {
	OK      bool
	Version string
}
