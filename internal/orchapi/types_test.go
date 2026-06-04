package orchapi

import (
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestNormalizeRunStatus(t *testing.T) {
	tests := []struct {
		input string
		want  RunStatus
	}{
		{"blocked", RunStatusWaiting},
		{"blocked_api", RunStatusRateLimited},
		{"waiting", RunStatusWaiting},
		{"rate_limited", RunStatusRateLimited},
		{"running", RunStatusRunning},
		{"done", RunStatusDone},
		{"queued", RunStatusQueued},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeRunStatus(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeRunStatus(%q) = %q, want %q", tt.input, got, tt.want)
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
		got := ComputeShortID(model.NewIssueID(c.issueID), model.NewRunID(c.runID))
		if got.String() != c.want {
			t.Errorf("ComputeShortID(%q, %q) = %q, want %q",
				c.issueID, c.runID, got, c.want)
		}
	}
}
