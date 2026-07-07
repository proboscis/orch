package daemon

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/model"
	filestore "github.com/proboscis/orch/internal/store/file"
)

// TestUpdateStatus_PublishesTransitionToBus is the missing end-to-end
// coverage: it confirms that the daemon's updateStatus actually invokes
// publishRunEvent with the correct from→to status and source, so a real
// subscriber wired through SocketServer.RunEventBus will receive it.
func TestUpdateStatus_PublishesTransitionToBus(t *testing.T) {
	root := t.TempDir()
	issuesRoot := filepath.Join(root, "issues")
	if err := os.MkdirAll(issuesRoot, 0o755); err != nil {
		t.Fatalf("mkdir issues root: %v", err)
	}

	st, err := filestore.New(issuesRoot)
	if err != nil {
		t.Fatalf("filestore.New: %v", err)
	}
	if err := st.CreateIssue(&model.Issue{ID: "issue-publish", Title: "t", Status: model.IssueStatusOpen}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	run, err := st.CreateRun("issue-publish", "run-1", nil)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	// Seed an initial running status so updateStatus sees a non-empty fromStatus.
	if err := st.AppendEvent(&model.RunRef{IssueID: run.IssueID, RunID: run.RunID}, model.NewStatusEvent(model.StatusRunning)); err != nil {
		t.Fatalf("seed AppendEvent: %v", err)
	}

	srv := NewSocketServer(nil, log.New(io.Discard, "", 0))
	d := newTestDaemon()
	d.socketServer = srv

	sub := srv.RunEventBus().Subscribe(RunEventFilter{})
	defer sub.Close()

	// Refetch run with seeded status.
	current, err := st.GetRun(&model.RunRef{IssueID: run.IssueID, RunID: run.RunID})
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if current.Status != model.StatusRunning {
		t.Fatalf("seeded status not running: %q", current.Status)
	}

	if err := d.updateStatus(current, model.StatusWaiting, "", st, ""); err != nil {
		t.Fatalf("updateStatus: %v", err)
	}

	select {
	case ev := <-sub.Events():
		if ev.IssueId != "issue-publish" || ev.RunId != "run-1" {
			t.Fatalf("unexpected ids: %+v", ev)
		}
		if ev.FromStatus != orchpb.RunStatus_RUN_STATUS_RUNNING {
			t.Fatalf("from = %v, want RUNNING", ev.FromStatus)
		}
		if ev.ToStatus != orchpb.RunStatus_RUN_STATUS_WAITING {
			t.Fatalf("to = %v, want WAITING", ev.ToStatus)
		}
		if ev.Source != string(model.EventSourceDaemon) {
			t.Fatalf("source = %q, want %q", ev.Source, model.EventSourceDaemon)
		}
		expectedShort := model.GenerateShortID("issue-publish", "run-1")
		if ev.ShortId != string(expectedShort) {
			t.Fatalf("short_id = %q, want %q", ev.ShortId, expectedShort)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event received within timeout")
	}
}

// TestPublishRunEvent_VocabularyDriftSkipsNotPanics feeds a status with no
// proto mapping through the monitor-plane publish path (both the from and
// the to arm) and asserts the drift rule of
// docs/design/run-state-machine.md §8: no panic (the old behavior panicked
// here and killed the daemon), an ERROR naming the run ref and the
// offending status is logged, no frame is published for the drifted run,
// and a subsequent healthy run's publish is unaffected.
func TestPublishRunEvent_VocabularyDriftSkipsNotPanics(t *testing.T) {
	var logBuf bytes.Buffer
	srv := NewSocketServer(nil, log.New(io.Discard, "", 0))
	d := newTestDaemon()
	d.logger = log.New(&logBuf, "", 0)
	d.socketServer = srv

	sub := srv.RunEventBus().Subscribe(RunEventFilter{})
	defer sub.Close()

	drifted := &model.Run{IssueID: "issue-drift", RunID: "run-drift"}
	unmapped := model.Status("status-from-the-future")

	// Old behavior: either of these calls panics and the test fails.
	d.publishRunEvent(drifted, unmapped, model.StatusRunning, model.EventSourceDaemon)
	d.publishRunEvent(drifted, model.StatusRunning, unmapped, model.EventSourceDaemon)

	logged := logBuf.String()
	if got := strings.Count(logged, "ERROR issue-drift#run-drift"); got != 2 {
		t.Fatalf("ERROR log lines naming the run ref = %d, want 2; log:\n%s", got, logged)
	}
	if !strings.Contains(logged, "status-from-the-future") {
		t.Fatalf("log does not name the offending status:\n%s", logged)
	}

	select {
	case ev := <-sub.Events():
		t.Fatalf("expected no event for drifted status, got %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// expected: publish skipped
	}

	// Other runs' updates are unaffected on the same tick.
	healthy := &model.Run{IssueID: "issue-healthy", RunID: "run-1"}
	d.publishRunEvent(healthy, model.StatusRunning, model.StatusWaiting, model.EventSourceDaemon)
	select {
	case ev := <-sub.Events():
		if ev.IssueId != "issue-healthy" || ev.RunId != "run-1" {
			t.Fatalf("unexpected ids: %+v", ev)
		}
		if ev.FromStatus != orchpb.RunStatus_RUN_STATUS_RUNNING || ev.ToStatus != orchpb.RunStatus_RUN_STATUS_WAITING {
			t.Fatalf("unexpected transition: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("healthy run publish blocked after drift skip")
	}
}

// TestUpdateStatus_SkipsRedundantPublish ensures that when daemon code
// calls updateStatus with a status that already matches the run's stored
// state, no proto event is emitted on the bus (the AppendEvent itself
// may still record an audit line, but external observers should not see
// transition noise).
func TestUpdateStatus_SkipsRedundantPublish(t *testing.T) {
	root := t.TempDir()
	issuesRoot := filepath.Join(root, "issues")
	if err := os.MkdirAll(issuesRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	st, err := filestore.New(issuesRoot)
	if err != nil {
		t.Fatalf("filestore.New: %v", err)
	}
	if err := st.CreateIssue(&model.Issue{ID: "issue-skip", Title: "t", Status: model.IssueStatusOpen}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	run, err := st.CreateRun("issue-skip", "run-1", nil)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := st.AppendEvent(&model.RunRef{IssueID: run.IssueID, RunID: run.RunID}, model.NewStatusEvent(model.StatusPROpen)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := NewSocketServer(nil, log.New(io.Discard, "", 0))
	d := newTestDaemon()
	d.socketServer = srv

	sub := srv.RunEventBus().Subscribe(RunEventFilter{})
	defer sub.Close()

	current, err := st.GetRun(&model.RunRef{IssueID: run.IssueID, RunID: run.RunID})
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if err := d.updateStatus(current, model.StatusPROpen, "", st, ""); err != nil {
		t.Fatalf("updateStatus: %v", err)
	}

	select {
	case ev := <-sub.Events():
		t.Fatalf("expected no event for from==to publish, got %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// expected: no event
	}
}
