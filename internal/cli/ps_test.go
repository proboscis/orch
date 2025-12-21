package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestParseStatusList(t *testing.T) {
	statuses := parseStatusList("running, blocked ,done")
	want := []model.Status{model.StatusRunning, model.StatusBlocked, model.StatusDone}
	if len(statuses) != len(want) {
		t.Fatalf("got %d statuses, want %d", len(statuses), len(want))
	}
	for i, status := range statuses {
		if status != want[i] {
			t.Fatalf("status[%d] = %q, want %q", i, status, want[i])
		}
	}
}

func TestColorStatus(t *testing.T) {
	colored := colorStatus(model.StatusRunning)
	if !strings.HasPrefix(colored, "\033[32m") || !strings.HasSuffix(colored, "\033[0m") {
		t.Fatalf("unexpected color format: %q", colored)
	}
	if !strings.Contains(colored, string(model.StatusRunning)) {
		t.Fatalf("missing status text: %q", colored)
	}

	unknown := colorStatus(model.Status("mystery"))
	if unknown != "mystery" {
		t.Fatalf("unknown status = %q, want %q", unknown, "mystery")
	}
}

func TestOutputTableTruncatesSummary(t *testing.T) {
	resetGlobalOpts(t)

	vault := t.TempDir()
	globalOpts.VaultPath = vault

	issuesDir := filepath.Join(vault, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}

	longSummary := strings.Repeat("s", 60)
	issueContent := fmt.Sprintf("---\ntype: issue\nsummary: %s\n---\n# Title\n", longSummary)
	if err := os.WriteFile(filepath.Join(issuesDir, "issue-1.md"), []byte(issueContent), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	run := &model.Run{
		IssueID:   "issue-1",
		RunID:     "run-1",
		Status:    model.StatusRunning,
		Phase:     model.PhasePlan,
		UpdatedAt: time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC),
	}
	now := time.Date(2025, 1, 2, 3, 6, 0, 0, time.UTC)

	out := captureStdout(t, func() {
		if err := outputTable([]*model.Run{run}, now, false); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	want := longSummary[:37] + "..."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing truncated summary %q: %q", want, out)
	}
}

func TestOutputTableNoRuns(t *testing.T) {
	resetGlobalOpts(t)

	out := captureStdout(t, func() {
		if err := outputTable(nil, time.Now(), false); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	if strings.TrimSpace(out) != "No runs found" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestOutputJSON(t *testing.T) {
	updatedAt := time.Date(2025, 1, 2, 3, 5, 6, 0, time.UTC)
	now := updatedAt.Add(2 * time.Minute)
	run := &model.Run{
		IssueID:      "issue-1",
		RunID:        "run-1",
		Status:       model.StatusRunning,
		Phase:        model.PhasePlan,
		Branch:       "branch",
		WorktreePath: "/tmp/worktree",
		TmuxSession:  "session",
		PRUrl:        "http://example.com/pr/1",
		StartedAt:    time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:    updatedAt,
	}

	out := captureStdout(t, func() {
		if err := outputJSON([]*model.Run{run}, now); err != nil {
			t.Fatalf("outputJSON: %v", err)
		}
	})

	var got struct {
		OK    bool `json:"ok"`
		Items []struct {
			IssueID      string `json:"issue_id"`
			RunID        string `json:"run_id"`
			ShortID      string `json:"short_id"`
			Status       string `json:"status"`
			Phase        string `json:"phase"`
			UpdatedAt    string `json:"updated_at"`
			UpdatedAgo   string `json:"updated_ago"`
			StartedAt    string `json:"started_at"`
			PRUrl        string `json:"pr_url"`
			Branch       string `json:"branch"`
			WorktreePath string `json:"worktree_path"`
			TmuxSession  string `json:"tmux_session"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || len(got.Items) != 1 {
		t.Fatalf("unexpected response: %+v", got)
	}
	item := got.Items[0]
	if item.ShortID != run.ShortID() {
		t.Fatalf("short_id = %q, want %q", item.ShortID, run.ShortID())
	}
	if item.UpdatedAt != "2025-01-02T03:05:06Z" {
		t.Fatalf("updated_at = %q", item.UpdatedAt)
	}
	if item.UpdatedAgo != "2m ago" {
		t.Fatalf("updated_ago = %q, want %q", item.UpdatedAgo, "2m ago")
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "just-now", when: now.Add(-5 * time.Second), want: "just now"},
		{name: "seconds", when: now.Add(-42 * time.Second), want: "42s ago"},
		{name: "minutes", when: now.Add(-2 * time.Minute), want: "2m ago"},
		{name: "hours", when: now.Add(-3 * time.Hour), want: "3h ago"},
		{name: "days", when: now.Add(-4 * 24 * time.Hour), want: "4d ago"},
		{name: "weeks", when: now.Add(-15 * 24 * time.Hour), want: "2w ago"},
		{name: "future", when: now.Add(5 * time.Second), want: "just now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRelativeTime(tt.when, now)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
