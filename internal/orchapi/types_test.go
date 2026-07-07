package orchapi

import (
	"testing"

	"github.com/proboscis/orch/internal/model"
)

func TestNormalizeRunStatus(t *testing.T) {
	tests := []struct {
		input string
		want  RunStatus
	}{
		{"blocked", RunStatusWaiting},
		{"blocked_api", RunStatusRateLimited},
		{"queued", RunStatusQueued},
		{"booting", RunStatusBooting},
		{"waiting", RunStatusWaiting},
		{"rate_limited", RunStatusRateLimited},
		{"running", RunStatusRunning},
		{"pr_open", RunStatusPROpen},
		{"done", RunStatusDone},
		{"failed", RunStatusFailed},
		{"canceled", RunStatusCanceled},
		{"unknown", RunStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeRunStatus(tt.input)
			if err != nil {
				t.Fatalf("NormalizeRunStatus(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeRunStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeRunStatusRoundTripsDeclaredStatuses(t *testing.T) {
	statuses := []RunStatus{
		RunStatusQueued,
		RunStatusBooting,
		RunStatusRunning,
		RunStatusWaiting,
		RunStatusRateLimited,
		RunStatusPROpen,
		RunStatusDone,
		RunStatusFailed,
		RunStatusCanceled,
		RunStatusUnknown,
	}
	for _, want := range statuses {
		t.Run(string(want), func(t *testing.T) {
			got, err := NormalizeRunStatus(string(want))
			if err != nil {
				t.Fatalf("NormalizeRunStatus(%q) error = %v", want, err)
			}
			if got != want {
				t.Fatalf("NormalizeRunStatus(%q) = %q, want %q", want, got, want)
			}
		})
	}
}

func TestNormalizeRunStatusRejectsUnknown(t *testing.T) {
	for _, input := range []string{"", "bogus", "fail", "cancel", "dry_run"} {
		t.Run(input, func(t *testing.T) {
			if got, err := NormalizeRunStatus(input); err == nil {
				t.Fatalf("NormalizeRunStatus(%q) = %q, want error", input, got)
			}
		})
	}
}

func TestComputeShortID(t *testing.T) {
	// Equality with the daemon's computeShortID (FNV-1a over "issue#run")
	// is critical: callers using ComputeShortID must match the short hex
	// that ps/show display for the same run.
	cases := []struct {
		issueID string
		runID   string
		want    string
	}{
		{"TRD-081", "20260425-145017", "6c8807"},
		{"TRD-081", "20260425-135017", "2fdb42"},
	}
	for _, c := range cases {
		got := ComputeShortID(model.IssueID(c.issueID), model.RunID(c.runID))
		if got != model.ShortID(c.want) {
			t.Errorf("ComputeShortID(%q, %q) = %q, want %q",
				c.issueID, c.runID, got, c.want)
		}
	}
}
