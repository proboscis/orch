package model

// IssueStatus represents the resolution state of an issue
type IssueStatus string

const (
	IssueStatusOpen     IssueStatus = "open"
	IssueStatusResolved IssueStatus = "resolved"
	IssueStatusClosed   IssueStatus = "closed"
)

// ValidIssueStatuses contains all valid issue status values
var ValidIssueStatuses = []IssueStatus{
	IssueStatusOpen,
	IssueStatusResolved,
	IssueStatusClosed,
}

// IsValidIssueStatus checks if a status string is a valid issue status
func IsValidIssueStatus(s string) bool {
	for _, status := range ValidIssueStatuses {
		if string(status) == s {
			return true
		}
	}
	return false
}

// ParseIssueStatus converts a string to IssueStatus, returning open for empty/invalid values
func ParseIssueStatus(s string) IssueStatus {
	if s == "" {
		return IssueStatusOpen
	}
	status := IssueStatus(s)
	for _, valid := range ValidIssueStatuses {
		if status == valid {
			return status
		}
	}
	return IssueStatusOpen
}

// Issue represents a specification unit
type Issue struct {
	ID          string
	Title       string
	Topic       string // Short topic for ps display
	Summary     string // Short one-line summary for display
	Body        string
	Path        string            // File path to issue document
	Frontmatter map[string]string // YAML frontmatter fields
}

// Status returns the typed IssueStatus from the frontmatter
func (i *Issue) Status() IssueStatus {
	if i == nil || i.Frontmatter == nil {
		return IssueStatusOpen
	}
	return ParseIssueStatus(i.Frontmatter["status"])
}
