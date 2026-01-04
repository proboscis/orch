package daemon

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func newTestDaemon() *Daemon {
	return &Daemon{
		logger:      log.New(io.Discard, "", 0),
		runStates:   make(map[string]*RunState),
		lastFetchAt: make(map[string]time.Time),
	}
}

func TestHashContentIgnoresStatusBar(t *testing.T) {
	outputA := strings.Join([]string{"line1", "line2", "line3", "line4", "line5", "line6"}, "\n")
	outputB := strings.Join([]string{"line1", "line2", "line3", "line4", "line5", "changed"}, "\n")

	if hashContent(outputA) != hashContent(outputB) {
		t.Fatal("expected hash to ignore last 5 lines")
	}
}

func TestDetectPRCreation(t *testing.T) {
	d := newTestDaemon()
	url := d.detectPRCreation("opened https://github.com/org/repo/pull/123 for review")
	if url != "https://github.com/org/repo/pull/123" {
		t.Fatalf("unexpected pr url: %q", url)
	}
	url = d.detectPRCreation("merge https://gitlab.com/org/repo/merge_requests/7 done")
	if url != "https://gitlab.com/org/repo/merge_requests/7" {
		t.Fatalf("unexpected pr url: %q", url)
	}
}

func TestCleanupStates(t *testing.T) {
	d := newTestDaemon()
	d.runStates["issue#1"] = &RunState{}
	d.runStates["issue#2"] = &RunState{}

	active := []*model.Run{{IssueID: "issue", RunID: "1"}}
	d.cleanupStates(active)

	if len(d.runStates) != 1 {
		t.Fatalf("expected 1 state, got %d", len(d.runStates))
	}
	if _, ok := d.runStates["issue#1"]; !ok {
		t.Fatal("expected issue#1 to remain")
	}
}

func TestGetOrCreateState(t *testing.T) {
	d := newTestDaemon()
	run := &model.Run{IssueID: "issue", RunID: "1"}

	state := d.getOrCreateState(run)
	if state == nil || state.LastCheckAt.IsZero() || state.LastOutputAt.IsZero() {
		t.Fatal("expected initialized state")
	}
	state2 := d.getOrCreateState(run)
	if state2 != state {
		t.Fatal("expected same state instance")
	}
}

func TestPeriodicFetchSkipsWithinInterval(t *testing.T) {
	d := newTestDaemon()
	repoPath := "/test/repo"
	d.lastFetchAt[repoPath] = time.Now()

	runs := []*model.Run{{IssueID: "test", RunID: "1", WorktreePath: ""}}
	d.periodicFetch(runs)

	if len(d.lastFetchAt) != 1 {
		t.Fatal("lastFetchAt should remain unchanged for runs without worktree")
	}
}

func TestPeriodicFetchTracking(t *testing.T) {
	d := newTestDaemon()
	if len(d.lastFetchAt) != 0 {
		t.Fatal("lastFetchAt should start empty")
	}
}
