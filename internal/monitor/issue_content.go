package monitor

import (
	"context"
	"fmt"
	"strings"
)

func (m *Monitor) IssueContent(issueID string) (string, error) {
	if strings.TrimSpace(issueID) == "" {
		return "", fmt.Errorf("issue id is required")
	}
	ctx := context.Background()
	issue, err := m.api.GetIssue(ctx, issueID)
	if err != nil {
		return "", err
	}
	if issue == nil {
		return "", fmt.Errorf("issue not found: %s", issueID)
	}
	return issue.Body, nil
}
