package github

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/s22625/orch/internal/model"
	_ "modernc.org/sqlite"
)

func TestNewIssueCache(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cache", "issues.db")

	cache, err := NewIssueCache(dbPath)
	if err != nil {
		t.Fatalf("NewIssueCache failed: %v", err)
	}
	defer cache.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestCacheUpsertAndGet(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Close()

	issue := &model.Issue{
		ID:      "gh-123",
		Title:   "Test Issue",
		Summary: "Test summary",
		Status:  model.IssueStatusOpen,
		Body:    "Issue body content",
		Path:    "https://github.com/test/repo/issues/123",
		Frontmatter: map[string]string{
			"number":     "123",
			"url":        "https://github.com/test/repo/issues/123",
			"labels":     "bug,urgent",
			"updated_at": "2025-01-18T00:00:00Z",
		},
	}

	if err := cache.Upsert(issue); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	got, err := cache.Get(123)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.ID != issue.ID {
		t.Errorf("ID = %q, want %q", got.ID, issue.ID)
	}
	if got.Title != issue.Title {
		t.Errorf("Title = %q, want %q", got.Title, issue.Title)
	}
	if got.Status != issue.Status {
		t.Errorf("Status = %q, want %q", got.Status, issue.Status)
	}
}

func TestCacheGetByID(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Close()

	issue := &model.Issue{
		ID:     "gh-456",
		Title:  "Another Issue",
		Status: model.IssueStatusOpen,
		Frontmatter: map[string]string{
			"number": "456",
		},
	}

	if err := cache.Upsert(issue); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	got, err := cache.GetByID("gh-456")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.ID != "gh-456" {
		t.Errorf("ID = %q, want %q", got.ID, "gh-456")
	}
}

func TestCacheListAll(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Close()

	issues := []*model.Issue{
		{ID: "gh-1", Title: "First", Status: model.IssueStatusOpen, Frontmatter: map[string]string{"number": "1"}},
		{ID: "gh-2", Title: "Second", Status: model.IssueStatusClosed, Frontmatter: map[string]string{"number": "2"}},
		{ID: "gh-3", Title: "Third", Status: model.IssueStatusOpen, Frontmatter: map[string]string{"number": "3"}},
	}

	for _, issue := range issues {
		if err := cache.Upsert(issue); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	got, err := cache.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	if len(got) != 3 {
		t.Errorf("ListAll returned %d issues, want 3", len(got))
	}
}

func TestCacheListByStatus(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Close()

	issues := []*model.Issue{
		{ID: "gh-1", Title: "Open 1", Status: model.IssueStatusOpen, Frontmatter: map[string]string{"number": "1"}},
		{ID: "gh-2", Title: "Closed", Status: model.IssueStatusClosed, Frontmatter: map[string]string{"number": "2"}},
		{ID: "gh-3", Title: "Open 2", Status: model.IssueStatusOpen, Frontmatter: map[string]string{"number": "3"}},
	}

	for _, issue := range issues {
		if err := cache.Upsert(issue); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	open, err := cache.ListByStatus(string(model.IssueStatusOpen))
	if err != nil {
		t.Fatalf("ListByStatus failed: %v", err)
	}

	if len(open) != 2 {
		t.Errorf("ListByStatus(open) returned %d issues, want 2", len(open))
	}

	closed, err := cache.ListByStatus(string(model.IssueStatusClosed))
	if err != nil {
		t.Fatalf("ListByStatus failed: %v", err)
	}

	if len(closed) != 1 {
		t.Errorf("ListByStatus(closed) returned %d issues, want 1", len(closed))
	}
}

func TestCacheUpsertUpdatesExisting(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Close()

	issue := &model.Issue{
		ID:     "gh-100",
		Title:  "Original Title",
		Status: model.IssueStatusOpen,
		Frontmatter: map[string]string{
			"number": "100",
		},
	}

	if err := cache.Upsert(issue); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	issue.Title = "Updated Title"
	issue.Status = model.IssueStatusClosed

	if err := cache.Upsert(issue); err != nil {
		t.Fatalf("Upsert (update) failed: %v", err)
	}

	got, err := cache.Get(100)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", got.Title, "Updated Title")
	}
	if got.Status != model.IssueStatusClosed {
		t.Errorf("Status = %q, want %q", got.Status, model.IssueStatusClosed)
	}
}

func TestCacheDelete(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Close()

	issue := &model.Issue{
		ID:     "gh-200",
		Title:  "To Delete",
		Status: model.IssueStatusOpen,
		Frontmatter: map[string]string{
			"number": "200",
		},
	}

	if err := cache.Upsert(issue); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	if err := cache.Delete("gh-200"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := cache.GetByID("gh-200")
	if err == nil {
		t.Error("GetByID should fail after delete")
	}
}

func TestCacheClear(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Close()

	for i := 1; i <= 5; i++ {
		issue := &model.Issue{
			ID:     "gh-" + string(rune('0'+i)),
			Title:  "Issue",
			Status: model.IssueStatusOpen,
			Frontmatter: map[string]string{
				"number": string(rune('0' + i)),
			},
		}
		cache.Upsert(issue)
	}

	if err := cache.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	issues, err := cache.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	if len(issues) != 0 {
		t.Errorf("ListAll after Clear returned %d issues, want 0", len(issues))
	}
}

func TestCacheGetNotFound(t *testing.T) {
	cache := newTestCache(t)
	defer cache.Close()

	_, err := cache.Get(999)
	if err == nil {
		t.Error("Get should return error for non-existent issue")
	}
}

func newTestCache(t *testing.T) *IssueCache {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewIssueCache(dbPath)
	if err != nil {
		t.Fatalf("NewIssueCache failed: %v", err)
	}

	return cache
}
