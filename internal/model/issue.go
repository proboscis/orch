package model

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
