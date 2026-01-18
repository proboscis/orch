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
	Tags        []string
	Body        string
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

// ParseTags parses a tags string from frontmatter.
// Supports formats: "[tag1, tag2]", "tag1, tag2", "tag1,tag2"
func ParseTags(s string) []string {
	if s == "" {
		return nil
	}

	// Remove brackets if present: [tag1, tag2] -> tag1, tag2
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")

	// Split by comma
	parts := strings.Split(s, ",")
	var tags []string
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// FormatTags formats tags for frontmatter output.
func FormatTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "[" + strings.Join(tags, ", ") + "]"
}
