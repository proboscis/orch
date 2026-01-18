package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunIssueCreatePrefersExistingIssuesDir(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "Issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir Issues: %v", err)
	}

	prev := *globalOpts
	globalOpts.IssuesRoot = vault
	globalOpts.JSON = false
	globalOpts.Quiet = true
	testBypassDaemon = true
	t.Cleanup(func() {
		*globalOpts = prev
		testBypassDaemon = false
	})

	issueID := "issue-123"
	opts := &issueCreateOptions{Title: "Test Issue"}
	if err := runIssueCreate(issueID, opts); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}

	expected := filepath.Join(issuesDir, issueID+".md")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected issue at %q: %v", expected, err)
	}
}

func TestRunIssueCreateUsesVaultIssuesDir(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "Issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir Issues: %v", err)
	}

	prev := *globalOpts
	globalOpts.IssuesRoot = issuesDir
	globalOpts.JSON = false
	globalOpts.Quiet = true
	testBypassDaemon = true
	t.Cleanup(func() {
		*globalOpts = prev
		testBypassDaemon = false
	})

	issueID := "issue-456"
	opts := &issueCreateOptions{Title: "Test Issue"}
	if err := runIssueCreate(issueID, opts); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}

	expected := filepath.Join(issuesDir, issueID+".md")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected issue at %q: %v", expected, err)
	}
}
