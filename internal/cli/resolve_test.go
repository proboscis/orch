package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestRunResolveMarksIssueResolved(t *testing.T) {
	resetGlobalOpts(t)

	vault := t.TempDir()
	globalOpts.VaultPath = vault
	globalOpts.Backend = "file"
	globalOpts.Quiet = true

	writeIssue(t, vault, "issue-1")

	st, err := getStore()
	if err != nil {
		t.Fatalf("getStore: %v", err)
	}

	// Create a completed run
	run, err := st.CreateRun("issue-1", "run-1", nil)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusDone)); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	// Resolve the issue
	if err := runResolve("issue-1", &resolveOptions{}); err != nil {
		t.Fatalf("runResolve: %v", err)
	}

	// Get a fresh store to check the updated issue status
	st2, err := getStore()
	if err != nil {
		t.Fatalf("getStore for check: %v", err)
	}

	// Check that issue status is resolved
	issue, err := st2.ResolveIssue("issue-1")
	if err != nil {
		t.Fatalf("ResolveIssue: %v", err)
	}
	if issue.Status != model.IssueStatusResolved {
		t.Fatalf("issue status = %q, want %q", issue.Status, model.IssueStatusResolved)
	}
}

func TestRunResolveRequiresForceWithoutCompletedRuns(t *testing.T) {
	resetGlobalOpts(t)

	vault := t.TempDir()
	globalOpts.VaultPath = vault
	globalOpts.Backend = "file"
	globalOpts.Quiet = true

	writeIssue(t, vault, "issue-2")

	st, err := getStore()
	if err != nil {
		t.Fatalf("getStore: %v", err)
	}

	// Create a running (not completed) run
	run, err := st.CreateRun("issue-2", "run-1", nil)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusRunning)); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	// Resolve should fail without --force since no completed runs
	if err := runResolve("issue-2", &resolveOptions{}); err == nil {
		t.Fatal("expected error without --force when no completed runs")
	}

	// Resolve should succeed with --force
	if err := runResolve("issue-2", &resolveOptions{Force: true}); err != nil {
		t.Fatalf("runResolve --force: %v", err)
	}

	// Get a fresh store to check the updated issue status
	st2, err := getStore()
	if err != nil {
		t.Fatalf("getStore for check: %v", err)
	}

	// Check that issue status is resolved
	issue, err := st2.ResolveIssue("issue-2")
	if err != nil {
		t.Fatalf("ResolveIssue: %v", err)
	}
	if issue.Status != model.IssueStatusResolved {
		t.Fatalf("issue status = %q, want %q", issue.Status, model.IssueStatusResolved)
	}
}

func TestRunResolveAlreadyResolved(t *testing.T) {
	resetGlobalOpts(t)

	vault := t.TempDir()
	globalOpts.VaultPath = vault
	globalOpts.Backend = "file"
	globalOpts.Quiet = true

	writeIssueWithStatus(t, vault, "issue-3", "resolved")

	// Resolve should succeed (no-op) for already resolved issue
	if err := runResolve("issue-3", &resolveOptions{}); err != nil {
		t.Fatalf("runResolve already resolved: %v", err)
	}
}

func writeIssue(t *testing.T, vaultPath, issueID string) {
	t.Helper()
	writeIssueWithStatus(t, vaultPath, issueID, "open")
}

func writeIssueWithStatus(t *testing.T, vaultPath, issueID, status string) {
	t.Helper()

	issuesDir := filepath.Join(vaultPath, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}

	issuePath := filepath.Join(issuesDir, issueID+".md")
	content := fmt.Sprintf("---\ntype: issue\nid: %s\ntitle: %s\nstatus: %s\n---\n# %s\n", issueID, issueID, status, issueID)
	if err := os.WriteFile(issuePath, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}
}
