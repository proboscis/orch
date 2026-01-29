package backend

import (
	"context"

	"github.com/s22625/orch/internal/model"
)

type ListRunsFilter struct {
	IssueID    string
	Status     []model.Status
	Agent      string
	TextSearch string
	TimeRange  string
	Limit      int
	Since      string
}

type RunStore interface {
	GetRun(ctx context.Context, ref *model.RunRef) (*model.Run, error)
	GetRunByShortID(ctx context.Context, shortID string) (*model.Run, error)
	GetLatestRun(ctx context.Context, issueID string) (*model.Run, error)
	ListRuns(ctx context.Context, filter *ListRunsFilter) ([]*model.Run, error)
	CreateRun(ctx context.Context, issueID, runID string, metadata map[string]string) (*model.Run, error)
	AppendEvent(ctx context.Context, ref *model.RunRef, event *model.Event) error

	RootPath() string
}
