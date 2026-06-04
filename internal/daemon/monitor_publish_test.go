package daemon

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/model"
	filestore "github.com/s22625/orch/internal/store/file"
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

	if err := d.updateStatus(current, model.StatusWaiting, st); err != nil {
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
		expectedShort := model.GenerateShortID("issue-publish", "run-1").String()
		if ev.ShortId != expectedShort {
			t.Fatalf("short_id = %q, want %q", ev.ShortId, expectedShort)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event received within timeout")
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
	if err := d.updateStatus(current, model.StatusPROpen, st); err != nil {
		t.Fatalf("updateStatus: %v", err)
	}

	select {
	case ev := <-sub.Events():
		t.Fatalf("expected no event for from==to publish, got %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// expected: no event
	}
}
