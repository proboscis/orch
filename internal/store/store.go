package store

import (
	"github.com/s22625/orch/internal/model"
)

// ListRunsFilter specifies criteria for filtering runs
type ListRunsFilter struct {
	IssueID string
	Status  []model.Status
	Limit   int
	Since   string // ISO8601 timestamp
}

// Store defines the interface for knowledge store backends
type Store interface {
	// ResolveIssue retrieves an issue by ID (looks for type: issue frontmatter)
	ResolveIssue(issueID string) (*model.Issue, error)

	// ListIssues returns all issues in the vault
	ListIssues() ([]*model.Issue, error)

	// CreateRun creates a new run for an issue
	CreateRun(issueID, runID string, metadata map[string]string) (*model.Run, error)

	// AppendEvent appends an event to a run
	AppendEvent(ref *model.RunRef, event *model.Event) error

	// ListRuns lists runs matching the filter
	ListRuns(filter *ListRunsFilter) ([]*model.Run, error)

	// GetRun retrieves a run by reference
	GetRun(ref *model.RunRef) (*model.Run, error)

	// GetRunByShortID finds a run by its short ID prefix (2-6 hex chars)
	// Returns an error if no match found or if multiple runs match (ambiguous)
	GetRunByShortID(shortID string) (*model.Run, error)

	// GetLatestRun retrieves the latest run for an issue
	GetLatestRun(issueID string) (*model.Run, error)

	// VaultPath returns the vault root path
	VaultPath() string
}
