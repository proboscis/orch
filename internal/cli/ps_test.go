package cli

import (
	"encoding/json"
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

	summary := strings.Repeat("s", 50)
	run := &model.Run{
		IssueID:   "issue-1",
		RunID:     "run-1",
		Status:    model.StatusRunning,
		Phase:     model.PhasePlan,
		UpdatedAt: time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC),
	}

	out := captureStdout(t, func() {
		if err := outputTable([]psRun{{
			Run:          run,
			IssueStatus:  "open",
			IssueSummary: summary,
		}}); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	want := summary[:37] + "..."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing truncated summary %q: %q", want, out)
	}
}

func TestOutputTableNoRuns(t *testing.T) {
	resetGlobalOpts(t)

	out := captureStdout(t, func() {
		if err := outputTable(nil); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	if strings.TrimSpace(out) != "No runs found" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestOutputJSON(t *testing.T) {
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
		UpdatedAt:    time.Date(2025, 1, 2, 3, 5, 6, 0, time.UTC),
	}

	out := captureStdout(t, func() {
		if err := outputJSON([]psRun{{
			Run:         run,
			IssueStatus: "open",
		}}); err != nil {
			t.Fatalf("outputJSON: %v", err)
		}
	})

	var got struct {
		OK    bool `json:"ok"`
		Items []struct {
			IssueID      string `json:"issue_id"`
			IssueStatus  string `json:"issue_status"`
			RunID        string `json:"run_id"`
			ShortID      string `json:"short_id"`
			Status       string `json:"status"`
			Phase        string `json:"phase"`
			UpdatedAt    string `json:"updated_at"`
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
	if item.IssueStatus != "open" {
		t.Fatalf("issue_status = %q, want %q", item.IssueStatus, "open")
	}
	if item.UpdatedAt != "2025-01-02T03:05:06Z" {
		t.Fatalf("updated_at = %q", item.UpdatedAt)
	}
}
