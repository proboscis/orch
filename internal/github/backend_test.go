package github

import (
	"path/filepath"
	"testing"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
	_ "modernc.org/sqlite"
)

func TestNewBackend(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.db")

	cfg := &config.GitHubConfig{
		Owner: "testowner",
		Repo:  "testrepo",
	}

	backend, err := NewBackend(cfg, cachePath)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}

	if backend.owner != "testowner" {
		t.Errorf("owner = %q, want %q", backend.owner, "testowner")
	}
	if backend.repo != "testrepo" {
		t.Errorf("repo = %q, want %q", backend.repo, "testrepo")
	}
}

func TestNewBackendMissingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.db")

	cfg := &config.GitHubConfig{}

	_, err := NewBackend(cfg, cachePath)
	if err == nil {
		t.Error("NewBackend should fail with empty config")
	}
}

func TestBackendListFromCache(t *testing.T) {
	backend := newTestBackend(t)

	issues := []*model.Issue{
		{ID: "gh-1", Title: "First", Status: model.IssueStatusOpen, Frontmatter: map[string]string{"number": "1"}},
		{ID: "gh-2", Title: "Second", Status: model.IssueStatusOpen, Frontmatter: map[string]string{"number": "2"}},
	}

	for _, issue := range issues {
		if err := backend.cache.Upsert(issue); err != nil {
			t.Fatalf("cache.Upsert failed: %v", err)
		}
	}

	got, err := backend.ListFromCache()
	if err != nil {
		t.Fatalf("ListFromCache failed: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("ListFromCache returned %d issues, want 2", len(got))
	}
}

func TestBackendGetFromCache(t *testing.T) {
	backend := newTestBackend(t)

	issue := &model.Issue{
		ID:     "gh-42",
		Title:  "Cached Issue",
		Status: model.IssueStatusOpen,
		Frontmatter: map[string]string{
			"number": "42",
		},
	}

	if err := backend.cache.Upsert(issue); err != nil {
		t.Fatalf("cache.Upsert failed: %v", err)
	}

	got, err := backend.GetFromCache(42)
	if err != nil {
		t.Fatalf("GetFromCache failed: %v", err)
	}

	if got.ID != "gh-42" {
		t.Errorf("ID = %q, want %q", got.ID, "gh-42")
	}
	if got.Title != "Cached Issue" {
		t.Errorf("Title = %q, want %q", got.Title, "Cached Issue")
	}
}

func TestBackendGetByIDFromCache(t *testing.T) {
	backend := newTestBackend(t)

	issue := &model.Issue{
		ID:     "gh-99",
		Title:  "Issue 99",
		Status: model.IssueStatusOpen,
		Frontmatter: map[string]string{
			"number": "99",
		},
	}

	if err := backend.cache.Upsert(issue); err != nil {
		t.Fatalf("cache.Upsert failed: %v", err)
	}

	got, err := backend.GetByIDFromCache("gh-99")
	if err != nil {
		t.Fatalf("GetByIDFromCache failed: %v", err)
	}

	if got.ID != "gh-99" {
		t.Errorf("ID = %q, want %q", got.ID, "gh-99")
	}
}

func TestBackendRepoArg(t *testing.T) {
	backend := newTestBackend(t)
	got := backend.repoArg()
	want := "testowner/testrepo"

	if got != want {
		t.Errorf("repoArg() = %q, want %q", got, want)
	}
}

func TestBackendParseIssueNumber(t *testing.T) {
	backend := newTestBackend(t)

	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"gh-123", 123, false},
		{"#456", 456, false},
		{"789", 789, false},
		{"abc", 0, true},
		{"gh-abc", 0, true},
	}

	for _, tt := range tests {
		got, err := backend.parseIssueNumber(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseIssueNumber(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseIssueNumber(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestBackendExtractNumberFromURL(t *testing.T) {
	backend := newTestBackend(t)

	tests := []struct {
		url  string
		want int
	}{
		{"https://github.com/owner/repo/issues/123", 123},
		{"https://github.com/owner/repo/issues/456", 456},
		{"https://github.com/owner/repo/pull/789", 789},
		{"", 0},
		{"https://github.com/owner/repo", 0},
	}

	for _, tt := range tests {
		got := backend.extractNumberFromURL(tt.url)
		if got != tt.want {
			t.Errorf("extractNumberFromURL(%q) = %d, want %d", tt.url, got, tt.want)
		}
	}
}

func TestBackendMapLabelsToStatus(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.db")

	cfg := &config.GitHubConfig{
		Owner: "testowner",
		Repo:  "testrepo",
		StatusLabels: map[string]string{
			"status:in-progress": "in_progress",
			"status:waiting":     "waiting",
			"status:blocked":     "blocked",
			"status:done":        "done",
		},
	}

	backend, err := NewBackend(cfg, cachePath)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}

	tests := []struct {
		labels []string
		want   model.IssueStatus
	}{
		{[]string{"bug", "status:in-progress"}, model.IssueStatus("in_progress")},
		{[]string{"status:waiting", "urgent"}, model.IssueStatus("waiting")},
		{[]string{"status:blocked", "urgent"}, model.IssueStatus("blocked")},
		{[]string{"enhancement"}, model.IssueStatus("")},
	}

	for _, tt := range tests {
		got := backend.mapLabelsToStatus(tt.labels)
		if got != tt.want {
			t.Errorf("mapLabelsToStatus(%v) = %q, want %q", tt.labels, got, tt.want)
		}
	}
}

func TestBackendMapLabelsToStatusNoConfig(t *testing.T) {
	backend := newTestBackend(t)

	got := backend.mapLabelsToStatus([]string{"bug", "urgent"})
	if got != "" {
		t.Errorf("mapLabelsToStatus with no config = %q, want empty", got)
	}
}

func TestTruncateSummary(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly ten", 11, "exactly ten"},
		{"this is a long string", 10, "this is..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncateSummary(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateSummary(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestBackendCache(t *testing.T) {
	backend := newTestBackend(t)

	cache := backend.Cache()
	if cache == nil {
		t.Error("Cache() returned nil")
	}
}

func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.db")

	cfg := &config.GitHubConfig{
		Owner: "testowner",
		Repo:  "testrepo",
	}

	backend, err := NewBackend(cfg, cachePath)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}

	return backend
}
