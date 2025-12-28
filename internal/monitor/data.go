package monitor

import (
	"time"

	"github.com/s22625/orch/internal/model"
)

// RunRow holds display data for a run.
type RunRow struct {
	Index       int
	ShortID     string
	IssueID     string
	IssueStatus string
	Agent       string
	Status      model.Status
	Alive       string
	Branch      string
	Worktree    string
	PR          string // PR display string (e.g., "#123" or "-")
	PRState     string // PR state: open, merged, closed, or empty
	Merged      string
	Updated     time.Time
	Topic       string
	Run         *model.Run
}

// IssueRow holds display data for an issue.
type IssueRow struct {
	Index         int
	ID            string
	Status        string
	Summary       string
	LatestRunID   string
	LatestStatus  model.Status
	LatestUpdated time.Time
	ActiveRuns    int
	Issue         *model.Issue
}
