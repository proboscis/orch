package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setIssueRootConfig(t *testing.T, issuesRoot string) {
	t.Helper()

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	configBody := []byte("issues:\n  path: " + issuesRoot + "\n")
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), configBody, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func TestRunIssueCreatePrefersExistingIssuesDir(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "Issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir Issues: %v", err)
	}

	prev := *globalOpts
	globalOpts.JSON = false
	globalOpts.Quiet = true
	setIssueRootConfig(t, vault)
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
	globalOpts.JSON = false
	globalOpts.Quiet = true
	setIssueRootConfig(t, issuesDir)
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

func TestRunIssueCreateQuotesSpecialCharacters(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}

	prev := *globalOpts
	globalOpts.JSON = false
	globalOpts.Quiet = true
	setIssueRootConfig(t, vault)
	testBypassDaemon = true
	t.Cleanup(func() {
		*globalOpts = prev
		testBypassDaemon = false
	})

	tests := []struct {
		name    string
		title   string
		summary string
		wantIn  []string
	}{
		{
			name:   "colon in title",
			title:  "Test: with colon",
			wantIn: []string{`title: "Test: with colon"`},
		},
		{
			name:    "colon in summary",
			title:   "Simple title",
			summary: "Summary: has colon",
			wantIn:  []string{"title: Simple title", `summary: "Summary: has colon"`},
		},
		{
			name:   "hash in title",
			title:  "Test # with hash",
			wantIn: []string{`title: "Test # with hash"`},
		},
		{
			name:   "simple title needs no quotes",
			title:  "Simple title",
			wantIn: []string{"title: Simple title"},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issueID := filepath.Base(t.Name()) + "-" + string(rune('a'+i))
			opts := &issueCreateOptions{Title: tt.title, Summary: tt.summary}
			if err := runIssueCreate(issueID, opts); err != nil {
				t.Fatalf("runIssueCreate: %v", err)
			}

			issuePath := filepath.Join(issuesDir, issueID+".md")
			content, err := os.ReadFile(issuePath)
			if err != nil {
				t.Fatalf("read issue file: %v", err)
			}

			for _, want := range tt.wantIn {
				if !strings.Contains(string(content), want) {
					t.Errorf("issue file missing %q\ngot:\n%s", want, content)
				}
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
