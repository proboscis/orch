package daemon

import (
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/multiplexer"
	"github.com/proboscis/orch/internal/store"
	filestore "github.com/proboscis/orch/internal/store/file"
)

// mockReviveMux records the session boot the revive physical path performs.
type mockReviveMux struct {
	hasSession bool
	newErr     error
	booted     []*multiplexer.SessionConfig
}

func (m *mockReviveMux) Type() multiplexer.Type { return multiplexer.TypeTmux }
func (m *mockReviveMux) HasSession(string) bool { return m.hasSession }
func (m *mockReviveMux) NewSession(cfg *multiplexer.SessionConfig) error {
	m.booted = append(m.booted, cfg)
	return m.newErr
}

// createReapedTestRun builds a run whose CURRENT generation is recorded as
// reaped: terminal status, agent_session g1, session_reaped note g1. The
// worktree is a real directory so the local physical preconditions hold.
func createReapedTestRun(t *testing.T, agentName string, withIdentity bool) (store.Store, *model.Run) {
	t.Helper()
	root := t.TempDir()
	st, err := filestore.New(root)
	if err != nil {
		t.Fatalf("filestore.New() error = %v", err)
	}
	issueID := model.IssueID("revive-issue")
	if err := st.CreateIssue(&model.Issue{ID: issueID, Title: "Revive issue", Status: model.IssueStatusOpen}); err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}
	runID := model.RunID("20260713-180000")
	if _, err := st.CreateRun(issueID, runID, map[string]string{"agent": agentName}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	worktree := t.TempDir()
	sessionName := model.GenerateSessionName(issueID, runID)
	events := []*model.Event{
		{Timestamp: now.Add(-5 * time.Second), Type: model.EventTypeArtifact, Name: "worktree", Attrs: map[string]string{"path": worktree}},
		{Timestamp: now.Add(-4 * time.Second), Type: model.EventTypeArtifact, Name: "session", Attrs: map[string]string{"name": sessionName, "multiplexer": "tmux"}},
	}
	if withIdentity {
		events = append(events, &model.Event{Timestamp: now.Add(-3 * time.Second), Type: model.EventTypeArtifact, Name: "agent_session", Attrs: map[string]string{"backend": agentName, "id": "conv-8888", "generation": "1"}})
	}
	events = append(events,
		&model.Event{Timestamp: now.Add(-2 * time.Second), Type: model.EventTypeStatus, Name: string(model.StatusDone), Attrs: map[string]string{"source": string(model.EventSourceDaemon)}},
		&model.Event{Timestamp: now.Add(-time.Second), Type: model.EventTypeNote, Name: model.DaemonNoticeEventName, Attrs: map[string]string{"kind": "session_reaped", "generation": "1", "session_name": sessionName, "reason": "terminal_grace"}},
	)
	for _, event := range events {
		if err := st.AppendEvent(&model.RunRef{IssueID: issueID, RunID: runID}, event); err != nil {
			t.Fatalf("AppendEvent(%s) error = %v", event.Name, err)
		}
	}
	run, err := st.GetRun(&model.RunRef{IssueID: issueID, RunID: runID})
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if !run.SessionReaped() {
		t.Fatalf("precondition: run must fold as reaped (gen=%d reaped=%v)", run.AgentSessionGeneration, run.SessionReaped())
	}
	return st, run
}

func withMockReviveMux(t *testing.T, mux *mockReviveMux) {
	t.Helper()
	prev := getReviveMultiplexer
	getReviveMultiplexer = func(run *model.Run) (reviveMultiplexer, error) { return mux, nil }
	t.Cleanup(func() { getReviveMultiplexer = prev })
}

func TestReviveBootsReapedClaudeSessionWithNativeResume(t *testing.T) {
	st, run := createReapedTestRun(t, "claude", true)
	mux := &mockReviveMux{}
	withMockReviveMux(t, mux)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))

	fresh, err := server.reviveIfReaped(st, "project", t.TempDir(), run)
	if err != nil {
		t.Fatalf("reviveIfReaped() error = %v", err)
	}

	// (1) the physical boot resumed the SAME identity natively.
	if len(mux.booted) != 1 {
		t.Fatalf("expected exactly one session boot, got %d", len(mux.booted))
	}
	boot := mux.booted[0]
	if boot.SessionName != model.GenerateSessionName(run.IssueID, run.RunID) {
		t.Fatalf("revive must reuse the run session name, got %q", boot.SessionName)
	}
	if !strings.Contains(boot.Command, "--resume conv-8888") {
		t.Fatalf("claude revive must resume the recorded identity, got %q", boot.Command)
	}
	if strings.Contains(boot.Command, "--session-id") {
		t.Fatalf("claude revive must not mint a new session id (CLI rejects it with --resume), got %q", boot.Command)
	}
	if boot.WorkDir != run.WorktreePath {
		t.Fatalf("revive must boot in the recorded worktree, got %q want %q", boot.WorkDir, run.WorktreePath)
	}

	// (2) the ledger: session_revived note + agent_session g2 + user-sourced
	// status re-entry, and the L-S3 latch is dissolved.
	if fresh.SessionReaped() {
		t.Fatal("revive must dissolve the SessionReaped latch (agent_session generation+1)")
	}
	if fresh.AgentSessionID != "conv-8888" || fresh.AgentSessionGeneration != 2 {
		t.Fatalf("revive identity fold = (%q, %d), want (conv-8888, 2)", fresh.AgentSessionID, fresh.AgentSessionGeneration)
	}
	if fresh.Status != model.StatusRunning {
		t.Fatalf("revive must re-enter running, got %s", fresh.Status)
	}
	var revivedNote, userStatus bool
	for _, e := range fresh.Events {
		if e.Type == model.EventTypeNote && e.Name == model.DaemonNoticeEventName && e.Attrs["kind"] == "session_revived" && e.Attrs["generation"] == "2" {
			revivedNote = true
		}
		if e.Type == model.EventTypeStatus && e.Name == string(model.StatusRunning) && e.Attrs[model.AttrStatusSource] == string(model.EventSourceUser) {
			userStatus = true
		}
	}
	if !revivedNote {
		t.Fatal("session_revived note (generation 2) not recorded")
	}
	if !userStatus {
		t.Fatal("terminal re-entry must be a user-sourced running status event")
	}
}

func TestReviveCodexReResolvesIdentityPostBoot(t *testing.T) {
	st, run := createReapedTestRun(t, "codex", true)
	mux := &mockReviveMux{}
	withMockReviveMux(t, mux)
	prevResolve := reviveResolveCodexSessionID
	reviveResolveCodexSessionID = func(sessionsHome, workDir string, timeout time.Duration) (string, error) {
		return "conv-8888", nil
	}
	t.Cleanup(func() { reviveResolveCodexSessionID = prevResolve })
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))

	fresh, err := server.reviveIfReaped(st, "project", t.TempDir(), run)
	if err != nil {
		t.Fatalf("reviveIfReaped() error = %v", err)
	}
	if len(mux.booted) != 1 {
		t.Fatalf("expected exactly one session boot, got %d", len(mux.booted))
	}
	if !strings.Contains(mux.booted[0].Command, "resume conv-8888") {
		t.Fatalf("codex revive must use the resume subcommand with the recorded id, got %q", mux.booted[0].Command)
	}
	if fresh.AgentSessionGeneration != 2 || fresh.SessionReaped() {
		t.Fatalf("codex revive must record generation 2 and dissolve the latch (gen=%d reaped=%v)", fresh.AgentSessionGeneration, fresh.SessionReaped())
	}
}

func TestReviveWithoutRecordedIdentityFailsNamingTheFact(t *testing.T) {
	st, run := createReapedTestRun(t, "claude", false)
	mux := &mockReviveMux{}
	withMockReviveMux(t, mux)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))

	_, err := server.reviveIfReaped(st, "project", t.TempDir(), run)
	if err == nil {
		t.Fatal("revive without recorded identity must fail")
	}
	if !strings.Contains(err.Error(), "agent_session identity is not recorded") || !strings.Contains(err.Error(), "restart-from --branch") {
		t.Fatalf("error must name the missing fact and point at restart-from --branch, got %v", err)
	}
	if len(mux.booted) != 0 {
		t.Fatal("no session may be booted when a stored-fact precondition is missing")
	}
}

func TestReviveWorktreeRemovedNoteFailsFast(t *testing.T) {
	st, run := createReapedTestRun(t, "claude", true)
	if err := st.AppendEvent(run.Ref(), model.NewDaemonNoticeEvent("worktree_removed", map[string]string{"path": run.WorktreePath})); err != nil {
		t.Fatalf("AppendEvent(worktree_removed) error = %v", err)
	}
	run, err := st.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	mux := &mockReviveMux{}
	withMockReviveMux(t, mux)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))

	_, err = server.reviveIfReaped(st, "project", t.TempDir(), run)
	if err == nil || !strings.Contains(err.Error(), "worktree_removed") {
		t.Fatalf("revive after worktree_removed must fail naming the fact, got %v", err)
	}
	if len(mux.booted) != 0 {
		t.Fatal("no session may be booted after worktree_removed")
	}
}

func TestRevivePendingReapKillRetryFailsClearly(t *testing.T) {
	st, run := createReapedTestRun(t, "claude", true)
	mux := &mockReviveMux{hasSession: true}
	withMockReviveMux(t, mux)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))

	_, err := server.reviveIfReaped(st, "project", t.TempDir(), run)
	if err == nil || !strings.Contains(err.Error(), "pending reap-kill retry") {
		t.Fatalf("revive against a still-alive reaped session must fail clearly, got %v", err)
	}
	if len(mux.booted) != 0 {
		t.Fatal("no session may be booted over a pending reap-kill")
	}
}

func TestReviveNonReapedRunIsUntouched(t *testing.T) {
	st, run := createReapedTestRun(t, "claude", true)
	// Dissolve the latch first: a generation-2 identity means not reaped.
	if err := st.AppendEvent(run.Ref(), model.NewArtifactEvent("agent_session", map[string]string{"backend": "claude", "id": "conv-8888", "generation": "2"})); err != nil {
		t.Fatalf("AppendEvent(agent_session) error = %v", err)
	}
	run, err := st.GetRun(run.Ref())
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	mux := &mockReviveMux{}
	withMockReviveMux(t, mux)
	server := NewSocketServer(func(string) (store.Store, error) { return st, nil }, log.New(io.Discard, "", 0))

	fresh, err := server.reviveIfReaped(st, "project", t.TempDir(), run)
	if err != nil {
		t.Fatalf("reviveIfReaped() on non-reaped run error = %v", err)
	}
	if fresh != run {
		t.Fatal("non-reaped run must pass through untouched")
	}
	if len(mux.booted) != 0 {
		t.Fatal("non-reaped run must not boot anything")
	}
}

// TestStepTerminalAbsorbsAllObservationsExceptReviveMilestone pins the L4'
// refinement: terminal views absorb every observation EXCEPT the
// user-sourced revive milestone, which alone re-enters running.
func TestStepTerminalAbsorbsAllObservationsExceptReviveMilestone(t *testing.T) {
	view := runView{IssueID: "i", RunID: "r", Status: model.StatusDone}

	observations := []runObservation{
		{Kind: obsSessionAlive},
		{Kind: obsSessionGone},
		{Kind: obsCaptured},
		{Kind: obsPRState},
		{Kind: obsGitEvidence},
		{Kind: obsLaunchProgress, Launch: launchReached(stageRunCreated)},
		{Kind: obsLaunchProgress, Launch: launchReached(stageLaunchReady)},
		{Kind: obsLaunchProgress, Launch: launchReached(stageAgentStarted)},
		{Kind: obsLaunchProgress, Launch: launchFailed("session", fmt.Errorf("boom"))},
	}
	for _, obs := range observations {
		if _, effects := stepRun(view, runCore{}, obs, time.Now()); len(effects) != 0 {
			t.Fatalf("terminal view must absorb %+v, got effects %+v", obs, effects)
		}
	}

	_, effects := stepRun(view, runCore{}, runObservation{Kind: obsLaunchProgress, Launch: launchReached(stageSessionRevived)}, time.Now())
	if len(effects) != 1 || effects[0].Kind != effectSetStatus || effects[0].Status != model.StatusRunning || effects[0].Source != model.EventSourceUser {
		t.Fatalf("revive milestone on terminal view must yield exactly one user-sourced running commit, got %+v", effects)
	}
}
