package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
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
	TmuxSession string    `json:"tmux_session,omitempty"`
}

// MonitorRequest is the base request for monitor operations
type MonitorRequest struct {
	Type        string `json:"type"`
	MonitorID   string `json:"monitor_id,omitempty"`
	PID         int    `json:"pid,omitempty"`
	MonitorType string `json:"monitor_type,omitempty"` // "go" or "python"
	View        string `json:"view,omitempty"`
	Project     string `json:"project,omitempty"`
	TmuxSession string `json:"tmux_session,omitempty"`
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
	Model             string         `json:"model,omitempty"`
	Branch            string         `json:"branch,omitempty"`
	WorktreePath      string         `json:"worktree_path,omitempty"`
	TmuxSession       string         `json:"tmux_session,omitempty"`
	Multiplexer       string         `json:"multiplexer,omitempty"`
	PRUrl             string         `json:"pr_url,omitempty"`
	ServerPort        int            `json:"server_port,omitempty"`
	OpenCodeSessionID string         `json:"opencode_session_id,omitempty"`
	Additions         int            `json:"additions"`
	Deletions         int            `json:"deletions"`
	DiffStats         *DiffStatsJSON `json:"diff_stats,omitempty"`
	BranchState       string         `json:"branch_state,omitempty"`
	ElapsedSeconds    int            `json:"elapsed_seconds,omitempty"`
	ElapsedDisplay    string         `json:"elapsed_display,omitempty"`
	Alive             bool           `json:"alive"`
	AliveKnown        bool           `json:"alive_known"`
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
	Model             string         `json:"model,omitempty"`
	ModelVariant      string         `json:"model_variant,omitempty"`
	Branch            string         `json:"branch,omitempty"`
	WorktreePath      string         `json:"worktree_path,omitempty"`
	TmuxSession       string         `json:"tmux_session,omitempty"`
	Multiplexer       string         `json:"multiplexer,omitempty"`
	PRUrl             string         `json:"pr_url,omitempty"`
	ServerPort        int            `json:"server_port,omitempty"`
	OpenCodeSessionID string         `json:"opencode_session_id,omitempty"`
	ContinuedFrom     string         `json:"continued_from,omitempty"`
	DiffStats         *DiffStatsJSON `json:"diff_stats,omitempty"`
	BranchState       string         `json:"branch_state,omitempty"`
	ElapsedSeconds    int            `json:"elapsed_seconds,omitempty"`
	ElapsedDisplay    string         `json:"elapsed_display,omitempty"`
	Alive             bool           `json:"alive"`
	AliveKnown        bool           `json:"alive_known"`
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

// RunToSummary converts a model.Run to a RunSummary
func RunToSummary(run *model.Run) *RunSummary {
	diffStats := git.GetDiffStats(run.WorktreePath, run.Branch, "main")

	elapsedSeconds, elapsedDisplay := computeElapsed(run)
	branchState := computeBranchStateString(run)

	return &RunSummary{
		IssueID:           run.IssueID,
		RunID:             run.RunID,
		ShortID:           run.ShortID(),
		Status:            string(run.Status),
		IsActive:          run.Status.IsActive(),
		IsTerminal:        run.Status.IsTerminal(),
		Phase:             string(run.Phase),
		Agent:             run.Agent,
		Model:             run.Model,
		Branch:            run.Branch,
		WorktreePath:      run.WorktreePath,
		TmuxSession:       run.TmuxSession,
		Multiplexer:       run.Multiplexer,
		PRUrl:             run.PRUrl,
		ServerPort:        run.ServerPort,
		OpenCodeSessionID: run.OpenCodeSessionID,
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
		StartedAt:      formatTime(run.StartedAt),
		UpdatedAt:      formatTime(run.UpdatedAt),
		URI:            FileURI(run.Path),
	}
}

// RunToSummaryWithAlive converts a model.Run to a RunSummary with alive status computed.
// This function requires the agent package and should be called when alive status is needed.
func RunToSummaryWithAlive(run *model.Run, computeAlive func(*model.Run) bool) *RunSummary {
	summary := RunToSummary(run)
	if computeAlive != nil {
		summary.Alive = computeAlive(run)
		summary.AliveKnown = true
	}
	return summary
}

// RunToFull converts a model.Run to a RunFull
func RunToFull(run *model.Run) *RunFull {
	events := make([]*EventJSON, len(run.Events))
	for i, e := range run.Events {
		events[i] = &EventJSON{
			Timestamp: e.Timestamp.Format(time.RFC3339),
			Type:      string(e.Type),
			Name:      e.Name,
			Attrs:     e.Attrs,
		}
	}

	diffStats := git.GetDiffStats(run.WorktreePath, run.Branch, "main")
	elapsedSeconds, elapsedDisplay := computeElapsed(run)
	branchState := computeBranchStateString(run)

	return &RunFull{
		IssueID:           run.IssueID,
		RunID:             run.RunID,
		ShortID:           run.ShortID(),
		Status:            string(run.Status),
		IsActive:          run.Status.IsActive(),
		IsTerminal:        run.Status.IsTerminal(),
		Phase:             string(run.Phase),
		Agent:             run.Agent,
		Model:             run.Model,
		ModelVariant:      run.ModelVariant,
		Branch:            run.Branch,
		WorktreePath:      run.WorktreePath,
		TmuxSession:       run.TmuxSession,
		Multiplexer:       run.Multiplexer,
		PRUrl:             run.PRUrl,
		ServerPort:        run.ServerPort,
		OpenCodeSessionID: run.OpenCodeSessionID,
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
		StartedAt:      formatTime(run.StartedAt),
		UpdatedAt:      formatTime(run.UpdatedAt),
		URI:            FileURI(run.Path),
		Events:         events,
	}
}

// RunToFullWithAlive converts a model.Run to a RunFull with alive status computed.
func RunToFullWithAlive(run *model.Run, computeAlive func(*model.Run) bool) *RunFull {
	full := RunToFull(run)
	if computeAlive != nil {
		full.Alive = computeAlive(run)
		full.AliveKnown = true
	}
	return full
}

// IssueToSummary converts a model.Issue to an IssueSummary
func IssueToSummary(issue *model.Issue) *IssueSummary {
	return &IssueSummary{
		ID:         issue.ID,
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
		ID:          issue.ID,
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
func SummaryToRun(s *RunSummary) *model.Run {
	if s == nil {
		return nil
	}

	startedAt, _ := time.Parse(time.RFC3339, s.StartedAt)
	updatedAt, _ := time.Parse(time.RFC3339, s.UpdatedAt)

	return &model.Run{
		IssueID:      s.IssueID,
		RunID:        s.RunID,
		Status:       model.Status(s.Status),
		Phase:        model.Phase(s.Phase),
		Agent:        s.Agent,
		Model:        s.Model,
		Branch:       s.Branch,
		WorktreePath: s.WorktreePath,
		TmuxSession:  s.TmuxSession,
		Multiplexer:  s.Multiplexer,
		PRUrl:        s.PRUrl,
		StartedAt:    startedAt,
		UpdatedAt:    updatedAt,
	}
}

// SummaryAliveInfo extracts alive info from a RunSummary
func SummaryAliveInfo(s *RunSummary) (alive bool, known bool) {
	if s == nil {
		return false, false
	}
	return s.Alive, s.AliveKnown
}

// StartRunResponse is the response for start_run
type StartRunResponse struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	Branch       string `json:"branch,omitempty"`
	WorktreePath string `json:"worktree,omitempty"`
	TmuxSession  string `json:"tmux_session,omitempty"`
	Status       string `json:"status,omitempty"`
}

type StopRunResponse struct {
	OK           bool     `json:"ok"`
	Error        string   `json:"error,omitempty"`
	StoppedRuns  []string `json:"stopped_runs,omitempty"`
	StoppedCount int      `json:"stopped_count"`
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
	TmuxSession       string `json:"tmux_session,omitempty"`
	Multiplexer       string `json:"multiplexer,omitempty"`
	WorktreePath      string `json:"worktree_path,omitempty"`
	ServerPort        int    `json:"server_port,omitempty"`
	OpenCodeSessionID string `json:"opencode_session_id,omitempty"`
	Branch            string `json:"branch,omitempty"`
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
