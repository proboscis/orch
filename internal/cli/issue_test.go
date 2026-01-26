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

func TestMatchTagsAnd(t *testing.T) {
	tests := []struct {
		name       string
		issueTags  []string
		filterTags []string
		want       bool
	}{
		{"empty filter", []string{"bug", "feature"}, nil, true},
		{"match all", []string{"bug", "feature", "urgent"}, []string{"bug", "feature"}, true},
		{"missing one", []string{"bug"}, []string{"bug", "feature"}, false},
		{"case insensitive", []string{"Bug", "FEATURE"}, []string{"bug", "feature"}, true},
		{"empty issue tags", nil, []string{"bug"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchTagsAnd(tt.issueTags, tt.filterTags); got != tt.want {
				t.Errorf("matchTagsAnd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchTagsOr(t *testing.T) {
	tests := []struct {
		name       string
		issueTags  []string
		filterTags []string
		want       bool
	}{
		{"empty filter", []string{"bug", "feature"}, nil, true},
		{"match one", []string{"bug"}, []string{"bug", "feature"}, true},
		{"match none", []string{"urgent"}, []string{"bug", "feature"}, false},
		{"case insensitive", []string{"BUG"}, []string{"bug", "feature"}, true},
		{"empty issue tags", nil, []string{"bug"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchTagsOr(tt.issueTags, tt.filterTags); got != tt.want {
				t.Errorf("matchTagsOr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchIssueFilters(t *testing.T) {
	tests := []struct {
		name   string
		status string
		tags   []string
		opts   *issueListOptions
		want   bool
	}{
		{"no filters", "open", []string{"bug"}, &issueListOptions{}, true},
		{"status match", "open", nil, &issueListOptions{Status: "open"}, true},
		{"status no match", "closed", nil, &issueListOptions{Status: "open"}, false},
		{"status case insensitive", "OPEN", nil, &issueListOptions{Status: "open"}, true},
		{"tag and match", "open", []string{"bug", "urgent"}, &issueListOptions{Tags: []string{"bug", "urgent"}}, true},
		{"tag and no match", "open", []string{"bug"}, &issueListOptions{Tags: []string{"bug", "urgent"}}, false},
		{"tag or match", "open", []string{"bug"}, &issueListOptions{TagsAny: []string{"bug", "feature"}}, true},
		{"tag or no match", "open", []string{"urgent"}, &issueListOptions{TagsAny: []string{"bug", "feature"}}, false},
		{"combined filters", "open", []string{"bug", "urgent"}, &issueListOptions{Status: "open", Tags: []string{"bug"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchIssueFilters(tt.status, tt.tags, tt.opts); got != tt.want {
				t.Errorf("matchIssueFilters() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIssueListOptionsWithRuns(t *testing.T) {
	opts := &issueListOptions{WithRuns: true}
	if !opts.WithRuns {
		t.Error("WithRuns should be true")
	}

	opts = &issueListOptions{WithRuns: false}
	if opts.WithRuns {
		t.Error("WithRuns should be false")
	}
}

func TestFormatRunsSummary(t *testing.T) {
	tests := []struct {
		name string
		runs []runSummary
		want string
	}{
		{
			name: "empty runs",
			runs: []runSummary{},
			want: "-",
		},
		{
			name: "single running",
			runs: []runSummary{{RunID: "run1", Status: "running"}},
			want: "1 running",
		},
		{
			name: "multiple same status",
			runs: []runSummary{
				{RunID: "run1", Status: "running"},
				{RunID: "run2", Status: "running"},
			},
			want: "2 running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRunsSummary(tt.runs)
			if tt.name == "empty runs" && got != "-" {
				t.Errorf("formatRunsSummary() = %v, want %v", got, tt.want)
			}
			if tt.name == "single running" && got != "1 running" {
				t.Errorf("formatRunsSummary() = %v, want %v", got, tt.want)
			}
			if tt.name == "multiple same status" && got != "2 running" {
				t.Errorf("formatRunsSummary() = %v, want %v", got, tt.want)
			}
		})
	}
}
