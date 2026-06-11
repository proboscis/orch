package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
	filestore "github.com/s22625/orch/internal/store/file"
)

type mockPsAPI struct {
	orchapi.OrchAPI
	issuesRoot string
}

func (m *mockPsAPI) GetIssue(ctx context.Context, issueID model.IssueID) (*orchapi.Issue, error) {
	st, err := filestore.New(m.issuesRoot)
	if err != nil {
		return nil, err
	}
	issue, err := st.ResolveIssue(issueID)
	if err != nil {
		return nil, err
	}
	return &orchapi.Issue{
		ID:      issue.ID,
		Title:   issue.Title,
		Topic:   issue.Topic,
		Summary: issue.Summary,
		Status:  orchapi.IssueStatus(issue.Status),
		Tags:    issue.Tags,
		Body:    issue.Body,
		Path:    issue.Path,
	}, nil
}

func setupMockPsDeps(issuesRoot string) *psDeps {
	mock := &mockPsAPI{issuesRoot: issuesRoot}
	return &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mock, nil
		},
	}
}

type recordingPsAPI struct {
	orchapi.OrchAPI
	filter  *orchapi.ListRunsFilter
	filters []*orchapi.ListRunsFilter
	results []*orchapi.ListRunsResult
}

func (m *recordingPsAPI) ListRuns(ctx context.Context, filter *orchapi.ListRunsFilter) (*orchapi.ListRunsResult, error) {
	m.filter = filter
	m.filters = append(m.filters, filter)
	if len(m.results) == 0 {
		return &orchapi.ListRunsResult{Runs: []*orchapi.Run{}}, nil
	}
	result := m.results[0]
	m.results = m.results[1:]
	return result, nil
}

func psTestRun(issueID string, runID string, status orchapi.RunStatus, when time.Time) *orchapi.Run {
	return &orchapi.Run{
		IssueID:     model.IssueID(issueID),
		RunID:       model.RunID(runID),
		Status:      status,
		IssueStatus: "open",
		StartedAt:   when,
		UpdatedAt:   when,
	}
}

func TestParseStatusList(t *testing.T) {
	statuses, err := parseStatusList("running, blocked ,done")
	if err != nil {
		t.Fatalf("parseStatusList() error = %v", err)
	}
	want := []model.Status{model.StatusRunning, model.StatusWaiting, model.StatusDone}
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

	vault := t.TempDir()
	deps := setupMockPsDeps(vault)

	issuesDir := filepath.Join(vault, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}

	longSummary := strings.Repeat("s", 60)
	issueContent := fmt.Sprintf("---\ntype: issue\nstatus: open\nsummary: %s\n---\n# Title\n", longSummary)
	if err := os.WriteFile(filepath.Join(issuesDir, "issue-1.md"), []byte(issueContent), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	run := &model.Run{
		IssueID:   "issue-1",
		RunID:     "run-1",
		Status:    model.StatusRunning,
		UpdatedAt: time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC),
	}
	now := time.Date(2025, 1, 2, 3, 6, 0, 0, time.UTC)

	out := captureStdout(t, func() {
		if err := outputTableWithIssueInfoAndBranchStateWithDeps(
			context.Background(),
			[]*model.Run{run},
			now,
			&psOptions{},
			nil,
			nil,
			nil,
			nil,
			deps,
		); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	want := longSummary[:37] + "..."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing truncated summary %q: %q", want, out)
	}
}

func TestOutputTableUsesTopic(t *testing.T) {
	resetGlobalOpts(t)

	vault := t.TempDir()
	deps := setupMockPsDeps(vault)

	issuesDir := filepath.Join(vault, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}

	topic := "one two three four five six"
	summary := "unused-summary"
	issueContent := fmt.Sprintf("---\ntype: issue\nstatus: open\ntopic: %s\nsummary: %s\n---\n# Title\n", topic, summary)
	if err := os.WriteFile(filepath.Join(issuesDir, "issue-1.md"), []byte(issueContent), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	run := &model.Run{
		IssueID:   "issue-1",
		RunID:     "run-1",
		Status:    model.StatusRunning,
		UpdatedAt: time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC),
	}
	now := time.Date(2025, 1, 2, 3, 6, 0, 0, time.UTC)

	out := captureStdout(t, func() {
		if err := outputTableWithIssueInfoAndBranchStateWithDeps(
			context.Background(),
			[]*model.Run{run},
			now,
			&psOptions{},
			nil,
			nil,
			nil,
			nil,
			deps,
		); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	want := "one two three four five..."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing truncated topic %q: %q", want, out)
	}
	if strings.Contains(out, summary) {
		t.Fatalf("output should not include summary %q: %q", summary, out)
	}
}

func TestOutputTableTruncatesTopicChars(t *testing.T) {
	resetGlobalOpts(t)

	vault := t.TempDir()
	deps := setupMockPsDeps(vault)

	issuesDir := filepath.Join(vault, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}

	longTopic := strings.Repeat("t", 35)
	issueContent := fmt.Sprintf("---\ntype: issue\nstatus: open\ntopic: %s\n---\n# Title\n", longTopic)
	if err := os.WriteFile(filepath.Join(issuesDir, "issue-1.md"), []byte(issueContent), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}

	run := &model.Run{
		IssueID:   "issue-1",
		RunID:     "run-1",
		Status:    model.StatusRunning,
		UpdatedAt: time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC),
	}
	now := time.Date(2025, 1, 2, 3, 6, 0, 0, time.UTC)

	out := captureStdout(t, func() {
		if err := outputTableWithIssueInfoAndBranchStateWithDeps(
			context.Background(),
			[]*model.Run{run},
			now,
			&psOptions{},
			nil,
			nil,
			nil,
			nil,
			deps,
		); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	want := strings.Repeat("t", 27) + "..."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing truncated topic %q: %q", want, out)
	}
}

func TestOutputTableNoRuns(t *testing.T) {
	resetGlobalOpts(t)

	out := captureStdout(t, func() {
		if err := outputTable(nil, time.Now(), false); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	if strings.TrimSpace(out) != "No runs found" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestOutputTableShowsNewColumns(t *testing.T) {
	resetGlobalOpts(t)

	updatedAt := time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC)
	run := &model.Run{
		IssueID:   "issue-1",
		RunID:     "run-1",
		Status:    model.StatusRunning,
		PRUrl:     "http://example.com/pr/1",
		UpdatedAt: updatedAt,
	}

	out := captureStdout(t, func() {
		if err := outputTable([]*model.Run{run}, updatedAt, false); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and row output, got %q", out)
	}

	header := lines[0]
	profileIdx := strings.Index(header, "PROFILE")
	targetIdx := strings.Index(header, "TARGET")
	hostIdx := strings.Index(header, "HOST")
	agentIdx := strings.Index(header, "AGENT")
	aliveIdx := strings.Index(header, "ALIVE")
	branchIdx := strings.Index(header, "BRANCH")
	worktreeIdx := strings.Index(header, "WORKTREE")
	// "PROFILE" also contains "PR", so locate the PR column from the end.
	prIdx := strings.LastIndex(header, "PR")
	if profileIdx == -1 || targetIdx == -1 || hostIdx == -1 || agentIdx == -1 || aliveIdx == -1 || branchIdx == -1 || worktreeIdx == -1 || prIdx == -1 {
		t.Fatalf("missing columns in header: %q", header)
	}
	if !(profileIdx < targetIdx && targetIdx < hostIdx && hostIdx < agentIdx && agentIdx < aliveIdx && aliveIdx < branchIdx && branchIdx < worktreeIdx && worktreeIdx < prIdx) {
		t.Fatalf("unexpected header order: %q", header)
	}

	if !strings.Contains(lines[1], "open") {
		t.Fatalf("missing PR 'open' value in row: %q", lines[1])
	}
}

func TestOutputTableShowsPROpenForPROpenStatus(t *testing.T) {
	resetGlobalOpts(t)

	updatedAt := time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC)
	run := &model.Run{
		IssueID:   "issue-2",
		RunID:     "run-2",
		Status:    model.StatusPROpen,
		UpdatedAt: updatedAt,
	}

	out := captureStdout(t, func() {
		if err := outputTable([]*model.Run{run}, updatedAt, false); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	if !strings.Contains(out, "open") {
		t.Fatalf("missing PR 'open' value for pr_open status: %q", out)
	}
}

func TestOutputJSON(t *testing.T) {
	updatedAt := time.Date(2025, 1, 2, 3, 5, 6, 0, time.UTC)
	now := updatedAt.Add(2 * time.Minute)
	run := &model.Run{
		IssueID:      "issue-1",
		RunID:        "run-1",
		Status:       model.StatusRunning,
		Target:       "zeus",
		Branch:       "branch",
		WorktreePath: "/tmp/worktree",
		SessionName:  "session",
		PRUrl:        "http://example.com/pr/1",
		StartedAt:    time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:    updatedAt,
	}

	out := captureStdout(t, func() {
		if err := outputJSON([]*model.Run{run}, now); err != nil {
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
			CLI          string `json:"cli"`
			Target       string `json:"target"`
			TargetHost   string `json:"target_host"`
			Status       string `json:"status"`
			AgentStatus  string `json:"agent_status"`
			BranchStatus string `json:"branch_status"`
			PRStatus     string `json:"pr_status"`
			UpdatedAt    string `json:"updated_at"`
			UpdatedAgo   string `json:"updated_ago"`
			StartedAt    string `json:"started_at"`
			PRUrl        string `json:"pr_url"`
			Branch       string `json:"branch"`
			Worktree     string `json:"worktree_path"`
			SessionName  string `json:"session_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || len(got.Items) != 1 {
		t.Fatalf("unexpected response: %+v", got)
	}
	item := got.Items[0]
	if item.ShortID != string(run.ShortID()) {
		t.Fatalf("short_id = %q, want %q", item.ShortID, run.ShortID())
	}
	if item.IssueStatus != "" {
		t.Fatalf("issue_status = %q, want empty", item.IssueStatus)
	}
	if item.UpdatedAt != "2025-01-02T03:05:06Z" {
		t.Fatalf("updated_at = %q", item.UpdatedAt)
	}
	if item.UpdatedAgo != "2m ago" {
		t.Fatalf("updated_ago = %q, want %q", item.UpdatedAgo, "2m ago")
	}
	if item.AgentStatus != "run" {
		t.Fatalf("agent_status = %q, want %q", item.AgentStatus, "run")
	}
	if item.PRStatus != "open" {
		t.Fatalf("pr_status = %q, want %q", item.PRStatus, "open")
	}
	if item.Target != "zeus" {
		t.Fatalf("target = %q, want %q", item.Target, "zeus")
	}
	if item.TargetHost != "zeus" {
		t.Fatalf("target_host = %q, want %q", item.TargetHost, "zeus")
	}
}

func TestOutputJSONUsesExecutionHostWithoutTargetName(t *testing.T) {
	updatedAt := time.Date(2025, 1, 2, 3, 5, 6, 0, time.UTC)
	now := updatedAt.Add(2 * time.Minute)
	run := &model.Run{
		IssueID:      "issue-2",
		RunID:        "run-2",
		Status:       model.StatusRunning,
		TargetHost:   "mac-host",
		Branch:       "branch",
		WorktreePath: "/tmp/worktree",
		SessionName:  "session",
		StartedAt:    time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:    updatedAt,
	}

	out := captureStdout(t, func() {
		if err := outputJSON([]*model.Run{run}, now); err != nil {
			t.Fatalf("outputJSON: %v", err)
		}
	})

	var got struct {
		OK    bool `json:"ok"`
		Items []struct {
			Target     string `json:"target"`
			TargetHost string `json:"target_host"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || len(got.Items) != 1 {
		t.Fatalf("unexpected response: %+v", got)
	}
	if got.Items[0].Target != "" {
		t.Fatalf("target = %q, want empty", got.Items[0].Target)
	}
	if got.Items[0].TargetHost != "mac-host" {
		t.Fatalf("target_host = %q, want %q", got.Items[0].TargetHost, "mac-host")
	}
}

func TestOutputTableShowsExecutionHostWithoutTargetName(t *testing.T) {
	resetGlobalOpts(t)

	updatedAt := time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC)
	run := &model.Run{
		IssueID:    "issue-3",
		RunID:      "run-3",
		Status:     model.StatusRunning,
		TargetHost: "mac-host",
		UpdatedAt:  updatedAt,
	}

	out := captureStdout(t, func() {
		if err := outputTable([]*model.Run{run}, updatedAt, false); err != nil {
			t.Fatalf("outputTable: %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and row output, got %q", out)
	}
	if !strings.Contains(lines[1], "mac-host") {
		t.Fatalf("expected HOST column to include execution host, row=%q", lines[1])
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "just-now", when: now.Add(-5 * time.Second), want: "just now"},
		{name: "seconds", when: now.Add(-42 * time.Second), want: "42s ago"},
		{name: "minutes", when: now.Add(-2 * time.Minute), want: "2m ago"},
		{name: "hours", when: now.Add(-3 * time.Hour), want: "3h ago"},
		{name: "days", when: now.Add(-4 * 24 * time.Hour), want: "4d ago"},
		{name: "weeks", when: now.Add(-15 * 24 * time.Hour), want: "2w ago"},
		{name: "future", when: now.Add(5 * time.Second), want: "just now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRelativeTime(tt.when, now)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestRunPsShowsAllIssuesByDefault(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.JSON = true

	mock := &mockCaptureAllAPI{
		runs: []*orchapi.Run{
			{
				IssueID:      "issue-resolved",
				RunID:        "run-1",
				Status:       orchapi.RunStatusDone,
				IssueStatus:  string(orchapi.IssueStatusResolved),
				StartedAt:    time.Now().Add(-2 * time.Hour),
				UpdatedAt:    time.Now().Add(-time.Hour),
				AliveKnown:   true,
				WorktreePath: "/tmp/worktree-1",
			},
			{
				IssueID:      "issue-open",
				RunID:        "run-2",
				Status:       orchapi.RunStatusRunning,
				IssueStatus:  string(orchapi.IssueStatusOpen),
				StartedAt:    time.Now().Add(-90 * time.Minute),
				UpdatedAt:    time.Now().Add(-30 * time.Minute),
				AliveKnown:   true,
				WorktreePath: "/tmp/worktree-2",
			},
		},
	}
	deps := &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mock, nil
		},
	}

	out := captureStdout(t, func() {
		if err := runPsWithDeps(context.Background(), &psOptions{Limit: 10}, deps); err != nil {
			t.Fatalf("runPs: %v", err)
		}
	})

	var got struct {
		OK    bool `json:"ok"`
		Items []struct {
			RunID   string `json:"run_id"`
			IssueID string `json:"issue_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Should see runs from both open and resolved issues (no default filtering)
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 items, got %d: %#v", len(got.Items), got.Items)
	}
}

func TestRunPsAllIncludesResolvedIssues(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.JSON = true

	mock := &mockCaptureAllAPI{
		runs: []*orchapi.Run{
			{
				IssueID:      "issue-resolved",
				RunID:        "run-1",
				Status:       orchapi.RunStatusDone,
				IssueStatus:  string(orchapi.IssueStatusResolved),
				StartedAt:    time.Now().Add(-2 * time.Hour),
				UpdatedAt:    time.Now().Add(-time.Hour),
				AliveKnown:   true,
				WorktreePath: "/tmp/worktree-1",
			},
			{
				IssueID:      "issue-open",
				RunID:        "run-2",
				Status:       orchapi.RunStatusRunning,
				IssueStatus:  string(orchapi.IssueStatusOpen),
				StartedAt:    time.Now().Add(-90 * time.Minute),
				UpdatedAt:    time.Now().Add(-30 * time.Minute),
				AliveKnown:   true,
				WorktreePath: "/tmp/worktree-2",
			},
		},
	}
	deps := &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mock, nil
		},
	}

	out := captureStdout(t, func() {
		if err := runPsWithDeps(context.Background(), &psOptions{All: true, Limit: 10}, deps); err != nil {
			t.Fatalf("runPs: %v", err)
		}
	})

	var got struct {
		OK    bool `json:"ok"`
		Items []struct {
			RunID   string `json:"run_id"`
			IssueID string `json:"issue_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// With --all, should see both runs
	found := map[string]bool{}
	for _, item := range got.Items {
		found[item.IssueID] = true
	}
	if !found["issue-resolved"] || !found["issue-open"] {
		t.Fatalf("missing issues in output: %#v", found)
	}
}

func TestRunPsUsesConfiguredDefaultStatuses(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.JSON = true

	api := &recordingPsAPI{}
	deps := &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
		getConfig: func(ctx context.Context, api orchapi.OrchAPI) (*orchapi.Config, error) {
			return &orchapi.Config{
				PS: orchapi.PSConfig{
					DefaultStatuses: []string{"queued", "booting", "running", "waiting", "rate_limited", "pr_open"},
				},
			}, nil
		},
	}

	captureStdout(t, func() {
		if err := runPsWithDeps(context.Background(), &psOptions{Limit: 10}, deps); err != nil {
			t.Fatalf("runPs: %v", err)
		}
	})

	want := []orchapi.RunStatus{
		orchapi.RunStatusQueued,
		orchapi.RunStatusBooting,
		orchapi.RunStatusRunning,
		orchapi.RunStatusWaiting,
		orchapi.RunStatusRateLimited,
		orchapi.RunStatusPROpen,
	}
	if api.filter == nil || !reflect.DeepEqual(api.filter.Status, want) {
		t.Fatalf("ListRuns status filter = %#v, want %#v", api.filter, want)
	}
}

func TestRunPsShowsExcludedStatusStatsForConfiguredDefaultStatuses(t *testing.T) {
	resetGlobalOpts(t)

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	api := &recordingPsAPI{
		results: []*orchapi.ListRunsResult{
			{
				Runs: []*orchapi.Run{
					psTestRun("issue-1", "run-1", orchapi.RunStatusPROpen, now),
				},
			},
			{
				Runs: []*orchapi.Run{
					psTestRun("issue-1", "run-1", orchapi.RunStatusPROpen, now),
					psTestRun("issue-1", "run-2", orchapi.RunStatusCanceled, now),
					psTestRun("issue-1", "run-3", orchapi.RunStatusCanceled, now),
					psTestRun("issue-2", "run-4", orchapi.RunStatusDone, now),
					psTestRun("issue-3", "run-5", orchapi.RunStatusFailed, now),
				},
			},
		},
	}
	deps := &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
		getConfig: func(ctx context.Context, api orchapi.OrchAPI) (*orchapi.Config, error) {
			return &orchapi.Config{
				PS: orchapi.PSConfig{
					DefaultStatuses: []string{"queued", "booting", "running", "waiting", "rate_limited", "pr_open"},
				},
			}, nil
		},
		hasConfigScope: func() bool {
			return true
		},
	}

	out := captureStdout(t, func() {
		if err := runPsWithDeps(context.Background(), &psOptions{Limit: 10}, deps); err != nil {
			t.Fatalf("runPs: %v", err)
		}
	})

	want := "Excluded by ps.default_statuses: canceled=2 done=1 failed=1 (4 total)"
	if !strings.Contains(out, want) {
		t.Fatalf("output missing excluded stats %q:\n%s", want, out)
	}
	if len(api.filters) != 2 {
		t.Fatalf("ListRuns call count = %d, want 2", len(api.filters))
	}
	if len(api.filters[0].Status) == 0 {
		t.Fatalf("first ListRuns call missing default status filter")
	}
	if len(api.filters[1].Status) != 0 || api.filters[1].Limit != psExcludedStatsPageLimit {
		t.Fatalf("stats ListRuns filter = %#v, want unfiltered status with limit %d", api.filters[1], psExcludedStatsPageLimit)
	}
}

func TestRunPsDoesNotShowExcludedStatusStatsForExplicitStatus(t *testing.T) {
	resetGlobalOpts(t)

	api := &recordingPsAPI{}
	deps := &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
		getConfig: func(ctx context.Context, api orchapi.OrchAPI) (*orchapi.Config, error) {
			t.Fatal("getConfig should not be called when --status is explicit")
			return nil, nil
		},
	}

	out := captureStdout(t, func() {
		if err := runPsWithDeps(context.Background(), &psOptions{Status: []string{"canceled"}, Limit: 10}, deps); err != nil {
			t.Fatalf("runPs: %v", err)
		}
	})

	if strings.Contains(out, "Excluded by ps.default_statuses") {
		t.Fatalf("output should not include excluded stats:\n%s", out)
	}
	if len(api.filters) != 1 {
		t.Fatalf("ListRuns call count = %d, want 1", len(api.filters))
	}
}

func TestRunPsSkipsConfiguredDefaultStatusesWithoutProjectScope(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.JSON = true

	api := &recordingPsAPI{}
	deps := &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
		getConfig: func(ctx context.Context, api orchapi.OrchAPI) (*orchapi.Config, error) {
			t.Fatal("getConfig should not be called without project scope")
			return nil, nil
		},
		hasConfigScope: func() bool {
			return false
		},
	}

	captureStdout(t, func() {
		if err := runPsWithDeps(context.Background(), &psOptions{Limit: 10}, deps); err != nil {
			t.Fatalf("runPs: %v", err)
		}
	})

	if api.filter == nil || len(api.filter.Status) != 0 {
		t.Fatalf("ListRuns status filter = %#v, want no status filter", api.filter)
	}
}

func TestRunPsExplicitStatusOverridesConfiguredDefaultStatuses(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.JSON = true

	api := &recordingPsAPI{}
	deps := &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
		getConfig: func(ctx context.Context, api orchapi.OrchAPI) (*orchapi.Config, error) {
			t.Fatal("getConfig should not be called when --status is explicit")
			return nil, nil
		},
	}

	captureStdout(t, func() {
		if err := runPsWithDeps(context.Background(), &psOptions{Status: []string{"done"}, Limit: 10}, deps); err != nil {
			t.Fatalf("runPs: %v", err)
		}
	})

	want := []orchapi.RunStatus{orchapi.RunStatusDone}
	if api.filter == nil || !reflect.DeepEqual(api.filter.Status, want) {
		t.Fatalf("ListRuns status filter = %#v, want %#v", api.filter, want)
	}
}

func TestRunPsAllBypassesConfiguredDefaultStatuses(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.JSON = true

	api := &recordingPsAPI{}
	deps := &psDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
		getConfig: func(ctx context.Context, api orchapi.OrchAPI) (*orchapi.Config, error) {
			t.Fatal("getConfig should not be called with --all")
			return nil, nil
		},
	}

	captureStdout(t, func() {
		if err := runPsWithDeps(context.Background(), &psOptions{All: true, Limit: 10}, deps); err != nil {
			t.Fatalf("runPs: %v", err)
		}
	})

	if api.filter == nil || len(api.filter.Status) != 0 {
		t.Fatalf("ListRuns status filter = %#v, want no status filter", api.filter)
	}
}

func TestShortAgentStatus(t *testing.T) {
	tests := []struct {
		status model.Status
		want   string
	}{
		{model.StatusQueued, "queue"},
		{model.StatusBooting, "boot"},
		{model.StatusRunning, "run"},
		{model.StatusWaiting, "wait"},
		{model.StatusRateLimited, "rlimit"},
		{model.StatusPROpen, "pr"},
		{model.StatusDone, "done"},
		{model.StatusFailed, "fail"},
		{model.StatusCanceled, "cancel"},
		{model.StatusUnknown, "?"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := shortAgentStatus(tt.status)
			if got != tt.want {
				t.Fatalf("shortAgentStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestPrStatusFromRun(t *testing.T) {
	tests := []struct {
		name     string
		run      *model.Run
		gitState string
		want     string
	}{
		{
			name: "done with PR URL",
			run: &model.Run{
				Status: model.StatusDone,
				PRUrl:  "http://example.com/pr/1",
			},
			gitState: "",
			want:     "merged",
		},
		{
			name: "running with PR URL",
			run: &model.Run{
				Status: model.StatusRunning,
				PRUrl:  "http://example.com/pr/1",
			},
			gitState: "",
			want:     "open",
		},
		{
			name: "pr_open status",
			run: &model.Run{
				Status: model.StatusPROpen,
			},
			gitState: "",
			want:     "open",
		},
		{
			name: "running no PR",
			run: &model.Run{
				Status: model.StatusRunning,
			},
			gitState: "",
			want:     "-",
		},
		{
			name: "merged gitState with PR",
			run: &model.Run{
				Status: model.StatusRunning,
				PRUrl:  "http://example.com/pr/1",
			},
			gitState: "merged",
			want:     "merged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prStatusFromRun(tt.run, tt.gitState)
			if got != tt.want {
				t.Fatalf("prStatusFromRun() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBranchStatusFromGitState(t *testing.T) {
	tests := []struct {
		gitState string
		want     string
	}{
		{"clean", "clean"},
		{"dirty", "dirty"},
		{"merged", "merged"},
		{"conflict", "conflict"},
		{"uncommit", "dirty"},
		{"unknown", "-"},
		{"", "-"},
	}

	for _, tt := range tests {
		t.Run(tt.gitState, func(t *testing.T) {
			got := branchStatusFromGitState(tt.gitState)
			if got != tt.want {
				t.Fatalf("branchStatusFromGitState(%q) = %q, want %q", tt.gitState, got, tt.want)
			}
		})
	}
}

func TestOutputTableAgentColumn(t *testing.T) {
	resetGlobalOpts(t)

	updatedAt := time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC)

	tests := []struct {
		status    model.Status
		wantShort string
	}{
		{model.StatusRunning, "run"},
		{model.StatusWaiting, "wait"},
		{model.StatusDone, "done"},
		{model.StatusFailed, "fail"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			run := &model.Run{
				IssueID:   "issue-1",
				RunID:     "run-1",
				Status:    tt.status,
				UpdatedAt: updatedAt,
			}

			out := captureStdout(t, func() {
				if err := outputTable([]*model.Run{run}, updatedAt, false); err != nil {
					t.Fatalf("outputTable: %v", err)
				}
			})

			if !strings.Contains(out, tt.wantShort) {
				t.Fatalf("output missing short status %q: %q", tt.wantShort, out)
			}
		})
	}
}

func TestColorShortStatus(t *testing.T) {
	colored := colorShortStatus(model.StatusRunning)
	if !strings.HasPrefix(colored, "\033[32m") || !strings.HasSuffix(colored, "\033[0m") {
		t.Fatalf("unexpected color format: %q", colored)
	}
	if !strings.Contains(colored, "run") {
		t.Fatalf("missing short status text: %q", colored)
	}
}

func TestColorBranchStatus(t *testing.T) {
	tests := []struct {
		status    string
		wantColor string
	}{
		{"clean", "\033[32m"},
		{"dirty", "\033[33m"},
		{"merged", "\033[34m"},
		{"conflict", "\033[31m"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			colored := colorBranchStatus(tt.status)
			if !strings.HasPrefix(colored, tt.wantColor) {
				t.Fatalf("colorBranchStatus(%q) = %q, want prefix %q", tt.status, colored, tt.wantColor)
			}
		})
	}
}

func TestColorPrStatus(t *testing.T) {
	tests := []struct {
		status    string
		wantColor string
	}{
		{"open", "\033[36m"},
		{"merged", "\033[32m"},
		{"closed", "\033[90m"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			colored := colorPrStatus(tt.status)
			if !strings.HasPrefix(colored, tt.wantColor) {
				t.Fatalf("colorPrStatus(%q) = %q, want prefix %q", tt.status, colored, tt.wantColor)
			}
		})
	}
}
