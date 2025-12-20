package model

// Issue represents a specification unit
type Issue struct {
	ID          string
	Title       string
	Body        string
	Path        string            // File path to issue document
	Frontmatter map[string]string // YAML frontmatter fields
}
