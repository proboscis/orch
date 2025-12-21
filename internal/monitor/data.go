package monitor

import (
	"time"

	"github.com/s22625/orch/internal/model"
)

// RunRow holds display data for a run.
type RunRow struct {
	Index   int
	ShortID string
	IssueID string
	Status  model.Status
	Summary string
	Updated time.Time
	Run     *model.Run
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
