package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	orchpb "github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/multiplexer"
)

type timingTestLogger struct {
	buf bytes.Buffer
}

func TestHandleProtoListMonitorsScopesAndSortsInDaemon(t *testing.T) {
	server := newTestServer(t, &mockStore{})
	baseTime := time.Unix(1_700_000_000, 0)
	server.monitors = map[string]*MonitorConnection{
		"newer": {
			ID:        "newer",
			PID:       202,
			Project:   "project-a",
			View:      "issues",
			StartedAt: baseTime.Add(time.Minute),
			LastSeen:  baseTime.Add(time.Minute),
		},
		"other-project": {
			ID:        "other-project",
			PID:       303,
			Project:   "project-b",
			View:      "runs",
			StartedAt: baseTime.Add(-time.Minute),
			LastSeen:  baseTime.Add(-time.Minute),
		},
		"older": {
			ID:          "older",
			PID:         101,
			Project:     "project-a",
			View:        "runs",
			SessionName: "orch-monitor-project-a",
			StartedAt:   baseTime,
			LastSeen:    baseTime,
		},
	}

	resp := server.handleProtoListMonitors(&orchpb.ListMonitorsRequest{
		Project: "project-a",
	})

	if !resp.Ok {
		t.Fatalf("handleProtoListMonitors() error = %s", resp.Error)
	}
	monitors := resp.GetListMonitors().GetMonitors()
	if len(monitors) != 2 {
		t.Fatalf("monitor count = %d, want 2", len(monitors))
	}
	if monitors[0].GetId() != "older" || monitors[1].GetId() != "newer" {
		t.Fatalf("monitor order = [%s, %s], want [older, newer]", monitors[0].GetId(), monitors[1].GetId())
	}
	if monitors[0].GetSessionName() != "orch-monitor-project-a" {
		t.Fatalf("session_name = %q, want orch-monitor-project-a", monitors[0].GetSessionName())
	}
}

func TestHandleProtoListMonitorsRejectsEmptyProjectScope(t *testing.T) {
	server := newTestServer(t, &mockStore{})
	server.monitors = map[string]*MonitorConnection{
		"project-a": {ID: "project-a", Project: "project-a"},
		"project-b": {ID: "project-b", Project: "project-b"},
	}

	resp := server.handleProtoListMonitors(&orchpb.ListMonitorsRequest{})

	if resp.Ok {
		t.Fatal("empty project scope unexpectedly listed every monitor")
	}
	if !strings.Contains(resp.Error, "requires a project scope") {
		t.Fatalf("error = %q, want actionable project-scope error", resp.Error)
	}
}

func TestHandleProtoKillMonitorRejectsEmptyProjectScope(t *testing.T) {
	server := newTestServer(t, &mockStore{})
	server.monitors = map[string]*MonitorConnection{
		"project-a": {ID: "project-a", PID: -1, Project: "project-a"},
		"project-b": {ID: "project-b", PID: -1, Project: "project-b"},
	}

	resp := server.handleProtoKillMonitor(&orchpb.KillMonitorRequest{All: true})

	if resp.Ok {
		t.Fatal("empty project scope unexpectedly killed every monitor")
	}
	if !strings.Contains(resp.Error, "kill_all requires a project scope") {
		t.Fatalf("error = %q, want actionable project-scope error", resp.Error)
	}
	server.monitorsMu.RLock()
	remainingRegistrations := len(server.monitors)
	server.monitorsMu.RUnlock()
	if remainingRegistrations != 2 {
		t.Fatalf("registrations after rejected kill = %d, want 2", remainingRegistrations)
	}
}

func TestHandleProtoKillMonitorTerminatesRegisteredProcess(t *testing.T) {
	server := newTestServer(t, &mockStore{})
	process := exec.Command("sleep", "30")
	if err := process.Start(); err != nil {
		t.Fatalf("start monitor process: %v", err)
	}
	processDone := make(chan error, 1)
	go func() {
		processDone <- process.Wait()
	}()
	t.Cleanup(func() {
		if process.Process != nil {
			_ = process.Process.Kill()
		}
		select {
		case <-processDone:
		default:
		}
	})

	server.monitors = map[string]*MonitorConnection{
		"mon-123": {
			ID:          "mon-123",
			PID:         process.Process.Pid,
			Project:     "project-a",
			View:        "runs",
			SessionName: "missing-test-session",
			StartedAt:   time.Now(),
			LastSeen:    time.Now(),
		},
		"mon-sibling": {
			ID:          "mon-sibling",
			PID:         -1,
			Project:     "project-a",
			View:        "issues",
			SessionName: "missing-test-session",
			StartedAt:   time.Now(),
			LastSeen:    time.Now(),
		},
	}

	resp := server.handleProtoKillMonitor(&orchpb.KillMonitorRequest{
		MonitorId: "mon-123",
		Project:   "project-a",
	})

	if !resp.Ok {
		t.Fatalf("handleProtoKillMonitor() error = %s", resp.Error)
	}
	if got := resp.GetKillMonitor().GetKilledCount(); got != 2 {
		t.Fatalf("killed_count = %d, want 2 registrations in the session", got)
	}
	select {
	case <-processDone:
	case <-time.After(2 * time.Second):
		t.Fatal("registered monitor process was not terminated")
	}
	server.monitorsMu.RLock()
	remainingRegistrations := len(server.monitors)
	server.monitorsMu.RUnlock()
	if remainingRegistrations != 0 {
		t.Fatalf("%d registrations remain for killed session", remainingRegistrations)
	}
}

func TestCleanupStaleMonitorsStillRemovesOnlyExpiredHeartbeats(t *testing.T) {
	server := newTestServer(t, &mockStore{})
	now := time.Now()
	server.monitors = map[string]*MonitorConnection{
		"stale": {
			ID:       "stale",
			PID:      101,
			LastSeen: now.Add(-61 * time.Second),
		},
		"fresh": {
			ID:       "fresh",
			PID:      202,
			LastSeen: now.Add(-59 * time.Second),
		},
	}

	server.cleanupStaleMonitors()

	server.monitorsMu.RLock()
	_, staleExists := server.monitors["stale"]
	_, freshExists := server.monitors["fresh"]
	server.monitorsMu.RUnlock()
	if staleExists {
		t.Fatal("stale monitor remains registered")
	}
	if !freshExists {
		t.Fatal("fresh monitor was incorrectly reaped")
	}
}

func TestKillMonitorSessionRemovesTmuxSession(t *testing.T) {
	mux := multiplexer.NewTmuxMultiplexer()
	if !mux.IsAvailable() {
		t.Skip("tmux is not available")
	}
	sessionName := fmt.Sprintf("orch-monitor-kill-test-%d", time.Now().UnixNano())
	if err := mux.NewSession(&multiplexer.SessionConfig{
		SessionName: sessionName,
		Command:     "sleep 30",
	}); err != nil {
		t.Fatalf("create tmux session: %v", err)
	}
	t.Cleanup(func() {
		if mux.HasSession(sessionName) {
			_ = mux.KillSession(sessionName)
		}
	})

	killed, err := killMonitorSession(sessionName)
	if err != nil {
		t.Fatalf("killMonitorSession() error = %v", err)
	}
	if !killed {
		t.Fatal("killMonitorSession() reported no matching session")
	}
	if mux.HasSession(sessionName) {
		t.Fatal("tmux session remains after killMonitorSession()")
	}
}

type waitForRunsStatusStore struct {
	mockStore
	run   *model.Run
	calls int
}

type resolveRunTestStore struct {
	mockStore
	appendErr          error
	setIssueErr        error
	latestRun          *model.Run
	appendCalls        []resolveRunAppendCall
	setIssueStatusCall []resolveRunSetIssueStatusCall
}

type resolveRunAppendCall struct {
	ref   model.RunRef
	event *model.Event
}

type resolveRunSetIssueStatusCall struct {
	issueID model.IssueID
	status  model.IssueStatus
}

func sessionStateTestRun(t *testing.T, issueID, runID string, reaped, withIdentity bool) *model.Run {
	t.Helper()
	now := time.Date(2026, 7, 13, 10, 11, 12, 0, time.UTC)
	run := &model.Run{
		IssueID: model.IssueID(issueID),
		RunID:   model.RunID(runID),
		Agent:   "codex",
		Events: []*model.Event{
			{Timestamp: now.Add(-4 * time.Second), Type: model.EventTypeArtifact, Name: "worktree", Attrs: map[string]string{"path": "/tmp/" + runID}},
			{Timestamp: now.Add(-3 * time.Second), Type: model.EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": model.GenerateSessionName(model.IssueID(issueID), model.RunID(runID)), "multiplexer": "tmux"}},
			{Timestamp: now.Add(-time.Second), Type: model.EventTypeStatus, Name: string(model.StatusRunning)},
		},
	}
	if withIdentity {
		run.Events = append(run.Events[:2], append([]*model.Event{
			{Timestamp: now.Add(-2 * time.Second), Type: model.EventTypeArtifact, Name: "agent_session", Attrs: map[string]string{"backend": "codex", "id": "rollout-" + runID, "generation": "1"}},
		}, run.Events[2:]...)...)
	}
	if reaped {
		run.Events = append(run.Events, &model.Event{
			Timestamp: now,
			Type:      model.EventTypeNote,
			Name:      model.DaemonNoticeEventName,
			Attrs:     map[string]string{"kind": "session_reaped", "generation": "1", "reason": "idle_ttl"},
		})
	}
	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}
	return run
}

func TestHandleProtoListRunsDerivesSessionStateFromStoredFacts(t *testing.T) {
	live := sessionStateTestRun(t, "issue-live", "run-live", false, true)
	revivable := sessionStateTestRun(t, "issue-revivable", "run-revivable", true, true)
	unrevivable := sessionStateTestRun(t, "issue-unrevivable", "run-unrevivable", true, false)
	st := &mockStore{
		runs: map[string]*model.Run{
			live.Ref().String():        live,
			revivable.Ref().String():   revivable,
			unrevivable.Ref().String(): unrevivable,
		},
		issues: map[string]*model.Issue{},
	}
	server := newTestServer(t, st)

	resp := server.handleProtoListRuns(&orchpb.ListRunsRequest{
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoListRuns() error = %s", resp.Error)
	}

	got := make(map[string]*orchpb.Run)
	for _, run := range resp.GetListRuns().GetRuns() {
		got[run.GetRunId()] = run
	}
	if state := got["run-live"].GetSessionState(); state != "live" {
		t.Fatalf("live session_state = %q, want live", state)
	}
	if state := got["run-revivable"].GetSessionState(); state != "reaped(revivable)" {
		t.Fatalf("revivable session_state = %q, want reaped(revivable)", state)
	}
	if state := got["run-unrevivable"].GetSessionState(); state != "reaped(unrevivable)" {
		t.Fatalf("unrevivable session_state = %q, want reaped(unrevivable)", state)
	}
	if detail := got["run-unrevivable"].GetSessionStateDetail(); !strings.Contains(detail, "agent_session identity is not recorded") {
		t.Fatalf("unrevivable session_state_detail = %q, want missing identity", detail)
	}
}

func TestHandleProtoCaptureSessionServesReapedSnapshotWithoutMultiplexer(t *testing.T) {
	previousCaptureMux := getCaptureMultiplexerForType
	getCaptureMultiplexerForType = func(multiplexer.Type) captureMultiplexer {
		panic("multiplexer must not be touched for a reaped capture")
	}
	t.Cleanup(func() { getCaptureMultiplexerForType = previousCaptureMux })

	snapshotPath := t.TempDir() + "/final-snapshot.txt"
	if err := os.WriteFile(snapshotPath, []byte("final pane output\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run := sessionStateTestRun(t, "issue-reaped-capture", "run-reaped-capture", true, true)
	reapNote := run.Events[len(run.Events)-1]
	run.Events = append(run.Events[:len(run.Events)-1],
		&model.Event{
			Timestamp: reapNote.Timestamp.Add(-time.Millisecond),
			Type:      model.EventTypeArtifact,
			Name:      "session_snapshot",
			Attrs:     map[string]string{"path": snapshotPath},
		},
		reapNote,
	)
	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}
	st := &mockStore{runs: map[string]*model.Run{run.Ref().String(): run}}
	server := newTestServer(t, st)

	resp := server.handleProtoCaptureSession(&orchpb.CaptureSessionRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoCaptureSession() error = %s", resp.Error)
	}
	capture := resp.GetCaptureSession()
	want := "session reaped at 2026-07-13T10:11:12Z (reason=idle_ttl); serving final snapshot\nfinal pane output\n"
	if capture.GetContent() != want {
		t.Fatalf("capture content = %q, want %q", capture.GetContent(), want)
	}
	if capture.GetSource() != "session_snapshot" {
		t.Fatalf("capture source = %q, want session_snapshot", capture.GetSource())
	}
	if capture.GetTimestampUnix() != reapNote.Timestamp.Unix() {
		t.Fatalf("capture timestamp = %d, want reap note %d", capture.GetTimestampUnix(), reapNote.Timestamp.Unix())
	}
}

func TestHandleProtoCaptureSessionReapedSnapshotMissingNamesSidecar(t *testing.T) {
	missingPath := t.TempDir() + "/missing-snapshot.txt"
	run := sessionStateTestRun(t, "issue-missing-capture", "run-missing-capture", true, true)
	reapNote := run.Events[len(run.Events)-1]
	run.Events = append(run.Events[:len(run.Events)-1],
		&model.Event{
			Timestamp: reapNote.Timestamp.Add(-time.Millisecond),
			Type:      model.EventTypeArtifact,
			Name:      "session_snapshot",
			Attrs:     map[string]string{"path": missingPath},
		},
		reapNote,
	)
	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}
	st := &mockStore{runs: map[string]*model.Run{run.Ref().String(): run}}
	server := newTestServer(t, st)

	resp := server.handleProtoCaptureSession(&orchpb.CaptureSessionRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if resp.Ok {
		t.Fatal("capture unexpectedly succeeded with a missing sidecar")
	}
	if !strings.Contains(resp.Error, missingPath) {
		t.Fatalf("capture error = %q, want missing sidecar path %q", resp.Error, missingPath)
	}
}

func (s *resolveRunTestStore) GetRun(ref *model.RunRef) (*model.Run, error) {
	if ref == nil {
		return nil, fmt.Errorf("run reference required")
	}
	if ref.IsLatest() {
		return s.GetLatestRun(ref.IssueID)
	}
	return s.mockStore.GetRun(ref)
}

func (s *resolveRunTestStore) GetLatestRun(issueID model.IssueID) (*model.Run, error) {
	if s.latestRun != nil && s.latestRun.IssueID == issueID {
		return s.latestRun, nil
	}
	return nil, fmt.Errorf("no runs found for issue: %s", issueID)
}

func (s *resolveRunTestStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
	if ref != nil && event != nil {
		s.appendCalls = append(s.appendCalls, resolveRunAppendCall{ref: *ref, event: event})
	}
	if s.appendErr != nil {
		return s.appendErr
	}
	return s.mockStore.AppendEvent(ref, event)
}

func (s *resolveRunTestStore) SetIssueStatus(issueID model.IssueID, status model.IssueStatus) error {
	s.setIssueStatusCall = append(s.setIssueStatusCall, resolveRunSetIssueStatusCall{issueID: issueID, status: status})
	return s.setIssueErr
}

func (s *waitForRunsStatusStore) GetRun(ref *model.RunRef) (*model.Run, error) {
	if s.run == nil || ref == nil || ref.String() != s.run.Ref().String() {
		return nil, fmt.Errorf("run not found")
	}

	s.calls++
	copy := *s.run
	if s.calls > 1 {
		copy.Status = model.StatusWaiting
	}
	return &copy, nil
}

func (l *timingTestLogger) Printf(format string, v ...interface{}) {
	_, _ = fmt.Fprintf(&l.buf, format, v...)
	l.buf.WriteByte('\n')
}

func (l *timingTestLogger) String() string {
	return l.buf.String()
}

func newResolveRunHandlerTestServer(t *testing.T, st *resolveRunTestStore) (*SocketServer, *orchpb.RequestContext) {
	t.Helper()

	server := NewSocketServer(nil, nil)
	registerRepoContextForTest(t, server, "project-resolve-run", t.TempDir(), st)
	return server, &orchpb.RequestContext{ProjectId: "project-resolve-run"}
}

func TestHandleProtoResolveRunAppendsDoneAndResolvesIssue(t *testing.T) {
	run := &model.Run{IssueID: "orch-1", RunID: "run-1", Status: model.StatusRunning}
	st := &resolveRunTestStore{
		mockStore: mockStore{runs: map[string]*model.Run{run.Ref().String(): run}},
	}
	server, ctx := newResolveRunHandlerTestServer(t, st)

	resp := server.handleProtoResolveRun(&orchpb.ResolveRunRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Context: ctx,
	})

	if !resp.Ok {
		t.Fatalf("handleProtoResolveRun() ok=false error=%q", resp.Error)
	}
	if run.Status != model.StatusDone {
		t.Fatalf("run.Status = %q, want %q", run.Status, model.StatusDone)
	}
	if len(st.appendCalls) != 1 {
		t.Fatalf("append call count = %d, want 1", len(st.appendCalls))
	}
	call := st.appendCalls[0]
	if call.ref.IssueID != run.IssueID || call.ref.RunID != run.RunID {
		t.Fatalf("append ref = %s, want %s", call.ref.String(), run.Ref().String())
	}
	if call.event.Type != model.EventTypeStatus || call.event.Name != string(model.StatusDone) {
		t.Fatalf("append event = %s/%s, want status/done", call.event.Type, call.event.Name)
	}
	if got := call.event.Attrs["source"]; got != string(model.EventSourceUser) {
		t.Fatalf("append event source = %q, want %q", got, model.EventSourceUser)
	}
	if len(st.setIssueStatusCall) != 1 {
		t.Fatalf("set issue status call count = %d, want 1", len(st.setIssueStatusCall))
	}
	if got := st.setIssueStatusCall[0]; got.issueID != run.IssueID || got.status != model.IssueStatusResolved {
		t.Fatalf("set issue status = %s/%s, want %s/%s", got.issueID, got.status, run.IssueID, model.IssueStatusResolved)
	}
}

func TestHandleProtoResolveRunTerminalOnlyResolvesIssue(t *testing.T) {
	run := &model.Run{IssueID: "orch-2", RunID: "run-2", Status: model.StatusDone}
	st := &resolveRunTestStore{
		mockStore: mockStore{runs: map[string]*model.Run{run.Ref().String(): run}},
	}
	server, ctx := newResolveRunHandlerTestServer(t, st)

	resp := server.handleProtoResolveRun(&orchpb.ResolveRunRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Context: ctx,
	})

	if !resp.Ok {
		t.Fatalf("handleProtoResolveRun() ok=false error=%q", resp.Error)
	}
	if len(st.appendCalls) != 0 {
		t.Fatalf("append call count = %d, want 0 for terminal run", len(st.appendCalls))
	}
	if len(st.setIssueStatusCall) != 1 {
		t.Fatalf("set issue status call count = %d, want 1", len(st.setIssueStatusCall))
	}
	if got := st.setIssueStatusCall[0]; got.issueID != run.IssueID || got.status != model.IssueStatusResolved {
		t.Fatalf("set issue status = %s/%s, want %s/%s", got.issueID, got.status, run.IssueID, model.IssueStatusResolved)
	}
}

func TestHandleProtoResolveRunAppendFailureFailsResponse(t *testing.T) {
	run := &model.Run{IssueID: "orch-3", RunID: "run-3", Status: model.StatusRunning}
	st := &resolveRunTestStore{
		mockStore: mockStore{runs: map[string]*model.Run{run.Ref().String(): run}},
		appendErr: errors.New("append boom"),
	}
	server, ctx := newResolveRunHandlerTestServer(t, st)

	resp := server.handleProtoResolveRun(&orchpb.ResolveRunRequest{
		IssueId: string(run.IssueID),
		RunId:   string(run.RunID),
		Context: ctx,
	})

	if resp.Ok {
		t.Fatal("handleProtoResolveRun() ok=true, want failure")
	}
	if !strings.Contains(resp.Error, "failed to mark run done") || !strings.Contains(resp.Error, "append boom") {
		t.Fatalf("error = %q, want append failure detail", resp.Error)
	}
	if len(st.setIssueStatusCall) != 0 {
		t.Fatalf("set issue status call count = %d, want 0 after append failure", len(st.setIssueStatusCall))
	}
	if run.Status != model.StatusRunning {
		t.Fatalf("run.Status = %q, want unchanged %q", run.Status, model.StatusRunning)
	}
}

func TestHandleProtoResolveRunEmptyRunIDResolvesLatestRun(t *testing.T) {
	older := &model.Run{IssueID: "orch-4", RunID: "run-1", Status: model.StatusRunning}
	latest := &model.Run{IssueID: "orch-4", RunID: "run-2", Status: model.StatusWaiting}
	st := &resolveRunTestStore{
		mockStore: mockStore{runs: map[string]*model.Run{
			older.Ref().String():  older,
			latest.Ref().String(): latest,
		}},
		latestRun: latest,
	}
	server, ctx := newResolveRunHandlerTestServer(t, st)

	resp := server.handleProtoResolveRun(&orchpb.ResolveRunRequest{
		IssueId: string(latest.IssueID),
		Context: ctx,
	})

	if !resp.Ok {
		t.Fatalf("handleProtoResolveRun() ok=false error=%q", resp.Error)
	}
	if latest.Status != model.StatusDone {
		t.Fatalf("latest.Status = %q, want %q", latest.Status, model.StatusDone)
	}
	if older.Status != model.StatusRunning {
		t.Fatalf("older.Status = %q, want unchanged %q", older.Status, model.StatusRunning)
	}
	if len(st.appendCalls) != 1 || st.appendCalls[0].ref.RunID != latest.RunID {
		t.Fatalf("append calls = %+v, want exactly latest run %s", st.appendCalls, latest.RunID)
	}
	if len(st.setIssueStatusCall) != 1 || st.setIssueStatusCall[0].issueID != latest.IssueID {
		t.Fatalf("set issue status calls = %+v, want latest issue %s", st.setIssueStatusCall, latest.IssueID)
	}
}

func TestDaemonListRunsTimingEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty disabled", value: "", want: false},
		{name: "zero disabled", value: "0", want: false},
		{name: "false disabled", value: "false", want: false},
		{name: "one enabled", value: "1", want: true},
		{name: "true enabled", value: "true", want: true},
		{name: "yes enabled", value: "yes", want: true},
		{name: "on enabled", value: "on", want: true},
		{name: "mixed case true", value: " TrUe ", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(listRunsTimingEnv, tt.value)
			if got := daemonListRunsTimingEnabled(); got != tt.want {
				t.Fatalf("daemonListRunsTimingEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaybeLogListRunsTiming_DefaultFastNoLog(t *testing.T) {
	t.Setenv(listRunsTimingEnv, "")
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	server.maybeLogListRunsTiming(
		&orchpb.ListRunsRequest{IssueId: "orch-1", Limit: 10},
		3,
		10*time.Millisecond,
		20*time.Millisecond,
		30*time.Millisecond,
		nil,
	)

	if got := logger.String(); got != "" {
		t.Fatalf("expected no timing logs for fast request when env disabled, got %q", got)
	}
}

func TestMaybeLogListRunsTiming_SlowLogsWithoutEnv(t *testing.T) {
	t.Setenv(listRunsTimingEnv, "")
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	server.maybeLogListRunsTiming(
		&orchpb.ListRunsRequest{IssueId: "orch-1", Limit: 20},
		5,
		20*time.Millisecond,
		30*time.Millisecond,
		listRunsSlowThreshold+time.Millisecond,
		nil,
	)

	logText := logger.String()
	if !strings.Contains(logText, "list_runs timing") {
		t.Fatalf("expected timing log for slow request, got %q", logText)
	}
	if !strings.Contains(logText, "slow=true") {
		t.Fatalf("expected slow=true in log, got %q", logText)
	}
}

func TestMaybeLogListRunsTiming_EnvEnabledLogsFast(t *testing.T) {
	t.Setenv(listRunsTimingEnv, "true")
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	server.maybeLogListRunsTiming(
		&orchpb.ListRunsRequest{IssueId: "orch-2", Limit: 5, TextSearch: "poll", OlderThan: "2026-02-01T00:00:00Z"},
		2,
		8*time.Millisecond,
		9*time.Millisecond,
		17*time.Millisecond,
		nil,
	)

	logText := logger.String()
	if !strings.Contains(logText, "list_runs timing") {
		t.Fatalf("expected timing log when env enabled, got %q", logText)
	}
	if !strings.Contains(logText, "slow=false") {
		t.Fatalf("expected slow=false in log, got %q", logText)
	}
	if !strings.Contains(logText, "text_search=true") {
		t.Fatalf("expected text_search=true in log, got %q", logText)
	}
	if !strings.Contains(logText, "older_than=true") {
		t.Fatalf("expected older_than=true in log, got %q", logText)
	}
}

func TestMaybeLogListRunsTiming_LogsErrorDetails(t *testing.T) {
	t.Setenv(listRunsTimingEnv, "1")
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	server.maybeLogListRunsTiming(
		&orchpb.ListRunsRequest{IssueId: "orch-3", Limit: 1},
		0,
		5*time.Millisecond,
		0,
		5*time.Millisecond,
		errors.New("store boom"),
	)

	logText := logger.String()
	if !strings.Contains(logText, "error=store boom") {
		t.Fatalf("expected error details in timing log, got %q", logText)
	}
}

func TestBuildAttachInfoResponse(t *testing.T) {
	tests := []struct {
		name string
		run  *model.Run
		want struct {
			agent             string
			serverPort        int32
			opencodeSessionId string
			issueId           string
			runId             string
		}
	}{
		{
			name: "OpenCode run includes all fields",
			run: &model.Run{
				IssueID:           "orch-123",
				RunID:             "20260130-120000",
				Agent:             "opencode",
				WorktreePath:      "/path/to/worktree",
				SessionName:       "run-orch-123",
				ServerPort:        4097,
				OpenCodeSessionID: "ses_abc123",
			},
			want: struct {
				agent             string
				serverPort        int32
				opencodeSessionId string
				issueId           string
				runId             string
			}{
				agent:             "opencode",
				serverPort:        4097,
				opencodeSessionId: "ses_abc123",
				issueId:           "orch-123",
				runId:             "20260130-120000",
			},
		},
		{
			name: "Claude run (non-OpenCode) has zero server port",
			run: &model.Run{
				IssueID:      "orch-456",
				RunID:        "20260130-130000",
				Agent:        "claude",
				WorktreePath: "/path/to/worktree2",
				SessionName:  "run-orch-456",
				ServerPort:   0,
			},
			want: struct {
				agent             string
				serverPort        int32
				opencodeSessionId string
				issueId           string
				runId             string
			}{
				agent:             "claude",
				serverPort:        0,
				opencodeSessionId: "",
				issueId:           "orch-456",
				runId:             "20260130-130000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachInfo := &orchpb.GetAttachInfoResponse{
				Agent:             tt.run.Agent,
				ServerPort:        int32(tt.run.ServerPort),
				OpencodeSessionId: tt.run.OpenCodeSessionID,
				IssueId:           string(tt.run.IssueID),
				RunId:             string(tt.run.RunID),
			}

			if attachInfo.Agent != tt.want.agent {
				t.Errorf("Agent = %q, want %q", attachInfo.Agent, tt.want.agent)
			}
			if attachInfo.ServerPort != tt.want.serverPort {
				t.Errorf("ServerPort = %d, want %d", attachInfo.ServerPort, tt.want.serverPort)
			}
			if attachInfo.OpencodeSessionId != tt.want.opencodeSessionId {
				t.Errorf("OpencodeSessionId = %q, want %q", attachInfo.OpencodeSessionId, tt.want.opencodeSessionId)
			}
			if attachInfo.IssueId != tt.want.issueId {
				t.Errorf("IssueId = %q, want %q", attachInfo.IssueId, tt.want.issueId)
			}
			if attachInfo.RunId != tt.want.runId {
				t.Errorf("RunId = %q, want %q", attachInfo.RunId, tt.want.runId)
			}
		})
	}
}

func TestIsOpenCodeRun(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		want  bool
	}{
		{
			name:  "opencode agent",
			agent: "opencode",
			want:  true,
		},
		{
			name:  "claude agent",
			agent: "claude",
			want:  false,
		},
		{
			name:  "codex agent",
			agent: "codex",
			want:  false,
		},
		{
			name:  "gemini agent",
			agent: "gemini",
			want:  false,
		},
		{
			name:  "empty agent",
			agent: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isOpenCode := tt.agent == "opencode"
			if isOpenCode != tt.want {
				t.Errorf("isOpenCode(%q) = %v, want %v", tt.agent, isOpenCode, tt.want)
			}
		})
	}
}

func TestOpenCodeAttachValidation(t *testing.T) {
	tests := []struct {
		name      string
		run       *model.Run
		wantError string
		wantOK    bool
	}{
		{
			name: "OpenCode run with valid server port succeeds",
			run: &model.Run{
				Agent:      "opencode",
				ServerPort: 4097,
			},
			wantError: "",
			wantOK:    true,
		},
		{
			name: "OpenCode run without server port fails",
			run: &model.Run{
				Agent:      "opencode",
				ServerPort: 0,
			},
			wantError: "opencode_server_not_found",
			wantOK:    false,
		},
		{
			name: "Non-OpenCode run doesn't check server port",
			run: &model.Run{
				Agent:      "claude",
				ServerPort: 0,
			},
			wantError: "",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isOpenCode := tt.run.Agent == "opencode"

			var gotError string
			var gotOK bool

			if isOpenCode {
				if tt.run.ServerPort == 0 {
					gotError = "opencode_server_not_found"
					gotOK = false
				} else {
					gotOK = true
				}
			} else {
				gotOK = true
			}

			if gotOK != tt.wantOK {
				t.Errorf("OK = %v, want %v", gotOK, tt.wantOK)
			}
			if gotError != tt.wantError {
				t.Errorf("Error = %q, want %q", gotError, tt.wantError)
			}
		})
	}
}

func TestComputeBranchState(t *testing.T) {
	tests := []struct {
		name         string
		worktreePath string
		wantState    orchpb.BranchState
	}{
		{
			name:         "empty worktree path returns unspecified",
			worktreePath: "",
			wantState:    orchpb.BranchState_BRANCH_STATE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeBranchState(tt.worktreePath, "", "main")
			if got != tt.wantState {
				t.Errorf("computeBranchState(%q, ...) = %v, want %v", tt.worktreePath, got, tt.wantState)
			}
		})
	}
}

func TestNewEventSetsTimestamp(t *testing.T) {
	before := time.Now()
	event := model.NewEvent(model.EventTypeStatus, "running", nil)
	after := time.Now()

	if event.Timestamp.IsZero() {
		t.Error("NewEvent should set a non-zero timestamp")
	}
	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Errorf("NewEvent timestamp %v should be between %v and %v", event.Timestamp, before, after)
	}
	if event.Type != model.EventTypeStatus {
		t.Errorf("event.Type = %v, want %v", event.Type, model.EventTypeStatus)
	}
	if event.Name != "running" {
		t.Errorf("event.Name = %q, want %q", event.Name, "running")
	}
}

func TestProtoAppendEventUsesNewEvent(t *testing.T) {
	req := &orchpb.AppendEventRequest{
		IssueId:    "test-001",
		RunId:      "20260130-120000",
		EventType:  "status",
		EventName:  "running",
		EventAttrs: map[string]string{"source": "agent"},
	}

	before := time.Now()
	event := model.NewEvent(model.EventType(req.EventType), req.EventName, req.EventAttrs)
	after := time.Now()

	if event.Timestamp.IsZero() {
		t.Error("event created for proto AppendEvent should have non-zero timestamp")
	}
	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Errorf("event timestamp %v should be between %v and %v", event.Timestamp, before, after)
	}
	if event.Type != model.EventTypeStatus {
		t.Errorf("event.Type = %v, want %v", event.Type, model.EventTypeStatus)
	}
	if event.Name != "running" {
		t.Errorf("event.Name = %q, want %q", event.Name, "running")
	}
	if event.Attrs["source"] != "agent" {
		t.Errorf("event.Attrs[source] = %q, want %q", event.Attrs["source"], "agent")
	}
}

func TestWorkerProtocolRegisterHeartbeatListUnregister(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	workerID := "worker-test-1"

	registerResp := server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:   workerID,
		WorkerType: "embedded",
		Host:       "localhost",
		Mode:       "co-located",
	})
	if !registerResp.GetOk() {
		t.Fatalf("register worker failed: %s", registerResp.GetError())
	}
	if registerResp.GetRegisterWorker().GetWorkerId() != workerID {
		t.Fatalf("worker_id = %q, want %q", registerResp.GetRegisterWorker().GetWorkerId(), workerID)
	}

	listResp := server.handleProtoListWorkers(&orchpb.ListWorkersRequest{})
	if !listResp.GetOk() {
		t.Fatalf("list workers failed: %s", listResp.GetError())
	}
	if !containsWorkerID(listResp.GetListWorkers().GetWorkers(), workerID) {
		t.Fatalf("expected worker %q in list", workerID)
	}

	hbResp := server.handleProtoWorkerHeartbeat(&orchpb.WorkerHeartbeatRequest{WorkerId: workerID})
	if !hbResp.GetOk() {
		t.Fatalf("worker heartbeat failed: %s", hbResp.GetError())
	}
	if hbResp.GetWorkerHeartbeat().GetHeartbeatTtlSeconds() <= 0 {
		t.Fatalf("expected positive heartbeat TTL")
	}

	unregisterResp := server.handleProtoUnregisterWorker(&orchpb.UnregisterWorkerRequest{WorkerId: workerID})
	if !unregisterResp.GetOk() {
		t.Fatalf("unregister worker failed: %s", unregisterResp.GetError())
	}

	listAfterResp := server.handleProtoListWorkers(&orchpb.ListWorkersRequest{})
	if !listAfterResp.GetOk() {
		t.Fatalf("list workers after unregister failed: %s", listAfterResp.GetError())
	}
	if containsWorkerID(listAfterResp.GetListWorkers().GetWorkers(), workerID) {
		t.Fatalf("worker %q should be absent after unregister", workerID)
	}
}

func TestWorkerProtocolLeaseAndAcknowledge(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	workerID := "worker-test-2"
	registerResp := server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:   workerID,
		WorkerType: "embedded",
		Host:       "localhost",
		Mode:       "co-located",
	})
	if !registerResp.GetOk() {
		t.Fatalf("register worker failed: %s", registerResp.GetError())
	}

	leasePayload := &WorkerEffectPayload{StartRun: &StartRunOptions{IssueID: "orch-1"}}
	lease, err := server.acquireWorkerLease("project-test", "start_run", "orch-1", "run-1", leasePayload)
	if err != nil {
		t.Fatalf("acquire lease failed: %v", err)
	}
	if lease.WorkerID == "" || lease.LeaseID == "" {
		t.Fatalf("invalid lease payload: %+v", lease)
	}

	leaseResp := server.handleProtoLeaseWork(&orchpb.LeaseWorkRequest{WorkerId: lease.WorkerID})
	if !leaseResp.GetOk() {
		t.Fatalf("lease work failed: %s", leaseResp.GetError())
	}
	if leaseResp.GetLeaseWork().GetLeaseId() != lease.LeaseID {
		t.Fatalf("lease_id = %q, want %q", leaseResp.GetLeaseWork().GetLeaseId(), lease.LeaseID)
	}
	if strings.TrimSpace(leaseResp.GetLeaseWork().GetPayloadJson()) == "" {
		t.Fatalf("expected lease payload_json for start_run lease")
	}

	leaseResp2 := server.handleProtoLeaseWork(&orchpb.LeaseWorkRequest{WorkerId: lease.WorkerID})
	if !leaseResp2.GetOk() {
		t.Fatalf("second lease work failed: %s", leaseResp2.GetError())
	}
	if leaseResp2.GetLeaseWork().GetLeaseId() != "" {
		t.Fatalf("expected no second active lease before expiry, got %q", leaseResp2.GetLeaseWork().GetLeaseId())
	}

	ackResp := server.handleProtoAcknowledgeEffect(&orchpb.AcknowledgeEffectRequest{
		WorkerId:   lease.WorkerID,
		LeaseId:    lease.LeaseID,
		Success:    true,
		ResultJson: `{"start_run_result":{"run_id":"run-1"}}`,
	})
	if !ackResp.GetOk() {
		t.Fatalf("acknowledge effect failed: %s", ackResp.GetError())
	}

	activeLeases := server.listWorkerLeases(false)
	for _, l := range activeLeases {
		if l.LeaseID == lease.LeaseID {
			t.Fatalf("lease %q should be completed and absent from active leases", lease.LeaseID)
		}
	}

	allLeases := server.listWorkerLeases(true)
	completed := false
	for _, l := range allLeases {
		if l.LeaseID == lease.LeaseID {
			completed = l.Completed && l.Success && strings.TrimSpace(l.ResultJSON) != ""
			break
		}
	}
	if !completed {
		t.Fatalf("expected completed successful lease record for %q", lease.LeaseID)
	}
}

func TestWorkerProtocolLeaseRedispatchAfterExpiry(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	workerID := "worker-test-3"
	registerResp := server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:   workerID,
		WorkerType: "embedded",
		Host:       "localhost",
		Mode:       "co-located",
	})
	if !registerResp.GetOk() {
		t.Fatalf("register worker failed: %s", registerResp.GetError())
	}

	lease, err := server.acquireWorkerLease("project-test", "continue_run", "orch-2", "run-2", nil)
	if err != nil {
		t.Fatalf("acquire lease failed: %v", err)
	}

	firstLeaseResp := server.handleProtoLeaseWork(&orchpb.LeaseWorkRequest{WorkerId: workerID})
	if !firstLeaseResp.GetOk() {
		t.Fatalf("first lease work failed: %s", firstLeaseResp.GetError())
	}
	if firstLeaseResp.GetLeaseWork().GetLeaseId() != lease.LeaseID {
		t.Fatalf("lease_id = %q, want %q", firstLeaseResp.GetLeaseWork().GetLeaseId(), lease.LeaseID)
	}

	server.workerLeasesMu.Lock()
	stored := server.workerLeases[lease.LeaseID]
	stored.ExpiresAt = time.Now().Add(-1 * time.Second)
	server.workerLeasesMu.Unlock()

	secondLeaseResp := server.handleProtoLeaseWork(&orchpb.LeaseWorkRequest{WorkerId: workerID})
	if !secondLeaseResp.GetOk() {
		t.Fatalf("second lease work failed: %s", secondLeaseResp.GetError())
	}
	if secondLeaseResp.GetLeaseWork().GetLeaseId() != lease.LeaseID {
		t.Fatalf("expected same lease to be re-dispatched after expiry, got %q", secondLeaseResp.GetLeaseWork().GetLeaseId())
	}

	server.workerLeasesMu.RLock()
	redispatched := server.workerLeases[lease.LeaseID]
	dispatchCount := redispatched.DispatchCount
	server.workerLeasesMu.RUnlock()
	if dispatchCount < 2 {
		t.Fatalf("dispatch_count = %d, want >= 2", dispatchCount)
	}
}

func TestWithWorkerLeaseDispatchesViaLeasePath(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	workerID := "lease-dispatch-worker"
	if _, ttl := server.registerWorker(workerID, "external", "localhost", "external", []string{"stop_run"}); ttl <= 0 {
		t.Fatal("expected positive heartbeat ttl for helper worker")
	}

	dispatchErr := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			lease := server.leaseWorkForWorker(workerID)
			if lease != nil {
				_, err := server.executeLeaseEffect(lease)
				if err != nil {
					_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, false, err.Error(), "")
					dispatchErr <- err
					return
				}
				_ = server.acknowledgeWorkerLease(workerID, lease.LeaseID, true, "", "")
				dispatchErr <- nil
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		dispatchErr <- fmt.Errorf("timed out waiting for lease")
	}()

	_, err := server.withWorkerLease("project-test", "stop_run", "orch-3", "run-3", nil)
	if err == nil {
		t.Fatal("withWorkerLease() error = nil, want project mapping error")
	}
	if !strings.Contains(err.Error(), "no local project mapping for project_id") {
		t.Fatalf("withWorkerLease() error = %v, want local project mapping error", err)
	}

	if helperErr := <-dispatchErr; helperErr == nil || !strings.Contains(helperErr.Error(), "no local project mapping for project_id") {
		t.Fatalf("helper dispatch error = %v, want local project mapping error", helperErr)
	}

	allLeases := server.listWorkerLeases(true)
	var matched *WorkerLease
	for _, lease := range allLeases {
		if lease.ProjectID == "project-test" && lease.Effect == "stop_run" && lease.IssueID == "orch-3" && lease.RunID == "run-3" {
			matched = lease
			break
		}
	}
	if matched == nil {
		t.Fatal("expected completed lease record for dispatched effect")
	}
	if !matched.Completed || matched.Success {
		t.Fatalf("expected completed failed lease, got completed=%v success=%v", matched.Completed, matched.Success)
	}
	if matched.DispatchCount < 1 {
		t.Fatalf("dispatch_count = %d, want >= 1", matched.DispatchCount)
	}
}

func TestWithWorkerLeaseFailsWhenDispatcherNotStarted(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	_, err := server.withWorkerLease("project-test", "stop_run", "orch-4", "run-4", nil)
	if err == nil {
		t.Fatal("withWorkerLease() error = nil, want no active workers error")
	}
	if !strings.Contains(err.Error(), "no active workers available") {
		t.Fatalf("withWorkerLease() error = %v, want no active workers error", err)
	}
}

func TestWorkerProtocolRejectsUnauthorizedRequests(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)
	server.workerAuthToken = "secret-token"

	regResp := server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{WorkerId: "w-auth"})
	if regResp.GetOk() {
		t.Fatal("expected unauthorized register worker response")
	}

	regResp = server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{WorkerId: "w-auth", AuthToken: "secret-token"})
	if !regResp.GetOk() {
		t.Fatalf("register worker with token failed: %s", regResp.GetError())
	}

	hbResp := server.handleProtoWorkerHeartbeat(&orchpb.WorkerHeartbeatRequest{WorkerId: "w-auth"})
	if hbResp.GetOk() {
		t.Fatal("expected unauthorized heartbeat response")
	}

	leaseResp := server.handleProtoLeaseWork(&orchpb.LeaseWorkRequest{WorkerId: "w-auth"})
	if leaseResp.GetOk() {
		t.Fatal("expected unauthorized lease work response")
	}

	ackResp := server.handleProtoAcknowledgeEffect(&orchpb.AcknowledgeEffectRequest{WorkerId: "w-auth", LeaseId: "lease-x"})
	if ackResp.GetOk() {
		t.Fatal("expected unauthorized acknowledge response")
	}
}

func TestWorkerProtocolCapabilityBasedRouting(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:     "worker-start",
		WorkerType:   "external",
		Host:         "localhost",
		Mode:         "external",
		Capabilities: []string{"start_run"},
	})
	server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:     "worker-stop",
		WorkerType:   "external",
		Host:         "localhost",
		Mode:         "external",
		Capabilities: []string{"stop_run"},
	})

	lease, err := server.acquireWorkerLease("project-test", "stop_run", "orch-cap", "run-cap", nil)
	if err != nil {
		t.Fatalf("acquireWorkerLease() error = %v", err)
	}
	if lease.WorkerID != "worker-stop" {
		t.Fatalf("lease worker = %q, want worker-stop", lease.WorkerID)
	}

	workersResp := server.handleProtoListWorkers(&orchpb.ListWorkersRequest{})
	if !workersResp.GetOk() {
		t.Fatalf("list workers failed: %s", workersResp.GetError())
	}
	var seenCaps bool
	for _, w := range workersResp.GetListWorkers().GetWorkers() {
		if w.GetId() == "worker-stop" {
			if len(w.GetCapabilities()) == 0 || w.GetCapabilities()[0] != "stop_run" {
				t.Fatalf("unexpected capabilities for worker-stop: %v", w.GetCapabilities())
			}
			seenCaps = true
		}
	}
	if !seenCaps {
		t.Fatal("expected worker-stop in list workers response")
	}
}

func TestWorkerProtocolCapabilityRoutingFailsWithoutSupportingWorker(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:     "worker-start-only",
		WorkerType:   "external",
		Host:         "localhost",
		Mode:         "external",
		Capabilities: []string{"start_run"},
	})

	_, err := server.acquireWorkerLease("project-test", "stop_run", "orch-cap-miss", "run-cap-miss", nil)
	if err == nil {
		t.Fatal("acquireWorkerLease() error = nil, want no active workers available")
	}
	if !strings.Contains(err.Error(), "no active workers available") {
		t.Fatalf("acquireWorkerLease() error = %v, want no active workers available", err)
	}
}

func TestWorkerSchedulingSkipsStaleWorkersAfterHeartbeatTTL(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:     "worker-stale",
		WorkerType:   "external",
		Host:         "localhost",
		Mode:         "external",
		Capabilities: []string{"stop_run"},
	})

	server.workersMu.Lock()
	server.workers["worker-stale"].LastHeartbeat = time.Now().Add(-1 * workerHeartbeatTTL).Add(-1 * time.Second)
	server.workersMu.Unlock()

	_, err := server.acquireWorkerLease("project-test", "stop_run", "orch-stale", "run-stale", nil)
	if err == nil {
		t.Fatal("acquireWorkerLease() error = nil, want no active workers available")
	}
	if !strings.Contains(err.Error(), "no active workers available") {
		t.Fatalf("acquireWorkerLease() error = %v, want no active workers available", err)
	}
}

func TestWorkerLeaseRedispatchToSecondWorkerAfterExpiry(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:     "worker-a",
		WorkerType:   "external",
		Host:         "localhost",
		Mode:         "external",
		Capabilities: []string{"stop_run"},
	})
	server.handleProtoRegisterWorker(&orchpb.RegisterWorkerRequest{
		WorkerId:     "worker-b",
		WorkerType:   "external",
		Host:         "localhost",
		Mode:         "external",
		Capabilities: []string{"stop_run"},
	})

	server.workersMu.Lock()
	server.workers["worker-a"].LastHeartbeat = time.Now().Add(2 * time.Second)
	server.workers["worker-b"].LastHeartbeat = time.Now().Add(1 * time.Second)
	server.workersMu.Unlock()

	lease, err := server.acquireWorkerLease("project-test", "stop_run", "orch-redispatch", "run-redispatch", nil)
	if err != nil {
		t.Fatalf("acquireWorkerLease() error = %v", err)
	}
	if lease.WorkerID != "worker-a" {
		t.Fatalf("expected first lease owner worker-a, got %s", lease.WorkerID)
	}

	first := server.handleProtoLeaseWork(&orchpb.LeaseWorkRequest{WorkerId: "worker-a"})
	if !first.GetOk() || first.GetLeaseWork().GetLeaseId() != lease.LeaseID {
		t.Fatalf("first lease dispatch mismatch: ok=%v lease_id=%q", first.GetOk(), first.GetLeaseWork().GetLeaseId())
	}

	server.workerLeasesMu.Lock()
	server.workerLeases[lease.LeaseID].ExpiresAt = time.Now().Add(-1 * time.Second)
	server.workerLeases[lease.LeaseID].WorkerID = "worker-b"
	server.workerLeasesMu.Unlock()

	second := server.handleProtoLeaseWork(&orchpb.LeaseWorkRequest{WorkerId: "worker-b"})
	if !second.GetOk() {
		t.Fatalf("second lease dispatch failed: %s", second.GetError())
	}
	if second.GetLeaseWork().GetLeaseId() != lease.LeaseID {
		t.Fatalf("expected redispatched lease %q, got %q", lease.LeaseID, second.GetLeaseWork().GetLeaseId())
	}
}

func TestSyncStartRunResultToMasterStorePreservesOpenCodeArtifacts(t *testing.T) {
	st := &mockStore{
		runs: map[string]*model.Run{},
		issues: map[string]*model.Issue{
			// The master store owns the issue; syncStartRunResultToMasterStore runs
			// on the master and creates the run projection via the verifying CreateRun.
			"issue-opencode-sync": {ID: "issue-opencode-sync", Status: model.IssueStatusOpen},
		},
	}
	server := NewSocketServer(nil, &timingTestLogger{})

	err := server.syncStartRunResultToMasterStore(st, &orchpb.StartRunRequest{
		IssueId: "issue-opencode-sync",
		Agent:   "opencode",
		Context: &orchpb.RequestContext{ProjectId: "project-sync"},
	}, &StartRunResult{
		RunID:             "run-opencode-sync",
		Branch:            "issue/issue-opencode-sync/run-run-opencode-sync",
		WorktreePath:      "/tmp/opencode-sync",
		SessionName:       "session-opencode-sync",
		Status:            string(model.StatusRunning),
		Multiplexer:       "tmux",
		SessionHost:       "mac-host",
		WorkerID:          "host-mac-host",
		ServerPort:        4111,
		OpenCodeSessionID: "ses_sync",
	}, "", "")
	if err != nil {
		t.Fatalf("syncStartRunResultToMasterStore() error = %v", err)
	}

	run, err := st.GetRun(&model.RunRef{IssueID: "issue-opencode-sync", RunID: "run-opencode-sync"})
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run == nil {
		t.Fatal("expected synced run")
	}
	if run.ServerPort != 4111 {
		t.Fatalf("ServerPort = %d, want 4111", run.ServerPort)
	}
	if run.OpenCodeSessionID != "ses_sync" {
		t.Fatalf("OpenCodeSessionID = %q, want %q", run.OpenCodeSessionID, "ses_sync")
	}
	if run.TargetHost != "mac-host" {
		t.Fatalf("TargetHost = %q, want %q", run.TargetHost, "mac-host")
	}
}

type artifactFailingStore struct {
	mockStore
	appendErr error
}

func (s *artifactFailingStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
	if event != nil && event.Type == model.EventTypeArtifact {
		return s.appendErr
	}
	return s.mockStore.AppendEvent(ref, event)
}

func TestSyncStartRunResultToMasterStoreLogsArtifactAppendFailure(t *testing.T) {
	st := &artifactFailingStore{
		mockStore: mockStore{
			runs: map[string]*model.Run{},
			issues: map[string]*model.Issue{
				"issue-artifact-log": {ID: "issue-artifact-log", Status: model.IssueStatusOpen},
			},
		},
		appendErr: errors.New("artifact append failed"),
	}
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)

	err := server.syncStartRunResultToMasterStore(st, &orchpb.StartRunRequest{
		IssueId: "issue-artifact-log",
		Agent:   "opencode",
	}, &StartRunResult{
		RunID:        "run-artifact-log",
		WorktreePath: "/tmp/artifact-log",
		Status:       string(model.StatusRunning),
	}, "", "")
	if err != nil {
		t.Fatalf("syncStartRunResultToMasterStore() error = %v", err)
	}

	logOutput := logger.String()
	wantLog := "issue-artifact-log#run-artifact-log: failed to record worktree artifact: artifact append failed"
	if !strings.Contains(logOutput, wantLog) {
		t.Fatalf("log output %q does not contain %q", logOutput, wantLog)
	}

	run, err := st.GetRun(&model.RunRef{IssueID: "issue-artifact-log", RunID: "run-artifact-log"})
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.Status != model.StatusRunning {
		t.Fatalf("run.Status = %q, want %q", run.Status, model.StatusRunning)
	}
}

func TestHandleProtoWaitForRunsReturnsImmediatelyForShortID(t *testing.T) {
	run := &model.Run{
		IssueID: "issue-1",
		RunID:   "20260101-010101",
		Status:  model.StatusPROpen,
		PRUrl:   "https://example.test/pr/123",
	}
	st := &mockStore{
		runs: map[string]*model.Run{
			run.Ref().String(): run,
		},
	}
	server := newTestServer(t, st)

	resp := server.handleProtoWaitForRuns(&orchpb.WaitForRunsRequest{
		RunRefs: []string{string(run.ShortID())},
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoWaitForRuns() error = %s", resp.Error)
	}

	waitResp := resp.GetWaitForRuns()
	if waitResp == nil {
		t.Fatal("expected WaitForRuns response payload")
	}
	if waitResp.RunId != string(run.ShortID()) {
		t.Fatalf("run_id = %q, want %q", waitResp.RunId, run.ShortID())
	}
	if waitResp.Status != string(model.StatusPROpen) {
		t.Fatalf("status = %q, want %q", waitResp.Status, model.StatusPROpen)
	}
	if waitResp.Issue != string(run.IssueID) {
		t.Fatalf("issue = %q, want %q", waitResp.Issue, run.IssueID)
	}
	if waitResp.PrUrl != run.PRUrl {
		t.Fatalf("pr_url = %q, want %q", waitResp.PrUrl, run.PRUrl)
	}
}

func TestHandleProtoWaitForRunsReturnsImmediatelyForReapedRun(t *testing.T) {
	origPollInterval := waitForRunsPollInterval
	origTimeoutUnit := waitForRunsTimeoutUnit
	waitForRunsPollInterval = time.Millisecond
	waitForRunsTimeoutUnit = 100 * time.Millisecond
	t.Cleanup(func() {
		waitForRunsPollInterval = origPollInterval
		waitForRunsTimeoutUnit = origTimeoutUnit
	})

	run := sessionStateTestRun(t, "issue-reaped-wait", "20260713-reaped-wait", true, true)
	st := &mockStore{runs: map[string]*model.Run{run.Ref().String(): run}}
	server := newTestServer(t, st)

	started := time.Now()
	resp := server.handleProtoWaitForRuns(&orchpb.WaitForRunsRequest{
		RunRefs:        []string{run.Ref().String()},
		TimeoutSeconds: 1,
		Context:        &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoWaitForRuns() error = %s", resp.Error)
	}
	if elapsed := time.Since(started); elapsed >= waitForRunsTimeoutUnit {
		t.Fatalf("reaped wait took %s, want prompt return before %s", elapsed, waitForRunsTimeoutUnit)
	}
	if got := resp.GetWaitForRuns().GetStatus(); got != string(model.StatusRunning) {
		t.Fatalf("status = %q, want running lifecycle status", got)
	}
}

func TestHandleProtoWaitForRunsWaitsForStatusChange(t *testing.T) {
	origPollInterval := waitForRunsPollInterval
	origTimeoutUnit := waitForRunsTimeoutUnit
	waitForRunsPollInterval = 5 * time.Millisecond
	waitForRunsTimeoutUnit = 10 * time.Millisecond
	t.Cleanup(func() {
		waitForRunsPollInterval = origPollInterval
		waitForRunsTimeoutUnit = origTimeoutUnit
	})

	run := &model.Run{
		IssueID: "issue-2",
		RunID:   "20260101-020202",
		Status:  model.StatusRunning,
	}
	st := &waitForRunsStatusStore{
		mockStore: mockStore{
			runs: map[string]*model.Run{
				run.Ref().String(): run,
			},
		},
		run: run,
	}
	server := newTestServer(t, st)

	resp := server.handleProtoWaitForRuns(&orchpb.WaitForRunsRequest{
		RunRefs: []string{run.Ref().String()},
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if !resp.Ok {
		t.Fatalf("handleProtoWaitForRuns() error = %s", resp.Error)
	}

	waitResp := resp.GetWaitForRuns()
	if waitResp == nil {
		t.Fatal("expected WaitForRuns response payload")
	}
	if waitResp.Status != string(model.StatusWaiting) {
		t.Fatalf("status = %q, want %q", waitResp.Status, model.StatusWaiting)
	}
}

func TestHandleProtoWaitForRunsTimesOut(t *testing.T) {
	origPollInterval := waitForRunsPollInterval
	origTimeoutUnit := waitForRunsTimeoutUnit
	waitForRunsPollInterval = 5 * time.Millisecond
	waitForRunsTimeoutUnit = 10 * time.Millisecond
	t.Cleanup(func() {
		waitForRunsPollInterval = origPollInterval
		waitForRunsTimeoutUnit = origTimeoutUnit
	})

	run := &model.Run{
		IssueID: "issue-3",
		RunID:   "20260101-030303",
		Status:  model.StatusRunning,
	}
	st := &mockStore{
		runs: map[string]*model.Run{
			run.Ref().String(): run,
		},
	}
	server := newTestServer(t, st)

	resp := server.handleProtoWaitForRuns(&orchpb.WaitForRunsRequest{
		RunRefs:        []string{run.Ref().String()},
		TimeoutSeconds: 3,
		Context:        &orchpb.RequestContext{ProjectId: testProjectID},
	})
	if resp.Ok {
		t.Fatal("expected timeout error response")
	}
	if resp.Error != "timeout" {
		t.Fatalf("error = %q, want %q", resp.Error, "timeout")
	}
}

func containsWorkerID(workers []*orchpb.WorkerInfo, workerID string) bool {
	for _, worker := range workers {
		if worker.GetId() == workerID {
			return true
		}
	}
	return false
}
