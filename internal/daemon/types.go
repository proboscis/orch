package daemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/s22625/orch/internal/model"
)

// ListRunsResponse is the response for list_runs
type ListRunsResponse struct {
	OK         bool          `json:"ok"`
	Error      string        `json:"error,omitempty"`
	Runs       []*RunSummary `json:"runs,omitempty"`
	NextCursor *string       `json:"next_cursor"`
	Total      int           `json:"total"`
}

// RunSummary is a summary view of a run for list operations
type RunSummary struct {
	IssueID      string `json:"issue_id"`
	RunID        string `json:"run_id"`
	ShortID      string `json:"short_id"`
	Status       string `json:"status"`
	Phase        string `json:"phase,omitempty"`
	Agent        string `json:"agent"`
	Model        string `json:"model,omitempty"`
	Branch       string `json:"branch,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	TmuxSession  string `json:"tmux_session,omitempty"`
	PRUrl        string `json:"pr_url,omitempty"`
	StartedAt    string `json:"started_at"`
	UpdatedAt    string `json:"updated_at"`
	URI          string `json:"uri"`
}

// GetRunResponse is the response for get_run
type GetRunResponse struct {
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Run   *RunFull `json:"run,omitempty"`
}

// RunFull is the full view of a run including events
type RunFull struct {
	IssueID           string       `json:"issue_id"`
	RunID             string       `json:"run_id"`
	ShortID           string       `json:"short_id"`
	Status            string       `json:"status"`
	Phase             string       `json:"phase,omitempty"`
	Agent             string       `json:"agent"`
	Model             string       `json:"model,omitempty"`
	ModelVariant      string       `json:"model_variant,omitempty"`
	Branch            string       `json:"branch,omitempty"`
	WorktreePath      string       `json:"worktree_path,omitempty"`
	TmuxSession       string       `json:"tmux_session,omitempty"`
	PRUrl             string       `json:"pr_url,omitempty"`
	ServerPort        int          `json:"server_port,omitempty"`
	OpenCodeSessionID string       `json:"opencode_session_id,omitempty"`
	ContinuedFrom     string       `json:"continued_from,omitempty"`
	StartedAt         string       `json:"started_at"`
	UpdatedAt         string       `json:"updated_at"`
	URI               string       `json:"uri"`
	Events            []*EventJSON `json:"events"`
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
	ID      string `json:"id"`
	Title   string `json:"title"`
	Topic   string `json:"topic,omitempty"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status"`
	URI     string `json:"uri"`
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
	return "file://" + path
}

// RunToSummary converts a model.Run to a RunSummary
func RunToSummary(run *model.Run) *RunSummary {
	return &RunSummary{
		IssueID:      run.IssueID,
		RunID:        run.RunID,
		ShortID:      run.ShortID(),
		Status:       string(run.Status),
		Phase:        string(run.Phase),
		Agent:        run.Agent,
		Model:        run.Model,
		Branch:       run.Branch,
		WorktreePath: run.WorktreePath,
		TmuxSession:  run.TmuxSession,
		PRUrl:        run.PRUrl,
		StartedAt:    formatTime(run.StartedAt),
		UpdatedAt:    formatTime(run.UpdatedAt),
		URI:          FileURI(run.Path),
	}
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

	return &RunFull{
		IssueID:           run.IssueID,
		RunID:             run.RunID,
		ShortID:           run.ShortID(),
		Status:            string(run.Status),
		Phase:             string(run.Phase),
		Agent:             run.Agent,
		Model:             run.Model,
		ModelVariant:      run.ModelVariant,
		Branch:            run.Branch,
		WorktreePath:      run.WorktreePath,
		TmuxSession:       run.TmuxSession,
		PRUrl:             run.PRUrl,
		ServerPort:        run.ServerPort,
		OpenCodeSessionID: run.OpenCodeSessionID,
		ContinuedFrom:     run.ContinuedFrom,
		StartedAt:         formatTime(run.StartedAt),
		UpdatedAt:         formatTime(run.UpdatedAt),
		URI:               FileURI(run.Path),
		Events:            events,
	}
}

// IssueToSummary converts a model.Issue to an IssueSummary
func IssueToSummary(issue *model.Issue) *IssueSummary {
	return &IssueSummary{
		ID:      issue.ID,
		Title:   issue.Title,
		Topic:   issue.Topic,
		Summary: issue.Summary,
		Status:  string(issue.Status),
		URI:     FileURI(issue.Path),
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
		URI:         FileURI(issue.Path),
		Frontmatter: issue.Frontmatter,
	}
}

// formatTime formats a time value, returning empty string for zero time
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// SummaryToRun converts a RunSummary to a model.Run for display purposes
func SummaryToRun(summary *RunSummary) (*model.Run, error) {
	startedAt, err := time.Parse(time.RFC3339, summary.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid started_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339, summary.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid updated_at: %w", err)
	}

	return &model.Run{
		IssueID:      summary.IssueID,
		RunID:        summary.RunID,
		Status:       model.Status(summary.Status),
		Phase:        model.Phase(summary.Phase),
		Agent:        summary.Agent,
		Model:        summary.Model,
		Branch:       summary.Branch,
		WorktreePath: summary.WorktreePath,
		TmuxSession:  summary.TmuxSession,
		PRUrl:        summary.PRUrl,
		StartedAt:    startedAt,
		UpdatedAt:    updatedAt,
		Events:       []*model.Event{},
	}, nil
}

// IssueSummaryToModel converts an IssueSummary to a model.Issue
func IssueSummaryToModel(summary *IssueSummary) *model.Issue {
	return &model.Issue{
		ID:      summary.ID,
		Title:   summary.Title,
		Topic:   summary.Topic,
		Summary: summary.Summary,
		Status:  model.IssueStatus(summary.Status),
		Path:    extractPathFromURI(summary.URI),
	}
}

func extractPathFromURI(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return strings.TrimPrefix(uri, "file://")
	}
	return uri
}

type StopRunRequest struct {
	IssueID string `json:"issue_id"`
	RunID   string `json:"run_id"`
	All     bool   `json:"all,omitempty"`
	Force   bool   `json:"force,omitempty"`
}

type StopRunResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Stopped int    `json:"stopped,omitempty"`
}

type ResolveIssueRequest struct {
	IssueID string `json:"issue_id"`
	Force   bool   `json:"force,omitempty"`
}

type ResolveIssueResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type CreateIssueRequest struct {
	IssueID string `json:"issue_id"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	Body    string `json:"body,omitempty"`
}

type CreateIssueResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	IssueID string `json:"issue_id,omitempty"`
	Path    string `json:"path,omitempty"`
}

type AttachInfoRequest struct {
	IssueID string `json:"issue_id"`
	RunID   string `json:"run_id"`
}

type AttachInfoResponse struct {
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
	Agent         string `json:"agent,omitempty"`
	TmuxSession   string `json:"tmux_session,omitempty"`
	ServerPort    int    `json:"server_port,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	WorktreePath  string `json:"worktree_path,omitempty"`
	SessionExists bool   `json:"session_exists"`
	CanAutoCreate bool   `json:"can_auto_create"`
}

// FullToRun converts a RunFull to a model.Run
func FullToRun(full *RunFull) (*model.Run, error) {
	startedAt, err := time.Parse(time.RFC3339, full.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid started_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339, full.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid updated_at: %w", err)
	}

	events := make([]*model.Event, len(full.Events))
	for i, e := range full.Events {
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("invalid event timestamp: %w", err)
		}
		events[i] = &model.Event{
			Timestamp: ts,
			Type:      model.EventType(e.Type),
			Name:      e.Name,
			Attrs:     e.Attrs,
		}
	}

	return &model.Run{
		IssueID:           full.IssueID,
		RunID:             full.RunID,
		Path:              extractPathFromURI(full.URI),
		Status:            model.Status(full.Status),
		Phase:             model.Phase(full.Phase),
		Agent:             full.Agent,
		Model:             full.Model,
		ModelVariant:      full.ModelVariant,
		Branch:            full.Branch,
		WorktreePath:      full.WorktreePath,
		TmuxSession:       full.TmuxSession,
		PRUrl:             full.PRUrl,
		ServerPort:        full.ServerPort,
		OpenCodeSessionID: full.OpenCodeSessionID,
		ContinuedFrom:     full.ContinuedFrom,
		StartedAt:         startedAt,
		UpdatedAt:         updatedAt,
		Events:            events,
	}, nil
}
