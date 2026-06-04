package monitor

import (
	"context"
	"fmt"
	"strings"

	"github.com/s22625/orch/internal/model"
)

func (m *Monitor) IssueContent(issueID string) (string, error) {
	if strings.TrimSpace(issueID) == "" {
		return "", fmt.Errorf("issue id is required")
	}
	ctx := context.Background()
	issue, err := m.api.GetIssue(ctx, model.NewIssueID(issueID))
	if err != nil {
		return "", err
	}
	if issue == nil {
		return "", fmt.Errorf("issue not found: %s", issueID)
	}
	return issue.Body, nil
}
