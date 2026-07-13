package daemon

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/config"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/store"
	filestore "github.com/proboscis/orch/internal/store/file"
)

type recordingReaperStore struct {
	store.Store
	order *[]string
}

func (s *recordingReaperStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
	label := string(event.Type) + ":" + event.Name
	if event.Type == model.EventTypeNote {
		label += ":" + event.Attrs["kind"]
	}
	*s.order = append(*s.order, label)
	return s.Store.AppendEvent(ref, event)
}

func createReaperTestRun(t *testing.T, status model.Status, updatedAt time.Time, withIdentity bool) (store.Store, *model.Run) {
	t.Helper()
	root := t.TempDir()
	st, err := filestore.New(root)
	if err != nil {
		t.Fatalf("filestore.New() error = %v", err)
	}
	issueID := model.IssueID("reaper-issue")
	if err := st.CreateIssue(&model.Issue{ID: issueID, Title: "Reaper issue", Status: model.IssueStatusOpen}); err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}
	runID := model.RunID("20260713-120000")
	if _, err := st.CreateRun(issueID, runID, map[string]string{"agent": "codex"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	worktree := t.TempDir()
	events := []*model.Event{
		{Timestamp: updatedAt.Add(-3 * time.Second), Type: model.EventTypeArtifact, Name: "worktree", Attrs: map[string]string{"path": worktree}},
		{Timestamp: updatedAt.Add(-2 * time.Second), Type: model.EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": model.GenerateSessionName(issueID, runID), "multiplexer": "tmux"}},
	}
	if withIdentity {
		events = append(events, &model.Event{Timestamp: updatedAt.Add(-time.Second), Type: model.EventTypeArtifact, Name: "agent_session", Attrs: map[string]string{"backend": "codex", "id": "rollout-test", "generation": "1"}})
	}
	events = append(events, &model.Event{Timestamp: updatedAt, Type: model.EventTypeStatus, Name: string(status), Attrs: map[string]string{"source": string(model.EventSourceDaemon)}})
	for _, event := range events {
		if err := st.AppendEvent(&model.RunRef{IssueID: issueID, RunID: runID}, event); err != nil {
			t.Fatalf("AppendEvent(%s) error = %v", event.Name, err)
		}
	}
	run, err := st.GetRun(&model.RunRef{IssueID: issueID, RunID: runID})
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	return st, run
}

func successfulReaperDeps(order *[]string, killed *bool) sessionReaperDeps {
	return sessionReaperDeps{
		Observe: func(*model.Run, string, string) (reaperSessionObservation, error) {
			*order = append(*order, "capture")
			return reaperSessionObservation{Content: "final pane output\n", SessionAlive: true, WorktreeExists: true}, nil
		},
		Persist: func(run *model.Run, content string, now time.Time) (string, error) {
			*order = append(*order, "persist")
			return persistSessionSnapshot(run, content, now)
		},
		Kill: func(*model.Run, string, string) error {
			*order = append(*order, "kill")
			*killed = true
			return nil
		},
	}
}

func TestSessionReaperIncidentReplayPreservesTerminalStatusAndProtocolOrder(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	baseStore, run := createReaperTestRun(t, model.StatusDone, now.Add(-time.Hour), true)
	var order []string
	killed := false
	recordingStore := &recordingReaperStore{Store: baseStore, order: &order}
	cfg := config.DefaultReaperConfig()
	cfg.TerminalGraceMinutes = 0

	outcome, err := reapRun(run, model.IssueStatusOpen, recordingStore, "project", t.TempDir(), cfg, now, successfulReaperDeps(&order, &killed))
	if err != nil {
		t.Fatalf("reapRun() error = %v", err)
	}
	if !outcome.Reaped || outcome.Reason != reapReasonTerminalGrace || !killed {
		t.Fatalf("outcome = %#v killed=%v", outcome, killed)
	}
	wantOrder := []string{"capture", "persist", "artifact:session_snapshot", "note:daemon_notice:session_reaped", "kill"}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("protocol order = %v, want %v", order, wantOrder)
	}

	reloaded, err := baseStore.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun() after reap error = %v", err)
	}
	if reloaded.Status != model.StatusDone {
		t.Fatalf("status changed to %s, want done", reloaded.Status)
	}
	if !reloaded.SessionReaped() {
		t.Fatal("session_reaped note did not latch the current generation")
	}
	var snapshotPath string
	for _, event := range reloaded.Events {
		if event.Type == model.EventTypeStatus && event.Name == string(model.StatusFailed) {
			t.Fatalf("reaper manufactured failed verdict: %s", event.String())
		}
		if event.Type == model.EventTypeArtifact && event.Name == "session_snapshot" {
			snapshotPath = event.Attrs["path"]
		}
		if event.Type == model.EventTypeNote && event.Attrs["kind"] == "session_reaped" {
			if event.Attrs["generation"] != "1" || event.Attrs["reason"] != string(reapReasonTerminalGrace) || event.Attrs["session_name"] != model.GenerateSessionName(run.IssueID, run.RunID) {
				t.Fatalf("session_reaped attrs = %#v", event.Attrs)
			}
		}
	}
	content, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot %q: %v", snapshotPath, err)
	}
	if string(content) != "final pane output\n" {
		t.Fatalf("snapshot content = %q", content)
	}
}

func TestSessionReaperIdleRunRequiresRecordedIdentity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cfg := config.DefaultReaperConfig()

	t.Run("recorded identity is reaped", func(t *testing.T) {
		st, run := createReaperTestRun(t, model.StatusWaiting, now.Add(-8*24*time.Hour), true)
		var order []string
		killed := false
		outcome, err := reapRun(run, model.IssueStatusOpen, st, "project", t.TempDir(), cfg, now, successfulReaperDeps(&order, &killed))
		if err != nil {
			t.Fatalf("reapRun() error = %v", err)
		}
		if !outcome.Reaped || outcome.Reason != reapReasonIdleTTL || !killed {
			t.Fatalf("outcome = %#v killed=%v", outcome, killed)
		}
	})

	t.Run("missing identity is kept and reported", func(t *testing.T) {
		st, run := createReaperTestRun(t, model.StatusWaiting, now.Add(-8*24*time.Hour), false)
		observed := false
		deps := sessionReaperDeps{
			Observe: func(*model.Run, string, string) (reaperSessionObservation, error) {
				observed = true
				return reaperSessionObservation{}, nil
			},
			Persist: persistSessionSnapshot,
			Kill:    func(*model.Run, string, string) error { return nil },
		}
		outcome, err := reapRun(run, model.IssueStatusOpen, st, "project", t.TempDir(), cfg, now, deps)
		if err != nil {
			t.Fatalf("reapRun() error = %v", err)
		}
		if outcome.KeptReason != "agent_session identity is not recorded" || observed {
			t.Fatalf("outcome = %#v observed=%v", outcome, observed)
		}
	})
}

func TestSessionReaperResolvedIssueGrace(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	st, run := createReaperTestRun(t, model.StatusWaiting, now.Add(-2*time.Hour), true)
	var order []string
	killed := false
	outcome, err := reapRun(run, model.IssueStatusResolved, st, "project", t.TempDir(), config.DefaultReaperConfig(), now, successfulReaperDeps(&order, &killed))
	if err != nil {
		t.Fatalf("reapRun() error = %v", err)
	}
	if !outcome.Reaped || outcome.Reason != reapReasonResolvedGrace || !killed {
		t.Fatalf("outcome = %#v killed=%v", outcome, killed)
	}
}

func TestSessionReaperKillFailureRecordsErrorAndRetriesNextPass(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	st, run := createReaperTestRun(t, model.StatusDone, now.Add(-time.Hour), true)
	cfg := config.DefaultReaperConfig()
	cfg.TerminalGraceMinutes = 0
	observeCalls := 0
	persistCalls := 0
	killCalls := 0
	deps := sessionReaperDeps{
		Observe: func(*model.Run, string, string) (reaperSessionObservation, error) {
			observeCalls++
			return reaperSessionObservation{Content: "retry snapshot", SessionAlive: true, WorktreeExists: true}, nil
		},
		Persist: func(run *model.Run, content string, now time.Time) (string, error) {
			persistCalls++
			return persistSessionSnapshot(run, content, now)
		},
		Kill: func(*model.Run, string, string) error {
			killCalls++
			if killCalls <= 2 {
				return errors.New("injected mux failure")
			}
			return nil
		},
	}

	if _, err := reapRun(run, model.IssueStatusOpen, st, "project", t.TempDir(), cfg, now, deps); err == nil || !strings.Contains(err.Error(), "injected mux failure") {
		t.Fatalf("first reap error = %v", err)
	}
	retryRun, err := st.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun() for retry error = %v", err)
	}
	if retryRun.UpdatedAt.Before(now) {
		t.Fatalf("precondition: error artifact did not refresh UpdatedAt: %s", retryRun.UpdatedAt)
	}
	eventsAfterFirstFailure := len(retryRun.Events)
	if _, err := reapRun(retryRun, model.IssueStatusOpen, st, "project", t.TempDir(), cfg, now.Add(time.Minute), deps); err == nil || !strings.Contains(err.Error(), "injected mux failure") {
		t.Fatalf("second reap error = %v", err)
	}

	afterTwoFailures, err := st.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun() after second failure error = %v", err)
	}
	if len(afterTwoFailures.Events) != eventsAfterFirstFailure {
		t.Fatalf("second identical failure appended events: first=%d second=%d", eventsAfterFirstFailure, len(afterTwoFailures.Events))
	}
	var snapshotArtifacts, reapNotes, errorArtifacts int
	for _, event := range afterTwoFailures.Events {
		switch {
		case event.Type == model.EventTypeArtifact && event.Name == "session_snapshot":
			snapshotArtifacts++
		case event.Type == model.EventTypeNote && event.Name == model.DaemonNoticeEventName && event.Attrs["kind"] == "session_reaped":
			reapNotes++
		case event.Type == model.EventTypeArtifact && event.Name == "error":
			errorArtifacts++
		}
	}
	if snapshotArtifacts != 1 || reapNotes != 1 || errorArtifacts != 1 {
		t.Fatalf("bounded retry ledger: snapshots=%d reap_notes=%d errors=%d, want 1 each", snapshotArtifacts, reapNotes, errorArtifacts)
	}
	if observeCalls != 1 || persistCalls != 1 || killCalls != 2 {
		t.Fatalf("retry calls: observe=%d persist=%d kill=%d, want 1,1,2", observeCalls, persistCalls, killCalls)
	}

	outcome, err := reapRun(afterTwoFailures, model.IssueStatusOpen, st, "project", t.TempDir(), cfg, now.Add(2*time.Minute), deps)
	if err != nil {
		t.Fatalf("successful retry reap error = %v", err)
	}
	if !outcome.Reaped || killCalls != 3 || observeCalls != 1 || persistCalls != 1 {
		t.Fatalf("successful retry outcome=%#v observe=%d persist=%d kill=%d", outcome, observeCalls, persistCalls, killCalls)
	}

	reloaded, err := st.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun() after successful retry error = %v", err)
	}
	if len(reloaded.Events) != eventsAfterFirstFailure {
		t.Fatalf("successful kill-only retry appended events: before=%d after=%d", eventsAfterFirstFailure, len(reloaded.Events))
	}
}

func TestSessionReaperEmptyMultiplexerIsError(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	st, run := createReaperTestRun(t, model.StatusDone, now.Add(-time.Hour), true)
	run.Multiplexer = ""
	observed := false
	deps := sessionReaperDeps{
		Observe: func(*model.Run, string, string) (reaperSessionObservation, error) {
			observed = true
			return reaperSessionObservation{}, nil
		},
		Persist: persistSessionSnapshot,
		Kill:    func(*model.Run, string, string) error { return nil },
	}
	cfg := config.DefaultReaperConfig()
	cfg.TerminalGraceMinutes = 0
	if _, err := reapRun(run, model.IssueStatusOpen, st, "project", t.TempDir(), cfg, now, deps); err == nil || !strings.Contains(err.Error(), "empty multiplexer") {
		t.Fatalf("empty multiplexer error = %v", err)
	}
	if observed {
		t.Fatal("empty multiplexer reached observation path")
	}
}

func TestWorkerReaperChecksWorktreeAndRejectsEmptyMultiplexer(t *testing.T) {
	projectRoot := t.TempDir()
	st, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatalf("filestore.New() error = %v", err)
	}
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
	server.SetWorkerIdentity("worker-reaper", "worker-host")
	if _, err := server.registerRepoContext("worker-reaper-project", projectRoot, "", st); err != nil {
		t.Fatalf("registerRepoContext() error = %v", err)
	}

	missingWorktree := filepath.Join(t.TempDir(), "missing")
	captureLease := &WorkerLease{
		LeaseID: "capture-reaper", WorkerID: "worker-reaper", ProjectID: "worker-reaper-project", Effect: "capture_session", IssueID: "worker-issue", RunID: "20260713-130000",
		Payload: &WorkerEffectPayload{CaptureSession: &CaptureSessionPayload{
			CheckWorktree: true,
			RunSnapshot:   &RunSnapshot{IssueID: "worker-issue", RunID: "20260713-130000", Status: model.StatusDone, WorktreePath: missingWorktree, Multiplexer: "tmux"},
		}},
	}
	result, err := server.executeLeaseEffect(captureLease)
	if err != nil {
		t.Fatalf("worktree interlock capture error = %v", err)
	}
	if result == nil || result.CaptureResult == nil || !result.CaptureResult.WorktreeChecked || result.CaptureResult.WorktreeExists {
		t.Fatalf("capture result = %#v, want checked missing worktree", result)
	}

	stopLease := &WorkerLease{
		LeaseID: "stop-reaper", WorkerID: "worker-reaper", ProjectID: "worker-reaper-project", Effect: "stop_run", IssueID: "worker-issue", RunID: "20260713-130000",
		Payload: &WorkerEffectPayload{StopRun: &StopRunPayload{
			ReapSession: true,
			RunSnapshot: &RunSnapshot{IssueID: "worker-issue", RunID: "20260713-130000", Status: model.StatusDone, WorktreePath: projectRoot},
		}},
	}
	if _, err := server.executeLeaseEffect(stopLease); err == nil || !strings.Contains(err.Error(), "empty multiplexer") {
		t.Fatalf("worker empty multiplexer error = %v", err)
	}
}

type countingListRunsStore struct {
	store.Store
	listCalls int
}

func (s *countingListRunsStore) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	s.listCalls++
	return s.Store.ListRuns(filter)
}

func TestSessionReaperEnabledFalseDisablesPass(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte("reaper:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	baseStore, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatalf("filestore.New() error = %v", err)
	}
	counting := &countingListRunsStore{Store: baseStore}
	server := NewSocketServer(func(string) (store.Store, error) { return counting, nil }, log.New(io.Discard, "", 0))
	if _, err := server.registerRepoContext("reaper-disabled", projectRoot, "", counting); err != nil {
		t.Fatalf("registerRepoContext() error = %v", err)
	}
	daemon := New(func(string) (store.Store, error) { return counting, nil })
	daemon.socketServer = server
	daemon.reapAllAt(time.Now())
	if counting.listCalls != 0 {
		t.Fatalf("disabled reaper listed runs %d times, want 0", counting.listCalls)
	}
}

func createRepairFindingRun(t *testing.T, st store.Store, issueID model.IssueID, runID model.RunID, status model.Status, updatedAt time.Time, identity bool) *model.Run {
	t.Helper()
	if err := st.CreateIssue(&model.Issue{ID: issueID, Title: string(issueID), Status: model.IssueStatusOpen}); err != nil {
		t.Fatalf("CreateIssue(%s) error = %v", issueID, err)
	}
	if _, err := st.CreateRun(issueID, runID, map[string]string{"agent": "codex"}); err != nil {
		t.Fatalf("CreateRun(%s) error = %v", issueID, err)
	}
	worktree := t.TempDir()
	events := []*model.Event{
		{Timestamp: updatedAt.Add(-3 * time.Second), Type: model.EventTypeArtifact, Name: "worktree", Attrs: map[string]string{"path": worktree}},
		{Timestamp: updatedAt.Add(-2 * time.Second), Type: model.EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": model.GenerateSessionName(issueID, runID), "multiplexer": "tmux"}},
	}
	if identity {
		events = append(events, &model.Event{Timestamp: updatedAt.Add(-time.Second), Type: model.EventTypeArtifact, Name: "agent_session", Attrs: map[string]string{"id": "recorded", "generation": "1"}})
	}
	events = append(events, &model.Event{Timestamp: updatedAt, Type: model.EventTypeStatus, Name: string(status), Attrs: map[string]string{"source": string(model.EventSourceDaemon)}})
	ref := &model.RunRef{IssueID: issueID, RunID: runID}
	for _, event := range events {
		if err := st.AppendEvent(ref, event); err != nil {
			t.Fatalf("AppendEvent(%s) error = %v", event.Name, err)
		}
	}
	run, err := st.GetRun(ref)
	if err != nil {
		t.Fatalf("GetRun(%s) error = %v", ref.String(), err)
	}
	return run
}

func TestRepairReportsTerminalAliveAndUnreapableKeptSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), []byte("reaper:\n  terminal_grace_minutes: 0\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	st, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatalf("filestore.New() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	terminal := createRepairFindingRun(t, st, "repair-terminal", "20260713-120001", model.StatusDone, now.Add(-time.Hour), true)
	unreapable := createRepairFindingRun(t, st, "repair-unreapable", "20260713-120002", model.StatusWaiting, now.Add(-8*24*time.Hour), false)

	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))
	if _, err := server.registerRepoContext("repair-project", projectRoot, "", st); err != nil {
		t.Fatalf("registerRepoContext() error = %v", err)
	}
	orphan := "run-orphan-20260713-120003"
	findings, err := server.classifySessionRepairFindings([]string{
		model.GenerateSessionName(terminal.IssueID, terminal.RunID),
		model.GenerateSessionName(unreapable.IssueID, unreapable.RunID),
		orphan,
		"orch-control-agent",
		"orch-monitor-test",
	}, now)
	if err != nil {
		t.Fatalf("classifySessionRepairFindings() error = %v", err)
	}

	want := map[sessionRepairKind]string{
		sessionRepairTerminalAlive:  model.GenerateSessionName(terminal.IssueID, terminal.RunID),
		sessionRepairUnreapableKept: model.GenerateSessionName(unreapable.IssueID, unreapable.RunID),
		sessionRepairOrphaned:       orphan,
	}
	if len(findings) != len(want) {
		t.Fatalf("findings = %#v, want one of each kind", findings)
	}
	for _, finding := range findings {
		if want[finding.Kind] != finding.SessionName {
			t.Fatalf("finding = %#v, want session %q for kind %s", finding, want[finding.Kind], finding.Kind)
		}
		if finding.Kind == sessionRepairUnreapableKept && !strings.Contains(finding.Reason, "identity is not recorded") {
			t.Fatalf("unreapable reason = %q", finding.Reason)
		}
	}
}
