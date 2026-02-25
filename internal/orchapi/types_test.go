package orchapi

import "testing"

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
