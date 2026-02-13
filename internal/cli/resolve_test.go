package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/s22625/orch/internal/orchapi"
)

type mockResolveAPI struct {
	orchapi.OrchAPI
	resolved        map[string]bool
	hasCompletedRun map[string]bool
}

func (m *mockResolveAPI) ResolveIssue(ctx context.Context, issueID string, force bool) error {
	if m.resolved == nil {
		m.resolved = make(map[string]bool)
	}
	if m.hasCompletedRun == nil {
		m.hasCompletedRun = make(map[string]bool)
	}

	if !m.hasCompletedRun[issueID] && !force {
		return errors.New("no completed runs")
	}

	m.resolved[issueID] = true
	return nil
}

func (m *mockResolveAPI) GetIssue(ctx context.Context, issueID string) (*orchapi.Issue, error) {
	status := orchapi.IssueStatusOpen
	if m.resolved != nil && m.resolved[issueID] {
		status = orchapi.IssueStatusResolved
	}
	return &orchapi.Issue{ID: issueID, Status: status}, nil
}

func TestRunResolveMarksIssueResolved(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true

	mock := &mockResolveAPI{
		hasCompletedRun: map[string]bool{"issue-1": true},
	}
	deps := &resolveDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mock, nil
		},
	}

	if err := runResolveWithDeps(context.Background(), "issue-1", &resolveOptions{}, deps); err != nil {
		t.Fatalf("runResolve: %v", err)
	}

	if !mock.resolved["issue-1"] {
		t.Fatal("issue-1 should be marked as resolved")
	}
}

func TestRunResolveRequiresForceWithoutCompletedRuns(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true

	mock := &mockResolveAPI{
		hasCompletedRun: map[string]bool{"issue-2": false},
	}
	deps := &resolveDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mock, nil
		},
	}

	err := runResolveWithDeps(context.Background(), "issue-2", &resolveOptions{}, deps)
	if err == nil {
		t.Fatal("expected error without --force when no completed runs")
	}

	if err := runResolveWithDeps(context.Background(), "issue-2", &resolveOptions{Force: true}, deps); err != nil {
		t.Fatalf("runResolve --force: %v", err)
	}

	if !mock.resolved["issue-2"] {
		t.Fatal("issue-2 should be marked as resolved with --force")
	}
}

func TestRunResolveAlreadyResolved(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true

	mock := &mockResolveAPI{
		resolved:        map[string]bool{"issue-3": true},
		hasCompletedRun: map[string]bool{"issue-3": true},
	}
	deps := &resolveDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mock, nil
		},
	}

	if err := runResolveWithDeps(context.Background(), "issue-3", &resolveOptions{}, deps); err != nil {
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
