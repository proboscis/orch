package file

import (
	"os"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/model"
)

// TestCreateIssueBaseBranchRoundTrip verifies that an issue's base_branch is
// written to frontmatter on create and read back on resolve.
func TestCreateIssueBaseBranchRoundTrip(t *testing.T) {
	dir, cleanup := setupTestVault(t)
	defer cleanup()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	issue := &model.Issue{
		ID:         "bb-issue",
		Title:      "Issue with base branch",
		Status:     model.IssueStatusOpen,
		BaseBranch: "develop",
	}
	if err := s.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}

	// Frontmatter on disk contains base_branch.
	raw, err := os.ReadFile(issue.Path)
	if err != nil {
		t.Fatalf("read issue file: %v", err)
	}
	if !strings.Contains(string(raw), "base_branch: develop") {
		t.Errorf("issue file missing base_branch frontmatter:\n%s", raw)
	}

	// Resolving the issue surfaces the typed field.
	resolved, err := s.ResolveIssue("bb-issue")
	if err != nil {
		t.Fatalf("ResolveIssue() error = %v", err)
	}
	if resolved.BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want %q", resolved.BaseBranch, "develop")
	}
}

// TestCreateIssueWithoutBaseBranch verifies base_branch is omitted when unset.
func TestCreateIssueWithoutBaseBranch(t *testing.T) {
	dir, cleanup := setupTestVault(t)
	defer cleanup()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	issue := &model.Issue{ID: "no-bb", Title: "No base branch", Status: model.IssueStatusOpen}
	if err := s.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}
	raw, err := os.ReadFile(issue.Path)
	if err != nil {
		t.Fatalf("read issue file: %v", err)
	}
	if strings.Contains(string(raw), "base_branch:") {
		t.Errorf("base_branch should be omitted when unset:\n%s", raw)
	}
	resolved, err := s.ResolveIssue("no-bb")
	if err != nil {
		t.Fatalf("ResolveIssue() error = %v", err)
	}
	if resolved.BaseBranch != "" {
		t.Errorf("BaseBranch = %q, want empty", resolved.BaseBranch)
	}
}
