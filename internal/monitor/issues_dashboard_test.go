package monitor

import (
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestBuildContinueTargets(t *testing.T) {
	now := time.Now()
	runs := []*model.Run{
		{
			IssueID:   "orch-070",
			RunID:     "run-1",
			Branch:    "issue/orch-070/run-1",
			UpdatedAt: now.Add(-2 * time.Hour),
			Agent:     "claude",
			Status:    model.StatusDone,
		},
		{
			IssueID:   "orch-070",
			RunID:     "run-2",
			Branch:    "issue/orch-070/run-2",
			UpdatedAt: now.Add(-1 * time.Hour),
			Agent:     "codex",
			Status:    model.StatusDone,
		},
	}
	branches := []branchInfo{
		{name: "issue/orch-070/run-1", commitTime: now.Add(-3 * time.Hour)},
		{name: "issue/orch-070/extra", commitTime: now.Add(-30 * time.Minute)},
	}

	targets := buildContinueTargets(runs, branches)
	if len(targets) != 3 {
		t.Fatalf("buildContinueTargets() got %d targets, want 3", len(targets))
	}

	if targets[0].kind != continueTargetRun || targets[0].run == nil || targets[0].run.RunID != "run-2" {
		t.Fatalf("target[0] = %#v, want run-2", targets[0])
	}
	if targets[1].kind != continueTargetRun || targets[1].run == nil || targets[1].run.RunID != "run-1" {
		t.Fatalf("target[1] = %#v, want run-1", targets[1])
	}
	if targets[2].kind != continueTargetBranch || targets[2].branch != "issue/orch-070/extra" {
		t.Fatalf("target[2] = %#v, want branch issue/orch-070/extra", targets[2])
	}
}
