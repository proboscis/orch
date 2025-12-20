package file

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

func setupTestVault(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "orch-test-*")
	if err != nil {
		t.Fatal(err)
	}

	// Create vault structure
	os.MkdirAll(filepath.Join(dir, "issues"), 0755)
	os.MkdirAll(filepath.Join(dir, "runs"), 0755)

	return dir, func() { os.RemoveAll(dir) }
}

func createTestIssue(t *testing.T, vaultPath, issueID, content string) {
	t.Helper()
	path := filepath.Join(vaultPath, "issues", issueID+".md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNew(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	s, err := New(vault)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if s.VaultPath() != vault {
		t.Errorf("VaultPath() = %v, want %v", s.VaultPath(), vault)
	}
}

func TestNewInvalidPath(t *testing.T) {
	_, err := New("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestResolveIssue(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	content := `---
title: Test Issue
status: open
---

# Test Issue

This is a test issue.
`
	createTestIssue(t, vault, "test123", content)

	s, _ := New(vault)
	issue, err := s.ResolveIssue("test123")
	if err != nil {
		t.Fatalf("ResolveIssue() error = %v", err)
	}

	if issue.ID != "test123" {
		t.Errorf("ID = %v, want test123", issue.ID)
	}
	if issue.Title != "Test Issue" {
		t.Errorf("Title = %v, want Test Issue", issue.Title)
	}
}

func TestResolveIssueNotFound(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	s, _ := New(vault)
	_, err := s.ResolveIssue("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent issue")
	}
}

func TestCreateRun(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntitle: Test\n---\n# Test")

	s, _ := New(vault)
	run, err := s.CreateRun("test123", "20231220-100000", map[string]string{"agent": "claude"})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if run.IssueID != "test123" {
		t.Errorf("IssueID = %v, want test123", run.IssueID)
	}
	if run.RunID != "20231220-100000" {
		t.Errorf("RunID = %v, want 20231220-100000", run.RunID)
	}

	// Verify file exists
	if _, err := os.Stat(run.Path); os.IsNotExist(err) {
		t.Error("run file was not created")
	}
}

func TestCreateRunDuplicate(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntitle: Test\n---\n# Test")

	s, _ := New(vault)
	_, err := s.CreateRun("test123", "20231220-100000", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Try to create same run again
	_, err = s.CreateRun("test123", "20231220-100000", nil)
	if err == nil {
		t.Error("expected error for duplicate run")
	}
}

func TestAppendEvent(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntitle: Test\n---\n# Test")

	s, _ := New(vault)
	run, _ := s.CreateRun("test123", "20231220-100000", nil)

	event := model.NewStatusEvent(model.StatusRunning)
	if err := s.AppendEvent(run.Ref(), event); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	// Read the run again and verify event
	updated, _ := s.GetRun(run.Ref())
	if len(updated.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(updated.Events))
	}
	if updated.Events[0].Type != model.EventTypeStatus {
		t.Errorf("expected status event, got %s", updated.Events[0].Type)
	}
}

func TestGetRun(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntitle: Test\n---\n# Test")

	s, _ := New(vault)
	_, _ = s.CreateRun("test123", "20231220-100000", nil)

	ref, _ := model.ParseRunRef("test123#20231220-100000")
	run, err := s.GetRun(ref)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}

	if run.IssueID != "test123" || run.RunID != "20231220-100000" {
		t.Errorf("unexpected run: %+v", run)
	}
}

func TestGetLatestRun(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntitle: Test\n---\n# Test")

	s, _ := New(vault)
	s.CreateRun("test123", "20231220-100000", nil)
	s.CreateRun("test123", "20231220-110000", nil)
	s.CreateRun("test123", "20231220-090000", nil)

	run, err := s.GetLatestRun("test123")
	if err != nil {
		t.Fatalf("GetLatestRun() error = %v", err)
	}

	if run.RunID != "20231220-110000" {
		t.Errorf("expected latest run 20231220-110000, got %s", run.RunID)
	}
}

func TestListRuns(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntitle: Test\n---\n# Test")
	createTestIssue(t, vault, "test456", "---\ntitle: Test 2\n---\n# Test 2")

	s, _ := New(vault)
	s.CreateRun("test123", "20231220-100000", nil)
	s.CreateRun("test123", "20231220-110000", nil)
	s.CreateRun("test456", "20231220-120000", nil)

	// List all
	runs, err := s.ListRuns(nil)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 3 {
		t.Errorf("expected 3 runs, got %d", len(runs))
	}

	// Filter by issue
	runs, _ = s.ListRuns(&store.ListRunsFilter{IssueID: "test123"})
	if len(runs) != 2 {
		t.Errorf("expected 2 runs for test123, got %d", len(runs))
	}

	// Filter with limit
	runs, _ = s.ListRuns(&store.ListRunsFilter{Limit: 1})
	if len(runs) != 1 {
		t.Errorf("expected 1 run with limit, got %d", len(runs))
	}
}

func TestListRunsWithStatusFilter(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntitle: Test\n---\n# Test")

	s, _ := New(vault)

	// Create runs with different statuses
	run1, _ := s.CreateRun("test123", "20231220-100000", nil)
	s.AppendEvent(run1.Ref(), model.NewStatusEvent(model.StatusRunning))

	run2, _ := s.CreateRun("test123", "20231220-110000", nil)
	s.AppendEvent(run2.Ref(), model.NewStatusEvent(model.StatusBlocked))

	run3, _ := s.CreateRun("test123", "20231220-120000", nil)
	s.AppendEvent(run3.Ref(), model.NewStatusEvent(model.StatusDone))

	// Filter by running status
	runs, _ := s.ListRuns(&store.ListRunsFilter{Status: []model.Status{model.StatusRunning}})
	if len(runs) != 1 {
		t.Errorf("expected 1 running run, got %d", len(runs))
	}

	// Filter by multiple statuses
	runs, _ = s.ListRuns(&store.ListRunsFilter{Status: []model.Status{model.StatusRunning, model.StatusBlocked}})
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
}
