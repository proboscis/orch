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
		logger:    log.New(io.Discard, "", 0),
		runStates: make(map[string]*RunState),
	}
}

func TestHashContentIgnoresStatusBar(t *testing.T) {
	outputA := strings.Join([]string{"line1", "line2", "line3", "line4", "line5", "line6"}, "\n")
	outputB := strings.Join([]string{"line1", "line2", "line3", "line4", "line5", "changed"}, "\n")

	if hashContent(outputA) != hashContent(outputB) {
		t.Fatal("expected hash to ignore last 5 lines")
	}
}

func TestDetectStatus(t *testing.T) {
	d := newTestDaemon()
	state := &RunState{LastOutputAt: time.Now()}
	run := &model.Run{}

	if got := d.detectStatus(run, "user@host:~$ ", state, false, false); got != model.StatusUnknown {
		t.Fatalf("agent exit status = %q, want %q", got, model.StatusUnknown)
	}
	if got := d.detectStatus(run, "Task completed successfully", state, false, false); got != model.StatusDone {
		t.Fatalf("completed status = %q, want %q", got, model.StatusDone)
	}
	if got := d.detectStatus(run, "Error: rate limit exceeded", state, false, false); got != model.StatusBlockedAPI {
		t.Fatalf("api limited status = %q, want %q", got, model.StatusBlockedAPI)
	}
	if got := d.detectStatus(run, "Fatal error: bad things", state, false, false); got != model.StatusFailed {
		t.Fatalf("failed status = %q, want %q", got, model.StatusFailed)
	}
	if got := d.detectStatus(run, "work in progress", state, true, false); got != model.StatusRunning {
		t.Fatalf("running status = %q, want %q", got, model.StatusRunning)
	}
	if got := d.detectStatus(run, "prompt text", state, false, true); got != model.StatusBlocked {
		t.Fatalf("blocked status = %q, want %q", got, model.StatusBlocked)
	}
}

func TestPromptDetection(t *testing.T) {
	d := newTestDaemon()
	if !d.isWaitingForInput("accept edits to continue") {
		t.Fatal("expected prompt detection")
	}
	if d.isWaitingForInput("no prompts here") {
		t.Fatal("unexpected prompt detection")
	}
}

func TestAgentExitedDetection(t *testing.T) {
	d := newTestDaemon()
	if !d.isAgentExited("some output\nuser@host:~$ ") {
		t.Fatal("expected agent exit detection")
	}
	if d.isAgentExited("accept edits\nuser@host:~$ ") {
		t.Fatal("expected agent running when UI pattern present")
	}
}

func TestCompletionAndFailureDetection(t *testing.T) {
	d := newTestDaemon()
	if !d.isCompleted("Task completed successfully\nsession ended") {
		t.Fatal("expected completion detection")
	}
	if !d.isFailed("Fatal error: something broke") {
		t.Fatal("expected failure detection")
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
