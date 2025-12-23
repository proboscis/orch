package monitor

import (
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestParseFilterDuration(t *testing.T) {
	tests := []struct {
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{raw: "24h", want: 24 * time.Hour},
		{raw: "7d", want: 7 * 24 * time.Hour},
		{raw: "2w", want: 14 * 24 * time.Hour},
		{raw: "0h", wantErr: true},
		{raw: "bad", wantErr: true},
	}

	for _, tt := range tests {
		got, err := parseFilterDuration(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseFilterDuration(%q) expected error, got nil", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseFilterDuration(%q) unexpected error: %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("parseFilterDuration(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

func TestCompileIssueQuery(t *testing.T) {
	re, isRegex, err := compileIssueQuery("/orch-\\d+/")
	if err != nil {
		t.Fatalf("compileIssueQuery returned error: %v", err)
	}
	if !isRegex {
		t.Fatal("compileIssueQuery expected regex true")
	}
	if re == nil || !re.MatchString("orch-123") {
		t.Fatal("compiled regex did not match expected input")
	}
	if re.MatchString("misc") {
		t.Fatal("compiled regex matched unexpected input")
	}
}

func TestRunFilterRows(t *testing.T) {
	now := time.Now()
	rows := []RunRow{
		{
			IssueID:     "orch-1",
			Status:      model.StatusRunning,
			Agent:       "codex",
			PR:          "#12",
			Merged:      "clean",
			Updated:     now.Add(-1 * time.Hour),
			IssueStatus: "open",
			Run:         &model.Run{Agent: "codex"},
		},
		{
			IssueID:     "orch-2",
			Status:      model.StatusBlocked,
			Agent:       "claude",
			PR:          "-",
			Merged:      "conflict",
			Updated:     now.Add(-48 * time.Hour),
			IssueStatus: "resolved",
			Run:         &model.Run{Agent: "claude"},
		},
		{
			IssueID:     "misc",
			Status:      model.StatusDone,
			Agent:       "gemini",
			PR:          "-",
			Merged:      "no change",
			Updated:     now.Add(-2 * time.Hour),
			IssueStatus: "open",
			Run:         &model.Run{Agent: "gemini"},
		},
	}

	filter := DefaultRunFilter()
	filter.Statuses = map[model.Status]bool{
		model.StatusRunning: true,
		model.StatusBlocked: true,
	}
	filter.Agent = "codex"
	filter.Merged = mergedFilterClean
	filter.PR = prFilterHas
	filter.IssueStatus = issueStatusOpen
	filter.IssueQuery = "orch"
	filter.UpdatedWithin = 24 * time.Hour

	filtered := filter.FilterRows(rows, now)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 row, got %d", len(filtered))
	}
	if filtered[0].IssueID != "orch-1" {
		t.Fatalf("expected orch-1, got %s", filtered[0].IssueID)
	}
}

func TestRunFilterRowsRegex(t *testing.T) {
	now := time.Now()
	re, _, err := compileIssueQuery("/orch-\\d+/")
	if err != nil {
		t.Fatalf("compileIssueQuery error: %v", err)
	}
	filter := DefaultRunFilter()
	filter.Statuses = map[model.Status]bool{model.StatusRunning: true}
	filter.IssueQuery = "/orch-\\d+/"
	filter.IssueRegex = re

	rows := []RunRow{
		{IssueID: "orch-123", Status: model.StatusRunning, Updated: now},
		{IssueID: "misc", Status: model.StatusRunning, Updated: now},
	}

	filtered := filter.FilterRows(rows, now)
	if len(filtered) != 1 || filtered[0].IssueID != "orch-123" {
		t.Fatalf("expected regex filter to match orch-123, got %+v", filtered)
	}
}
