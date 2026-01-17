package monitor

import (
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestParseSortKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback SortKey
		want     SortKey
		wantErr  bool
	}{
		{name: "empty uses fallback", input: "", fallback: SortByUpdated, want: SortByUpdated},
		{name: "name", input: "name", fallback: SortByUpdated, want: SortByName},
		{name: "updated", input: "updated", fallback: SortByName, want: SortByUpdated},
		{name: "status", input: "status", fallback: SortByName, want: SortByStatus},
		{name: "alias id", input: "id", fallback: SortByUpdated, want: SortByName},
		{name: "started", input: "started", fallback: SortByUpdated, want: SortByStarted},
		{name: "issue", input: "issue", fallback: SortByUpdated, want: SortByIssue},
		{name: "agent", input: "agent", fallback: SortByUpdated, want: SortByAgent},
		{name: "elapsed", input: "elapsed", fallback: SortByUpdated, want: SortByElapsed},
		{name: "title", input: "title", fallback: SortByName, want: SortByTitle},
		{name: "priority", input: "priority", fallback: SortByName, want: SortByPriority},
		{name: "invalid", input: "nope", fallback: SortByUpdated, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSortKey(tt.input, tt.fallback)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseSortKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNextRunSortKey(t *testing.T) {
	tests := []struct {
		current SortKey
		want    SortKey
	}{
		{SortByUpdated, SortByStarted},
		{SortByStarted, SortByStatus},
		{SortByStatus, SortByIssue},
		{SortByIssue, SortByAgent},
		{SortByAgent, SortByElapsed},
		{SortByElapsed, SortByUpdated},
	}

	for _, tt := range tests {
		t.Run(string(tt.current), func(t *testing.T) {
			got := NextRunSortKey(tt.current)
			if got != tt.want {
				t.Fatalf("NextRunSortKey(%q) = %q, want %q", tt.current, got, tt.want)
			}
		})
	}
}

func TestNextIssueSortKey(t *testing.T) {
	tests := []struct {
		current SortKey
		want    SortKey
	}{
		{SortByName, SortByStatus},
		{SortByStatus, SortByTitle},
		{SortByTitle, SortByPriority},
		{SortByPriority, SortByUpdated},
		{SortByUpdated, SortByName},
	}

	for _, tt := range tests {
		t.Run(string(tt.current), func(t *testing.T) {
			got := NextIssueSortKey(tt.current)
			if got != tt.want {
				t.Fatalf("NextIssueSortKey(%q) = %q, want %q", tt.current, got, tt.want)
			}
		})
	}
}

func TestSortIndicator(t *testing.T) {
	if got := SortIndicator(SortAsc); got != "▲" {
		t.Fatalf("SortIndicator(SortAsc) = %q, want ▲", got)
	}
	if got := SortIndicator(SortDesc); got != "▼" {
		t.Fatalf("SortIndicator(SortDesc) = %q, want ▼", got)
	}
}

func TestSortRunRows(t *testing.T) {
	base := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	updatedOld := base
	updatedMid := base.Add(1 * time.Hour)
	updatedNew := base.Add(2 * time.Hour)

	rows := []RunRow{
		{
			IssueID: "b",
			ShortID: "b1",
			Status:  model.StatusBlocked,
			Updated: updatedMid,
			Run:     &model.Run{IssueID: "b", RunID: "002"},
		},
		{
			IssueID: "a",
			ShortID: "a1",
			Status:  model.StatusRunning,
			Updated: updatedNew,
			Run:     &model.Run{IssueID: "a", RunID: "001"},
		},
		{
			IssueID: "a",
			ShortID: "a0",
			Status:  model.StatusDone,
			Updated: updatedOld,
			Run:     &model.Run{IssueID: "a", RunID: "000"},
		},
	}

	sortRunRows(rows, SortByUpdated)
	if rows[0].Status != model.StatusRunning || rows[1].Status != model.StatusBlocked || rows[2].Status != model.StatusDone {
		t.Fatalf("SortByUpdated order mismatch: got %v, %v, %v", rows[0].Status, rows[1].Status, rows[2].Status)
	}
	if rows[0].Index != 1 || rows[1].Index != 2 || rows[2].Index != 3 {
		t.Fatalf("SortByUpdated index mismatch: got %d, %d, %d", rows[0].Index, rows[1].Index, rows[2].Index)
	}

	sortRunRows(rows, SortByName)
	if rows[0].Run.RunID != "000" || rows[1].Run.RunID != "001" || rows[2].Run.RunID != "002" {
		t.Fatalf("SortByName order mismatch: got %s, %s, %s", rows[0].Run.RunID, rows[1].Run.RunID, rows[2].Run.RunID)
	}

	rows = []RunRow{
		{
			IssueID: "c",
			ShortID: "c1",
			Status:  model.StatusRunning,
			Updated: updatedOld,
			Run:     &model.Run{IssueID: "c", RunID: "003"},
		},
		{
			IssueID: "b",
			ShortID: "b1",
			Status:  model.StatusBlocked,
			Updated: updatedMid,
			Run:     &model.Run{IssueID: "b", RunID: "002"},
		},
		{
			IssueID: "a",
			ShortID: "a1",
			Status:  model.StatusRunning,
			Updated: updatedNew,
			Run:     &model.Run{IssueID: "a", RunID: "001"},
		},
		{
			IssueID: "d",
			ShortID: "d1",
			Status:  model.StatusDone,
			Updated: updatedNew,
			Run:     &model.Run{IssueID: "d", RunID: "004"},
		},
	}

	sortRunRows(rows, SortByStatus)
	if rows[0].IssueID != "a" || rows[1].IssueID != "c" || rows[2].IssueID != "b" || rows[3].IssueID != "d" {
		t.Fatalf("SortByStatus order mismatch: got %s, %s, %s, %s", rows[0].IssueID, rows[1].IssueID, rows[2].IssueID, rows[3].IssueID)
	}
}

func TestSortRunRowsByStarted(t *testing.T) {
	base := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	startedOld := base
	startedMid := base.Add(1 * time.Hour)
	startedNew := base.Add(2 * time.Hour)

	rows := []RunRow{
		{IssueID: "a", ShortID: "a1", Started: startedOld, Run: &model.Run{IssueID: "a", RunID: "001"}},
		{IssueID: "b", ShortID: "b1", Started: startedNew, Run: &model.Run{IssueID: "b", RunID: "002"}},
		{IssueID: "c", ShortID: "c1", Started: startedMid, Run: &model.Run{IssueID: "c", RunID: "003"}},
	}

	sortRunRows(rows, SortByStarted)
	if rows[0].IssueID != "b" || rows[1].IssueID != "c" || rows[2].IssueID != "a" {
		t.Fatalf("SortByStarted order mismatch: got %s, %s, %s", rows[0].IssueID, rows[1].IssueID, rows[2].IssueID)
	}
}

func TestSortRunRowsByIssue(t *testing.T) {
	rows := []RunRow{
		{IssueID: "orch-150", ShortID: "a1", Run: &model.Run{IssueID: "orch-150", RunID: "001"}},
		{IssueID: "orch-100", ShortID: "b1", Run: &model.Run{IssueID: "orch-100", RunID: "002"}},
		{IssueID: "orch-200", ShortID: "c1", Run: &model.Run{IssueID: "orch-200", RunID: "003"}},
	}

	sortRunRows(rows, SortByIssue)
	if rows[0].IssueID != "orch-100" || rows[1].IssueID != "orch-150" || rows[2].IssueID != "orch-200" {
		t.Fatalf("SortByIssue order mismatch: got %s, %s, %s", rows[0].IssueID, rows[1].IssueID, rows[2].IssueID)
	}
}

func TestSortRunRowsByAgent(t *testing.T) {
	base := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)

	rows := []RunRow{
		{IssueID: "a", ShortID: "a1", Agent: "opencode", Updated: base, Run: &model.Run{IssueID: "a", RunID: "001"}},
		{IssueID: "b", ShortID: "b1", Agent: "claude", Updated: base, Run: &model.Run{IssueID: "b", RunID: "002"}},
		{IssueID: "c", ShortID: "c1", Agent: "gemini", Updated: base, Run: &model.Run{IssueID: "c", RunID: "003"}},
	}

	sortRunRows(rows, SortByAgent)
	if rows[0].Agent != "claude" || rows[1].Agent != "gemini" || rows[2].Agent != "opencode" {
		t.Fatalf("SortByAgent order mismatch: got %s, %s, %s", rows[0].Agent, rows[1].Agent, rows[2].Agent)
	}
}

func TestSortIssueRows(t *testing.T) {
	base := time.Date(2024, 1, 2, 9, 0, 0, 0, time.UTC)
	updatedOld := base
	updatedMid := base.Add(2 * time.Hour)
	updatedNew := base.Add(4 * time.Hour)

	rows := []IssueRow{
		{ID: "orch-2", Status: string(model.IssueStatusResolved), LatestUpdated: updatedMid},
		{ID: "orch-1", Status: string(model.IssueStatusOpen), LatestUpdated: updatedOld},
		{ID: "orch-3", Status: string(model.IssueStatusClosed), LatestUpdated: updatedNew},
	}

	sortIssueRows(rows, SortByStatus)
	if rows[0].Status != string(model.IssueStatusOpen) || rows[1].Status != string(model.IssueStatusResolved) || rows[2].Status != string(model.IssueStatusClosed) {
		t.Fatalf("SortByStatus order mismatch: got %s, %s, %s", rows[0].Status, rows[1].Status, rows[2].Status)
	}

	sortIssueRows(rows, SortByName)
	if rows[0].ID != "orch-1" || rows[1].ID != "orch-2" || rows[2].ID != "orch-3" {
		t.Fatalf("SortByName order mismatch: got %s, %s, %s", rows[0].ID, rows[1].ID, rows[2].ID)
	}

	rows = []IssueRow{
		{ID: "orch-2", Status: string(model.IssueStatusResolved), LatestUpdated: updatedMid},
		{ID: "orch-4", Status: string(model.IssueStatusOpen)},
		{ID: "orch-1", Status: string(model.IssueStatusOpen), LatestUpdated: updatedOld},
		{ID: "orch-3", Status: string(model.IssueStatusClosed), LatestUpdated: updatedNew},
	}
	sortIssueRows(rows, SortByUpdated)
	if rows[0].ID != "orch-3" || rows[1].ID != "orch-2" || rows[2].ID != "orch-1" || rows[3].ID != "orch-4" {
		t.Fatalf("SortByUpdated order mismatch: got %s, %s, %s, %s", rows[0].ID, rows[1].ID, rows[2].ID, rows[3].ID)
	}
	if rows[0].Index != 1 || rows[3].Index != 4 {
		t.Fatalf("SortByUpdated index mismatch: got %d..%d", rows[0].Index, rows[3].Index)
	}
}

func TestSortIssueRowsByTitle(t *testing.T) {
	rows := []IssueRow{
		{ID: "orch-1", Summary: "Zebra feature", Issue: &model.Issue{ID: "orch-1", Title: "Zebra feature"}},
		{ID: "orch-2", Summary: "Alpha bug", Issue: &model.Issue{ID: "orch-2", Title: "Alpha bug"}},
		{ID: "orch-3", Summary: "Beta improvement", Issue: &model.Issue{ID: "orch-3", Title: "Beta improvement"}},
	}

	sortIssueRows(rows, SortByTitle)
	if rows[0].ID != "orch-2" || rows[1].ID != "orch-3" || rows[2].ID != "orch-1" {
		t.Fatalf("SortByTitle order mismatch: got %s, %s, %s", rows[0].ID, rows[1].ID, rows[2].ID)
	}
}

func TestSortIssueRowsByPriority(t *testing.T) {
	base := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)

	rows := []IssueRow{
		{ID: "orch-1", LatestUpdated: base, Issue: &model.Issue{ID: "orch-1", Frontmatter: map[string]string{"priority": "low"}}},
		{ID: "orch-2", LatestUpdated: base, Issue: &model.Issue{ID: "orch-2", Frontmatter: map[string]string{"priority": "critical"}}},
		{ID: "orch-3", LatestUpdated: base, Issue: &model.Issue{ID: "orch-3", Frontmatter: map[string]string{"priority": "high"}}},
		{ID: "orch-4", LatestUpdated: base, Issue: &model.Issue{ID: "orch-4", Frontmatter: map[string]string{"priority": "medium"}}},
	}

	sortIssueRows(rows, SortByPriority)
	wantOrder := []string{"orch-2", "orch-3", "orch-4", "orch-1"}
	for i, want := range wantOrder {
		if rows[i].ID != want {
			t.Fatalf("SortByPriority order mismatch at %d: got %s, want %s", i, rows[i].ID, want)
		}
	}
}

func TestIssuePriorityRank(t *testing.T) {
	tests := []struct {
		priority string
		want     int
	}{
		{"critical", 0},
		{"CRITICAL", 0},
		{"high", 1},
		{"medium", 2},
		{"low", 3},
		{"", 4},
		{"unknown", 5},
	}

	for _, tt := range tests {
		t.Run(tt.priority, func(t *testing.T) {
			got := issuePriorityRank(tt.priority)
			if got != tt.want {
				t.Fatalf("issuePriorityRank(%q) = %d, want %d", tt.priority, got, tt.want)
			}
		})
	}
}
