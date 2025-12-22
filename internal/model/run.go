package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// RunRef represents a reference to a run (ISSUE_ID#RUN_ID)
type RunRef struct {
	IssueID string
	RunID   string
}

// ParseRunRef parses a RUN_REF string (ISSUE_ID#RUN_ID or just ISSUE_ID for latest)
func ParseRunRef(ref string) (*RunRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty run reference")
	}

	parts := strings.SplitN(ref, "#", 2)
	r := &RunRef{
		IssueID: parts[0],
	}
	if len(parts) > 1 {
		r.RunID = parts[1]
	}
	return r, nil
}

// String returns the canonical RUN_REF format
func (r *RunRef) String() string {
	if r.RunID == "" {
		return r.IssueID
	}
	return r.IssueID + "#" + r.RunID
}

// IsLatest returns true if this ref points to the latest run (no RunID specified)
func (r *RunRef) IsLatest() bool {
	return r.RunID == ""
}

// Run represents a single execution of an issue
type Run struct {
	IssueID string
	RunID   string
	Path    string // File path to run document

	// Derived from events
	Status    Status
	Phase     Phase
	Events    []*Event
	StartedAt time.Time
	UpdatedAt time.Time

	// Artifacts (from events)
	Agent        string
	Branch       string
	WorktreePath string
	TmuxSession  string
	TmuxWindowID string
	PRUrl        string

	// Frontmatter metadata
	ContinuedFrom string
}

// Ref returns the RunRef for this run
func (r *Run) Ref() *RunRef {
	return &RunRef{
		IssueID: r.IssueID,
		RunID:   r.RunID,
	}
}

// ShortID returns a 6-character hex identifier for the run (git-style)
func (r *Run) ShortID() string {
	return GenerateShortID(r.IssueID, r.RunID)
}

// GenerateShortID generates a 6-char hex ID from issue and run IDs
func GenerateShortID(issueID, runID string) string {
	h := sha256.Sum256([]byte(issueID + "#" + runID))
	return hex.EncodeToString(h[:])[:6]
}

// GetStatus derives status from events (last status event wins)
func (r *Run) GetStatus() Status {
	for i := len(r.Events) - 1; i >= 0; i-- {
		e := r.Events[i]
		if e.Type == EventTypeStatus {
			return Status(e.Name)
		}
	}
	return StatusQueued
}

// GetPhase derives phase from events (last phase event wins).
func (r *Run) GetPhase() Phase {
	for i := len(r.Events) - 1; i >= 0; i-- {
		e := r.Events[i]
		if e.Type == EventTypePhase {
			return Phase(e.Name)
		}
	}
	return ""
}

// GetArtifacts extracts artifacts from events
func (r *Run) GetArtifacts() map[string]map[string]string {
	artifacts := make(map[string]map[string]string)
	for _, e := range r.Events {
		if e.Type == EventTypeArtifact {
			artifacts[e.Name] = e.Attrs
		}
	}
	return artifacts
}

// DeriveState updates Status and artifacts from events
func (r *Run) DeriveState() {
	r.Status = r.GetStatus()
	r.Phase = r.GetPhase()

	artifacts := r.GetArtifacts()
	if worktree, ok := artifacts["worktree"]; ok {
		r.WorktreePath = worktree["path"]
	}
	if branch, ok := artifacts["branch"]; ok {
		r.Branch = branch["name"]
	}
	if session, ok := artifacts["session"]; ok {
		r.TmuxSession = session["name"]
	}
	if window, ok := artifacts["window"]; ok {
		r.TmuxWindowID = window["id"]
	}
	if pr, ok := artifacts["pr"]; ok {
		r.PRUrl = pr["url"]
	}

	// Derive timestamps
	if len(r.Events) > 0 {
		r.StartedAt = r.Events[0].Timestamp
		r.UpdatedAt = r.Events[len(r.Events)-1].Timestamp
	}
}

// GenerateRunID generates a run ID using the convention YYYYMMDD-HHMMSS
func GenerateRunID() string {
	return time.Now().Format("20060102-150405")
}

// GenerateBranchName generates a branch name using the convention
func GenerateBranchName(issueID, runID string) string {
	return fmt.Sprintf("issue/%s/run-%s", issueID, runID)
}

// BranchTemplateData holds values for branch naming templates.
type BranchTemplateData struct {
	IssueID       string
	RunID         string
	ShortID       string
	GeneratedName string
	DefaultBranch string
	User          string
}

var branchTemplateReplacer = strings.NewReplacer(
	"<issue-id>", "{{.IssueID}}",
	"<run-id>", "{{.RunID}}",
	"<short-id>", "{{.ShortID}}",
	"<generated-name>", "{{.GeneratedName}}",
	"<doeff-generated-name>", "{{.GeneratedName}}",
	"<default-branch>", "{{.DefaultBranch}}",
	"<branch-name>", "{{.DefaultBranch}}",
	"<user-space>", "{{.User}}",
	"<user>", "{{.User}}",
)

var branchTemplateTokenPattern = regexp.MustCompile(`<[^>]+>`)

// GenerateBranchNameFromTemplate renders a branch name from a template string.
func GenerateBranchNameFromTemplate(templateStr, issueID, runID string) (string, error) {
	if strings.TrimSpace(templateStr) == "" {
		return GenerateBranchName(issueID, runID), nil
	}

	data := BranchTemplateData{
		IssueID:       issueID,
		RunID:         runID,
		ShortID:       GenerateShortID(issueID, runID),
		GeneratedName: fmt.Sprintf("%s/run-%s", issueID, runID),
		DefaultBranch: GenerateBranchName(issueID, runID),
		User:          branchTemplateUser(),
	}

	normalized := branchTemplateReplacer.Replace(templateStr)
	if unknown := branchTemplateTokenPattern.FindAllString(normalized, -1); len(unknown) > 0 {
		return "", fmt.Errorf("unknown branch template placeholders: %s", strings.Join(uniqueStrings(unknown), ", "))
	}

	tmpl, err := template.New("branch").Option("missingkey=error").Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("invalid branch template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render branch template: %w", err)
	}

	branch := strings.TrimSpace(buf.String())
	if branch == "" {
		return "", fmt.Errorf("branch template produced an empty branch name")
	}

	return branch, nil
}

func branchTemplateUser() string {
	user := strings.TrimSpace(os.Getenv("USER"))
	if user != "" {
		return user
	}
	return strings.TrimSpace(os.Getenv("USERNAME"))
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// GenerateTmuxSession generates a tmux session name using the convention
func GenerateTmuxSession(issueID, runID string) string {
	return fmt.Sprintf("run-%s-%s", issueID, runID)
}
