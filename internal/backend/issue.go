package backend

import (
	"context"

	"github.com/proboscis/orch/internal/model"
)

type ListIssuesFilter struct {
	Status     []model.IssueStatus
	Tags       []string
	TagsMode   string // "or" (any tag matches) or "and" (all tags must match)
	TextSearch string
	Limit      int
}

type IssueBackend interface {
	GetIssue(ctx context.Context, issueID model.IssueID) (*model.Issue, error)
	ListIssues(ctx context.Context, filter *ListIssuesFilter) ([]*model.Issue, error)
	CreateIssue(ctx context.Context, issue *model.Issue) (*model.Issue, error)
	SetIssueStatus(ctx context.Context, issueID model.IssueID, status model.IssueStatus) error

	SupportsCreate() bool
	SupportsPR() bool

	RootPath() string
}
