package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestRunResolveMarksResolved(t *testing.T) {
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

	run, err := st.CreateRun("issue-1", "run-1", nil)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusDone)); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	if err := runResolve("issue-1#run-1", &resolveOptions{}); err != nil {
		t.Fatalf("runResolve: %v", err)
	}

	updated, err := st.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if updated.Status != model.StatusCompleted {
		t.Fatalf("status = %q, want %q", updated.Status, model.StatusCompleted)
	}
}

func TestRunResolveRequiresForceForActive(t *testing.T) {
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

	run, err := st.CreateRun("issue-2", "run-1", nil)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusRunning)); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	if err := runResolve("issue-2#run-1", &resolveOptions{}); err == nil {
		t.Fatal("expected error without --force")
	}

	if err := runResolve("issue-2#run-1", &resolveOptions{Force: true}); err != nil {
		t.Fatalf("runResolve --force: %v", err)
	}

	updated, err := st.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if updated.Status != model.StatusCompleted {
		t.Fatalf("status = %q, want %q", updated.Status, model.StatusCompleted)
	}
}

func writeIssue(t *testing.T, vaultPath, issueID string) {
	t.Helper()

	issuesDir := filepath.Join(vaultPath, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}

	issuePath := filepath.Join(issuesDir, issueID+".md")
	content := fmt.Sprintf("---\ntype: issue\nid: %s\ntitle: %s\n---\n# %s\n", issueID, issueID, issueID)
	if err := os.WriteFile(issuePath, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}
}
