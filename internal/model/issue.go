package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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
	BaseBranch  string // Explicit root branch new runs branch off of (frontmatter: base_branch)
	Frontmatter map[string]string
	ModifiedAt  time.Time // File modification time for sorting
}

// RenderFrontmatter returns the YAML frontmatter block (including the leading and
// trailing "---" fences and a trailing newline) for an issue Markdown file.
//
// This is the single source of truth for issue frontmatter serialization so that
// every create path (CLI local/editor, daemon proto/legacy socket, FileStore)
// writes a consistent set of fields. Callers append the body (and any "# title"
// header) after this block.
func (i *Issue) RenderFrontmatter() string {
	status := i.Status
	if status == "" {
		status = IssueStatusOpen
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("type: issue\n")
	sb.WriteString("id: " + QuoteYAMLValue(i.ID) + "\n")
	sb.WriteString("title: " + QuoteYAMLValue(i.Title) + "\n")
	if i.Summary != "" {
		sb.WriteString("summary: " + QuoteYAMLValue(i.Summary) + "\n")
	}
	sb.WriteString("status: " + string(status) + "\n")
	if i.BaseBranch != "" {
		sb.WriteString("base_branch: " + QuoteYAMLValue(i.BaseBranch) + "\n")
	}
	if len(i.Tags) > 0 {
		sb.WriteString("tags: " + FormatTags(i.Tags) + "\n")
	}
	sb.WriteString("---\n")
	return sb.String()
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
