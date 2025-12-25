package monitor

import (
	"fmt"
	"strings"
)

// IssueContent returns the issue body content for display in the run dashboard.
func (m *Monitor) IssueContent(issueID string) (string, error) {
	if strings.TrimSpace(issueID) == "" {
		return "", fmt.Errorf("issue id is required")
	}
	issue, err := m.store.ResolveIssue(issueID)
	if err != nil {
		return "", err
	}
	if issue == nil {
		return "", fmt.Errorf("issue not found: %s", issueID)
	}
	return issue.Body, nil
}
