package cli

import (
	"context"
	"reflect"
	"testing"

	"github.com/s22625/orch/internal/orchapi"
	"github.com/s22625/orch/internal/testutil"
)

func TestCleanStatusFilterDefaultsToFailedAndCanceled(t *testing.T) {
	got, err := cleanStatusFilter("")
	if err != nil {
		t.Fatalf("cleanStatusFilter() error = %v", err)
	}

	want := []orchapi.RunStatus{orchapi.RunStatusFailed, orchapi.RunStatusCanceled}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanStatusFilter() = %v, want %v", got, want)
	}
}

func TestCleanStatusFilterSupportsAliasesAndCommaSeparatedValues(t *testing.T) {
	got, err := cleanStatusFilter("stopped, done, cancelled")
	if err != nil {
		t.Fatalf("cleanStatusFilter() error = %v", err)
	}

	want := []orchapi.RunStatus{orchapi.RunStatusCanceled, orchapi.RunStatusDone}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanStatusFilter() = %v, want %v", got, want)
	}
}

func TestCleanStatusFilterRejectsActiveStatuses(t *testing.T) {
	if _, err := cleanStatusFilter("running"); err == nil {
		t.Fatal("expected error for active status")
	}
}

func TestRunCleanByAgeUsesDefaultTerminalStatuses(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true

	api := &testutil.OrchAPIMock{
		ListRunsFunc: func(ctx context.Context, filter *orchapi.ListRunsFilter) (*orchapi.ListRunsResult, error) {
			want := []orchapi.RunStatus{orchapi.RunStatusFailed, orchapi.RunStatusCanceled}
			if !reflect.DeepEqual(filter.Status, want) {
				t.Fatalf("ListRuns filter.Status = %v, want %v", filter.Status, want)
			}
			if filter.OlderThan == "" {
				t.Fatal("ListRuns filter.OlderThan should be set")
			}
			return &orchapi.ListRunsResult{
				Runs: []*orchapi.Run{
					{
						IssueID:      "orch-1",
						RunID:        "20260101-010101",
						ShortID:      "abc123",
						Status:       orchapi.RunStatusFailed,
						WorktreePath: "/tmp/worktree-a",
					},
				},
			}, nil
		},
		CleanRunWorktreeFunc: func(ctx context.Context, ref orchapi.RunRef) (*orchapi.CleanRunWorktreeResult, error) {
			return &orchapi.CleanRunWorktreeResult{
				IssueID:         ref.IssueID,
				RunID:           ref.RunID,
				ShortID:         "abc123",
				WorktreePath:    "/tmp/worktree-a",
				WorktreeRemoved: true,
			}, nil
		},
	}

	deps := &cleanDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
	}

	if err := runCleanCommand(context.Background(), nil, &cleanOptions{
		OlderThan: "7d",
		Force:     true,
	}, deps); err != nil {
		t.Fatalf("runCleanCommand() error = %v", err)
	}

	if got := api.CleanRunWorktreeCalls(); len(got) != 1 {
		t.Fatalf("CleanRunWorktreeCalls() len = %d, want 1", len(got))
	}
}

func TestRunCleanIssueOnlyTargetsLatestMatchingRun(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true

	api := &testutil.OrchAPIMock{
		ListRunsFunc: func(ctx context.Context, filter *orchapi.ListRunsFilter) (*orchapi.ListRunsResult, error) {
			if filter.IssueID != "orch-issue" {
				t.Fatalf("ListRuns filter.IssueID = %q, want %q", filter.IssueID, "orch-issue")
			}
			if filter.Limit != 1 {
				t.Fatalf("ListRuns filter.Limit = %d, want 1", filter.Limit)
			}
			want := []orchapi.RunStatus{orchapi.RunStatusFailed, orchapi.RunStatusCanceled}
			if !reflect.DeepEqual(filter.Status, want) {
				t.Fatalf("ListRuns filter.Status = %v, want %v", filter.Status, want)
			}
			return &orchapi.ListRunsResult{
				Runs: []*orchapi.Run{
					{
						IssueID:      "orch-issue",
						RunID:        "20260102-020202",
						ShortID:      "def456",
						Status:       orchapi.RunStatusFailed,
						WorktreePath: "/tmp/worktree-b",
					},
				},
			}, nil
		},
		ResolveRunFunc: func(ctx context.Context, ref orchapi.RunRef) (*orchapi.Run, error) {
			t.Fatal("ResolveRun should not be used for bare ISSUE_ID cleanup")
			return nil, nil
		},
		CleanRunWorktreeFunc: func(ctx context.Context, ref orchapi.RunRef) (*orchapi.CleanRunWorktreeResult, error) {
			return &orchapi.CleanRunWorktreeResult{
				IssueID:         ref.IssueID,
				RunID:           ref.RunID,
				ShortID:         "def456",
				WorktreePath:    "/tmp/worktree-b",
				WorktreeRemoved: true,
			}, nil
		},
	}

	deps := &cleanDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
	}

	if err := runCleanCommand(context.Background(), []string{"orch-issue"}, &cleanOptions{
		Force: true,
	}, deps); err != nil {
		t.Fatalf("runCleanCommand() error = %v", err)
	}

	if got := api.CleanRunWorktreeCalls(); len(got) != 1 {
		t.Fatalf("CleanRunWorktreeCalls() len = %d, want 1", len(got))
	}
	if got := api.CleanRunWorktreeCalls()[0].Ref; got.IssueID != "orch-issue" || got.RunID != "20260102-020202" {
		t.Fatalf("CleanRunWorktree ref = %#v, want orch-issue#20260102-020202", got)
	}
}

func TestRunCleanAllWithoutIssueTargetsAllMatchingRuns(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true

	api := &testutil.OrchAPIMock{
		ListRunsFunc: func(ctx context.Context, filter *orchapi.ListRunsFilter) (*orchapi.ListRunsResult, error) {
			if filter.IssueID != "" {
				t.Fatalf("ListRuns filter.IssueID = %q, want empty", filter.IssueID)
			}
			want := []orchapi.RunStatus{orchapi.RunStatusFailed, orchapi.RunStatusCanceled}
			if !reflect.DeepEqual(filter.Status, want) {
				t.Fatalf("ListRuns filter.Status = %v, want %v", filter.Status, want)
			}
			return &orchapi.ListRunsResult{
				Runs: []*orchapi.Run{
					{
						IssueID:      "orch-a",
						RunID:        "20260103-030303",
						ShortID:      "aaa111",
						Status:       orchapi.RunStatusFailed,
						WorktreePath: "/tmp/worktree-c",
					},
					{
						IssueID:      "orch-b",
						RunID:        "20260104-040404",
						ShortID:      "bbb222",
						Status:       orchapi.RunStatusCanceled,
						WorktreePath: "/tmp/worktree-d",
					},
				},
			}, nil
		},
		ResolveRunFunc: func(ctx context.Context, ref orchapi.RunRef) (*orchapi.Run, error) {
			t.Fatal("ResolveRun should not be used for --all cleanup")
			return nil, nil
		},
		CleanRunWorktreeFunc: func(ctx context.Context, ref orchapi.RunRef) (*orchapi.CleanRunWorktreeResult, error) {
			return &orchapi.CleanRunWorktreeResult{
				IssueID:         ref.IssueID,
				RunID:           ref.RunID,
				WorktreeRemoved: true,
			}, nil
		},
	}

	deps := &cleanDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return api, nil
		},
	}

	if err := runCleanCommand(context.Background(), nil, &cleanOptions{
		All:   true,
		Force: true,
	}, deps); err != nil {
		t.Fatalf("runCleanCommand() error = %v", err)
	}

	if got := api.CleanRunWorktreeCalls(); len(got) != 2 {
		t.Fatalf("CleanRunWorktreeCalls() len = %d, want 2", len(got))
	}
}
