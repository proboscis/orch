package cli

import (
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"1m", 30 * 24 * time.Hour},
	}

	for _, tc := range cases {
		got, err := parseDuration(tc.input)
		if err != nil {
			t.Fatalf("parseDuration(%q) error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("parseDuration(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseDurationInvalid(t *testing.T) {
	if _, err := parseDuration("7x"); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestParseStatus(t *testing.T) {
	if _, err := parseStatus(string(model.StatusDone)); err != nil {
		t.Fatalf("parseStatus done error: %v", err)
	}
	if _, err := parseStatus(string(model.StatusRunning)); err == nil {
		t.Fatal("expected error for running status")
	}
}

func TestFilterByAge(t *testing.T) {
	now := time.Now()
	runs := []*model.Run{
		{IssueID: "old", RunID: "1", UpdatedAt: now.Add(-10 * 24 * time.Hour)},
		{IssueID: "new", RunID: "2", UpdatedAt: now.Add(-2 * 24 * time.Hour)},
	}

	filtered, err := filterByAge(runs, "7d")
	if err != nil {
		t.Fatalf("filterByAge error: %v", err)
	}
	if len(filtered) != 1 || filtered[0].IssueID != "old" {
		t.Fatalf("unexpected filter result: %#v", filtered)
	}
}
