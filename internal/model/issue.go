package model

import (
	"fmt"
	"strconv"
	"strings"
)

type Issue struct {
	ID          string
	Title       string
	Topic       string
	Summary     string
	Status      IssueStatus
	Body        string
	Tags        []string
	Path        string
	Frontmatter map[string]string
}

func IsGitHubIssueID(id string) bool {
	if strings.HasPrefix(id, "gh-") || strings.HasPrefix(id, "gh#") {
		return true
	}
	if strings.HasPrefix(id, "#") {
		_, err := strconv.Atoi(strings.TrimPrefix(id, "#"))
		return err == nil
	}
	_, err := strconv.Atoi(id)
	return err == nil
}

func NormalizeGitHubIssueID(id string) string {
	if strings.HasPrefix(id, "gh-") {
		return id
	}
	if strings.HasPrefix(id, "gh#") {
		return "gh-" + strings.TrimPrefix(id, "gh#")
	}
	id = strings.TrimPrefix(id, "#")
	if _, err := strconv.Atoi(id); err == nil {
		return "gh-" + id
	}
	return id
}

func ParseGitHubIssueNumber(id string) (int, error) {
	id = strings.TrimPrefix(id, "gh-")
	id = strings.TrimPrefix(id, "gh#")
	id = strings.TrimPrefix(id, "#")
	n, err := strconv.Atoi(id)
	if err != nil {
		return 0, fmt.Errorf("invalid GitHub issue ID: %s", id)
	}
	return n, nil
}
