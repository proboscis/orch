package monitor

import (
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestSessionNameForVault(t *testing.T) {
	tests := []struct {
		name      string
		vaultPath string
		wantStart string
		wantLen   int // approximate expected length
	}{
		{
			name:      "empty path returns default",
			vaultPath: "",
			wantStart: defaultSessionName,
			wantLen:   len(defaultSessionName),
		},
		{
			name:      "simple path",
			vaultPath: "/home/user/projects/myproject",
			wantStart: "orch-myproject-",
			wantLen:   len("orch-myproject-") + 6,
		},
		{
			name:      "path with dots replaced",
			vaultPath: "/home/user/.vault",
			wantStart: "orch--vault-",
			wantLen:   len("orch--vault-") + 6,
		},
		{
			name:      "path with spaces replaced",
			vaultPath: "/home/user/my project",
			wantStart: "orch-my-project-",
			wantLen:   len("orch-my-project-") + 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sessionNameForVault(tt.vaultPath)

			if tt.vaultPath == "" {
				if result != defaultSessionName {
					t.Errorf("sessionNameForVault(%q) = %q, want %q", tt.vaultPath, result, defaultSessionName)
				}
				return
			}

			if !strings.HasPrefix(result, tt.wantStart) {
				t.Errorf("sessionNameForVault(%q) = %q, want prefix %q", tt.vaultPath, result, tt.wantStart)
			}

			// Check that it has a 6-char hash suffix
			parts := strings.Split(result, "-")
			if len(parts) < 2 {
				t.Errorf("sessionNameForVault(%q) = %q, expected at least 2 parts separated by -", tt.vaultPath, result)
				return
			}
			hash := parts[len(parts)-1]
			if len(hash) != 6 {
				t.Errorf("sessionNameForVault(%q) hash suffix = %q (len %d), want len 6", tt.vaultPath, hash, len(hash))
			}
		})
	}
}

func TestSessionNameForVaultConsistency(t *testing.T) {
	// Same path should always produce same session name
	path := "/home/user/projects/test"
	result1 := sessionNameForVault(path)
	result2 := sessionNameForVault(path)

	if result1 != result2 {
		t.Errorf("sessionNameForVault produced inconsistent results: %q vs %q", result1, result2)
	}
}

func TestSessionNameForVaultUniqueness(t *testing.T) {
	// Different paths should produce different session names
	path1 := "/home/user/project1"
	path2 := "/home/user/project2"

	result1 := sessionNameForVault(path1)
	result2 := sessionNameForVault(path2)

	if result1 == result2 {
		t.Errorf("different paths produced same session name: %q", result1)
	}
}

func TestFilterBranchesForIssue(t *testing.T) {
	now := time.Now()
	branches := map[string]time.Time{
		"issue/orch-001/run-1": now.Add(-1 * time.Hour),
		"issue/orch-001/run-2": now.Add(-30 * time.Minute),
		"issue/orch-002/run-1": now.Add(-2 * time.Hour),
		"feature/something":    now.Add(-3 * time.Hour),
		"main":                 now,
	}

	tests := []struct {
		name     string
		issueID  string
		wantLen  int
		wantName string // first branch name expected (most recent)
	}{
		{
			name:     "filters branches with issue ID",
			issueID:  "orch-001",
			wantLen:  2,
			wantName: "issue/orch-001/run-2", // Most recent
		},
		{
			name:     "case insensitive match",
			issueID:  "ORCH-001",
			wantLen:  2,
			wantName: "issue/orch-001/run-2",
		},
		{
			name:    "no matching branches",
			issueID: "orch-999",
			wantLen: 0,
		},
		{
			name:     "partial match",
			issueID:  "orch-00",
			wantLen:  3, // Both orch-001 runs and orch-002
			wantName: "issue/orch-001/run-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterBranchesForIssue(branches, tt.issueID)
			if len(result) != tt.wantLen {
				t.Errorf("filterBranchesForIssue() got %d branches, want %d", len(result), tt.wantLen)
			}
			if tt.wantLen > 0 && result[0].name != tt.wantName {
				t.Errorf("filterBranchesForIssue() first branch = %q, want %q", result[0].name, tt.wantName)
			}
		})
	}
}

func TestFilterBranchesForIssueSorting(t *testing.T) {
	now := time.Now()
	branches := map[string]time.Time{
		"issue/test-001/old":    now.Add(-24 * time.Hour),
		"issue/test-001/newest": now,
		"issue/test-001/middle": now.Add(-1 * time.Hour),
	}

	result := filterBranchesForIssue(branches, "test-001")
	if len(result) != 3 {
		t.Fatalf("expected 3 branches, got %d", len(result))
	}

	// Should be sorted by commit time descending (most recent first)
	expected := []string{"issue/test-001/newest", "issue/test-001/middle", "issue/test-001/old"}
	for i, want := range expected {
		if result[i].name != want {
			t.Errorf("branch[%d] = %q, want %q", i, result[i].name, want)
		}
	}
}

func TestDashboardRenderCapture(t *testing.T) {
	tests := []struct {
		name           string
		runs           []RunRow
		cursor         int
		captureRunID   string
		captureContent string
		wantEmpty      bool
		wantContains   []string
	}{
		{
			name:      "empty runs returns empty",
			runs:      nil,
			cursor:    0,
			wantEmpty: true,
		},
		{
			name:      "cursor out of bounds returns empty",
			runs:      []RunRow{{Index: 0}},
			cursor:    5,
			wantEmpty: true,
		},
		{
			name:   "run without run data returns empty",
			runs:   []RunRow{{Index: 0, Run: nil}},
			cursor: 0,
			wantEmpty: true,
		},
		{
			name:   "no capture content shows message",
			runs:   []RunRow{{Index: 0, IssueID: "test-001", Run: &model.Run{IssueID: "test-001", RunID: "20231225-120000"}}},
			cursor: 0,
			captureRunID:   "",
			captureContent: "",
			wantContains:   []string{"CAPTURE", "No capture available"},
		},
		{
			name:   "capture content for different run shows message",
			runs:   []RunRow{{Index: 0, IssueID: "test-001", Run: &model.Run{IssueID: "test-001", RunID: "20231225-120000"}}},
			cursor: 0,
			captureRunID:   "test-002#20231225-130000",
			captureContent: "some content",
			wantContains:   []string{"CAPTURE", "No capture available"},
		},
		{
			name:   "capture content for current run is displayed",
			runs:   []RunRow{{Index: 0, IssueID: "test-001", Run: &model.Run{IssueID: "test-001", RunID: "20231225-120000"}}},
			cursor: 0,
			captureRunID:   "test-001#20231225-120000",
			captureContent: "Hello from tmux pane",
			wantContains:   []string{"CAPTURE", "Hello from tmux pane"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Dashboard{
				runs:           tt.runs,
				cursor:         tt.cursor,
				captureRunID:   tt.captureRunID,
				captureContent: tt.captureContent,
				width:          120,
				height:         80, // Larger height to accommodate capture pane
				styles:         DefaultStyles(),
			}

			result := d.renderCapture()

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("renderCapture() = %q, want empty", result)
				}
				return
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("renderCapture() = %q, want to contain %q", result, want)
				}
			}
		})
	}
}
