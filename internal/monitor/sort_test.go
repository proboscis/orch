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
