package monitor

import (
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
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

type mockStore struct {
	vaultPath string
}

func (m *mockStore) VaultPath() string                                              { return m.vaultPath }
func (m *mockStore) ListRuns(_ *store.ListRunsFilter) ([]*model.Run, error)         { return nil, nil }
func (m *mockStore) GetRun(_ *model.RunRef) (*model.Run, error)                     { return nil, nil }
func (m *mockStore) GetRunByShortID(_ string) (*model.Run, error)                   { return nil, nil }
func (m *mockStore) GetLatestRun(_ string) (*model.Run, error)                      { return nil, nil }
func (m *mockStore) ListIssues() ([]*model.Issue, error)                            { return nil, nil }
func (m *mockStore) ResolveIssue(_ string) (*model.Issue, error)                    { return nil, nil }
func (m *mockStore) SetIssueStatus(_ string, _ model.IssueStatus) error             { return nil }
func (m *mockStore) CreateRun(_ string, _ string, _ map[string]string) (*model.Run, error) {
	return nil, nil
}
func (m *mockStore) AppendEvent(_ *model.RunRef, _ *model.Event) error { return nil }

func TestMonitorRepoName(t *testing.T) {
	tests := []struct {
		name      string
		vaultPath string
		want      string
	}{
		{
			name:      "empty vault path returns empty",
			vaultPath: "",
			want:      "",
		},
		{
			name:      "typical vault path extracts repo name",
			vaultPath: "/home/user/projects/myrepo/.orch/vault",
			want:      "myrepo",
		},
		{
			name:      "repo with dots",
			vaultPath: "/home/user/my.project/.orch/vault",
			want:      "my.project",
		},
		{
			name:      "repo with hyphens",
			vaultPath: "/home/user/my-project/.orch/vault",
			want:      "my-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Monitor{
				store: &mockStore{vaultPath: tt.vaultPath},
			}
			result := m.RepoName()
			if result != tt.want {
				t.Errorf("RepoName() = %q, want %q", result, tt.want)
			}
		})
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
		name         string
		runs         []RunRow
		cursor       int
		capture      captureState
		height       int
		wantEmpty    bool
		wantContains []string
	}{
		{
			name:      "zero height returns empty",
			runs:      nil,
			cursor:    0,
			height:    0,
			wantEmpty: true,
		},
		{
			name:         "no runs shows header only with no capture",
			runs:         nil,
			cursor:       0,
			height:       10,
			wantContains: []string{"CAPTURE", "No capture available"},
		},
		{
			name:         "with selected run shows run ref in header",
			runs:         []RunRow{{Index: 0, IssueID: "test-001", Run: &model.Run{IssueID: "test-001", RunID: "20231225-120000"}}},
			cursor:       0,
			height:       10,
			wantContains: []string{"CAPTURE", "test-001#20231225-120000"},
		},
		{
			name:    "no capture content shows message",
			runs:    []RunRow{{Index: 0, IssueID: "test-001", Run: &model.Run{IssueID: "test-001", RunID: "20231225-120000"}}},
			cursor:  0,
			capture: captureState{runRef: "", content: "", message: ""},
			height:  10,
			wantContains: []string{"CAPTURE", "No capture available"},
		},
		{
			name:    "capture message is displayed",
			runs:    []RunRow{{Index: 0, IssueID: "test-001", Run: &model.Run{IssueID: "test-001", RunID: "20231225-120000"}}},
			cursor:  0,
			capture: captureState{message: "Session not found"},
			height:  10,
			wantContains: []string{"CAPTURE", "Session not found"},
		},
		{
			name:    "capture content is displayed",
			runs:    []RunRow{{Index: 0, IssueID: "test-001", Run: &model.Run{IssueID: "test-001", RunID: "20231225-120000"}}},
			cursor:  0,
			capture: captureState{runRef: "test-001#20231225-120000", content: "Hello from tmux pane"},
			height:  10,
			wantContains: []string{"CAPTURE", "Hello from tmux pane"},
		},
		{
			name:    "loading state shows loading message",
			runs:    []RunRow{{Index: 0, IssueID: "test-001", Run: &model.Run{IssueID: "test-001", RunID: "20231225-120000"}}},
			cursor:  0,
			capture: captureState{loading: true},
			height:  10,
			wantContains: []string{"CAPTURE", "Loading capture"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Dashboard{
				runs:    tt.runs,
				cursor:  tt.cursor,
				capture: tt.capture,
				width:   120,
				height:  80,
				styles:  DefaultStyles(),
			}

			result := d.renderCapture(tt.height)

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
