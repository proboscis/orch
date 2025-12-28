package file

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
type: issue
title: Test Issue
topic: Short topic
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
	if issue.Topic != "Short topic" {
		t.Errorf("Topic = %v, want Short topic", issue.Topic)
	}
}

func TestResolveIssueWithSymlinkedIssuesDir(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	issuesTarget, err := os.MkdirTemp("", "orch-issues-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(issuesTarget)

	issuesPath := filepath.Join(vault, "issues")
	if err := os.RemoveAll(issuesPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(issuesTarget, issuesPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	content := `---
type: issue
title: Test Issue
topic: Short topic
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

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")

	s, _ := New(vault)
	metadata := map[string]string{
		"agent":          "claude",
		"continued_from": "test123#20231220-090000",
	}
	run, err := s.CreateRun("test123", "20231220-100000", metadata)
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

	loaded, err := s.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if loaded.Agent != metadata["agent"] {
		t.Errorf("Agent = %v, want %v", loaded.Agent, metadata["agent"])
	}
	if loaded.ContinuedFrom != metadata["continued_from"] {
		t.Errorf("ContinuedFrom = %v, want %v", loaded.ContinuedFrom, metadata["continued_from"])
	}
}

func TestCreateRunDuplicate(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")

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

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")

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

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")

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

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")

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

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")
	createTestIssue(t, vault, "test456", "---\ntype: issue\ntitle: Test 2\n---\n# Test 2")

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

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")

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

func TestGetRunByShortID(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")

	s, _ := New(vault)
	run, _ := s.CreateRun("test123", "20231220-100000", nil)

	// Get the full short ID for this run
	fullShortID := run.ShortID()

	// Test exact match with full 6-char short ID
	foundRun, err := s.GetRunByShortID(fullShortID)
	if err != nil {
		t.Fatalf("GetRunByShortID() with full ID error = %v", err)
	}
	if foundRun.RunID != run.RunID {
		t.Errorf("expected run %s, got %s", run.RunID, foundRun.RunID)
	}

	// Test prefix match with 4-char prefix
	prefix4 := fullShortID[:4]
	foundRun, err = s.GetRunByShortID(prefix4)
	if err != nil {
		t.Fatalf("GetRunByShortID() with 4-char prefix error = %v", err)
	}
	if foundRun.RunID != run.RunID {
		t.Errorf("expected run %s, got %s", run.RunID, foundRun.RunID)
	}

	// Test prefix match with 2-char prefix
	prefix2 := fullShortID[:2]
	foundRun, err = s.GetRunByShortID(prefix2)
	if err != nil {
		t.Fatalf("GetRunByShortID() with 2-char prefix error = %v", err)
	}
	if foundRun.RunID != run.RunID {
		t.Errorf("expected run %s, got %s", run.RunID, foundRun.RunID)
	}
}

func TestGetRunByShortIDNotFound(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")

	s, _ := New(vault)
	s.CreateRun("test123", "20231220-100000", nil)

	// Test non-matching short ID
	_, err := s.GetRunByShortID("ffffff")
	if err == nil {
		t.Error("expected error for non-matching short ID")
	}
	if !strings.Contains(err.Error(), "run not found") {
		t.Errorf("expected 'run not found' error, got: %v", err)
	}

	// Test non-matching prefix
	_, err = s.GetRunByShortID("zz")
	if err == nil {
		t.Error("expected error for non-matching prefix")
	}
}

func TestGetRunByShortIDAmbiguous(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	createTestIssue(t, vault, "test123", "---\ntype: issue\ntitle: Test\n---\n# Test")
	createTestIssue(t, vault, "test456", "---\ntype: issue\ntitle: Test 2\n---\n# Test 2")

	s, _ := New(vault)

	// Create multiple runs
	run1, _ := s.CreateRun("test123", "20231220-100000", nil)
	run2, _ := s.CreateRun("test123", "20231220-110000", nil)
	run3, _ := s.CreateRun("test456", "20231220-120000", nil)

	// Find the shortest common prefix among all runs
	ids := []string{run1.ShortID(), run2.ShortID(), run3.ShortID()}

	// Find runs that share a common prefix (testing ambiguity)
	// Try to find any 2-char prefix that matches multiple runs
	prefixCounts := make(map[string]int)
	for _, id := range ids {
		prefix := id[:2]
		prefixCounts[prefix]++
	}

	var ambiguousPrefix string
	for prefix, count := range prefixCounts {
		if count > 1 {
			ambiguousPrefix = prefix
			break
		}
	}

	if ambiguousPrefix != "" {
		// Test that ambiguous prefix returns error
		_, err := s.GetRunByShortID(ambiguousPrefix)
		if err == nil {
			t.Error("expected error for ambiguous short ID prefix")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("expected 'ambiguous' error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "Hint:") {
			t.Errorf("expected hint in error message, got: %v", err)
		}
	} else {
		// If no natural collision, skip this test
		t.Log("No naturally ambiguous prefixes found in test runs, skipping ambiguity test")
	}
}

func TestGetRunByShortIDAmbiguousForced(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	// Create many runs to increase chance of collision
	createTestIssue(t, vault, "test", "---\ntype: issue\ntitle: Test\n---\n# Test")

	s, _ := New(vault)

	// Create 20 runs to increase collision probability
	var runs []*model.Run
	for i := 0; i < 20; i++ {
		runID := fmt.Sprintf("20231220-%02d0000", i)
		run, err := s.CreateRun("test", runID, nil)
		if err != nil {
			t.Fatalf("failed to create run %d: %v", i, err)
		}
		runs = append(runs, run)
	}

	// Find any prefix that has collisions
	prefixCounts := make(map[string][]*model.Run)
	for _, run := range runs {
		prefix := run.ShortID()[:2]
		prefixCounts[prefix] = append(prefixCounts[prefix], run)
	}

	var ambiguousPrefix string
	for prefix, matchingRuns := range prefixCounts {
		if len(matchingRuns) > 1 {
			ambiguousPrefix = prefix
			break
		}
	}

	if ambiguousPrefix == "" {
		t.Skip("No collisions found even with 20 runs, very unlucky hash distribution")
	}

	// Test that ambiguous prefix returns error
	_, err := s.GetRunByShortID(ambiguousPrefix)
	if err == nil {
		t.Error("expected error for ambiguous short ID prefix")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "ambiguous") {
		t.Errorf("expected 'ambiguous' in error, got: %v", errStr)
	}
	if !strings.Contains(errStr, "matches") {
		t.Errorf("expected 'matches' in error, got: %v", errStr)
	}
	if !strings.Contains(errStr, "Hint:") {
		t.Errorf("expected 'Hint:' in error, got: %v", errStr)
	}
}

func TestSetIssueStatus(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	content := `---
type: issue
id: test123
status: open
---
# Test`
	createTestIssue(t, vault, "test123", content)

	s, _ := New(vault)
	if err := s.SetIssueStatus("test123", model.IssueStatusResolved); err != nil {
		t.Fatalf("SetIssueStatus() error = %v", err)
	}

	// Verify cache
	issue, _ := s.ResolveIssue("test123")
	if issue.Status != model.IssueStatusResolved {
		t.Errorf("expected cached status resolved, got %s", issue.Status)
	}

	// Verify file content
	reloaded, _ := New(vault) // New store to force re-read
	issue2, _ := reloaded.ResolveIssue("test123")
	if issue2.Status != model.IssueStatusResolved {
		t.Errorf("expected reloaded status resolved, got %s", issue2.Status)
	}
}

func TestSetIssueStatusMissing(t *testing.T) {
	vault, cleanup := setupTestVault(t)
	defer cleanup()

	content := `---
type: issue
id: test123
---
# Test`
	createTestIssue(t, vault, "test123", content)

	s, _ := New(vault)
	if err := s.SetIssueStatus("test123", model.IssueStatusResolved); err != nil {
		t.Fatalf("SetIssueStatus() error = %v", err)
	}

	issue, _ := s.ResolveIssue("test123")
	if issue.Status != model.IssueStatusResolved {
		t.Errorf("expected status resolved, got %s", issue.Status)
	}
}
