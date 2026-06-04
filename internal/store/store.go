package store

import (
	"github.com/s22625/orch/internal/model"
)

// ListRunsFilter specifies criteria for filtering runs
type ListRunsFilter struct {
	IssueID    model.IssueID
	Status     []model.Status
	Agent      string // Filter by agent name (e.g., "opencode", "claude")
	TextSearch string // Search in run_id, issue_id, branch
	TimeRange  string // "hour", "today", "week", "all"
	Limit      int
	Since      string // ISO8601 timestamp - keep runs NEWER than this time
	OlderThan  string // ISO8601 timestamp - keep runs OLDER than this time
}

// ListIssuesFilter specifies criteria for filtering issues
type ListIssuesFilter struct {
	Status     []model.IssueStatus
	Tags       []string // Filter by tags
	TagsMode   string   // "or" (any tag matches) or "and" (all tags must match)
	TextSearch string   // Search in id, title, summary
	Limit      int
}

// Store defines the interface for knowledge store backends
type Store interface {
	// ResolveIssue retrieves an issue by ID (looks for type: issue frontmatter)
	ResolveIssue(issueID model.IssueID) (*model.Issue, error)

	// ListIssues returns all issues in the issues root
	ListIssues() ([]*model.Issue, error)

	// SetIssueStatus updates an issue's status in frontmatter
	SetIssueStatus(issueID model.IssueID, status model.IssueStatus) error

	// CreateRun creates a new run for an issue
	CreateRun(issueID model.IssueID, runID model.RunID, metadata map[string]string) (*model.Run, error)

	// AppendEvent appends an event to a run
	AppendEvent(ref *model.RunRef, event *model.Event) error

	// ListRuns lists runs matching the filter
	ListRuns(filter *ListRunsFilter) ([]*model.Run, error)

	// GetRun retrieves a run by reference
	GetRun(ref *model.RunRef) (*model.Run, error)

	// GetRunByShortID finds a run by its short ID prefix (2-6 hex chars)
	// Returns an error if no match found or if multiple runs match (ambiguous)
	GetRunByShortID(shortID model.ShortID) (*model.Run, error)

	// GetLatestRun retrieves the latest run for an issue
	GetLatestRun(issueID model.IssueID) (*model.Run, error)

	// RootPath returns the issues root path (where issues and runs are stored)
	RootPath() string

	// DeleteRun removes a run and its associated files
	DeleteRun(ref *model.RunRef) error

	// UpdateIssue updates an existing issue
	UpdateIssue(issue *model.Issue) error

	// ValidateIssueFiles validates issue files and returns results
	ValidateIssueFiles(issueID model.IssueID) (*ValidationResult, error)

	// WriteAgentPrompt writes the agent control prompt for a run
	WriteAgentPrompt(ref *model.RunRef, content string) error

	// ReadAgentPrompt reads the agent control prompt for a run
	ReadAgentPrompt(ref *model.RunRef) (string, error)

	// CreateIssue creates a new issue
	CreateIssue(issue *model.Issue) error
}

type ValidationResult struct {
	Total      int
	Valid      int
	Errors     []*ValidationResultItem
	Duplicates []*DuplicateID
}

type ValidationResultItem struct {
	File    string
	IssueID model.IssueID
	Errors  []ValidationIssue
}

type ValidationIssue struct {
	Code    string
	Message string
	Line    int
	Level   string
}

type DuplicateID struct {
	ID    model.IssueID
	Files []string
}
