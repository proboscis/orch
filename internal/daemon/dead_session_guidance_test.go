package daemon

import (
	"fmt"
	"strings"
	"testing"

	orchpb "github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/multiplexer"
)

// deadUnlatchedRun builds the issue state: the session is gone (callers fake
// the mux), the run is NOT SessionReaped() (the event fold has no
// session_reaped note), and an agent_session identity IS recorded.
func deadUnlatchedRun(t *testing.T, issueID, runID string) *model.Run {
	t.Helper()
	run := &model.Run{
		IssueID:     model.IssueID(issueID),
		RunID:       model.RunID(runID),
		Agent:       string(agent.AgentClaude),
		Multiplexer: string(multiplexer.TypeTmux),
		SessionName: model.GenerateSessionName(model.IssueID(issueID), model.RunID(runID)),
		Events: []*model.Event{
			model.NewStatusEvent(model.StatusWaiting),
			model.NewArtifactEvent("agent_session", map[string]string{
				"id":         "0f1e2d3c-claude-session",
				"generation": "1",
				"agent":      string(agent.AgentClaude),
			}),
		},
	}
	if err := run.DeriveState(); err != nil {
		t.Fatalf("DeriveState() error = %v", err)
	}
	if run.SessionReaped() {
		t.Fatal("test premise broken: run must NOT be SessionReaped()")
	}
	if run.AgentSessionID == "" {
		t.Fatal("test premise broken: run must have a recorded agent_session identity")
	}
	return run
}

// assertDeadUnlatchedGuidance asserts the three required elements of the
// dead-but-unlatched message: (i) the daemon did not reap the session (the
// L-S3 latch is unset), (ii) why auto-revive does not apply, and (iii) the
// escape path `orch restart-from <ref>`.
func assertDeadUnlatchedGuidance(t *testing.T, msg, ref string) {
	t.Helper()
	checks := []string{
		"not reaped by the daemon",
		"L-S3 latch is unset",
		"auto-revive",
		"does not apply",
		fmt.Sprintf("orch restart-from %s", ref),
		"--branch",
	}
	for _, check := range checks {
		if !strings.Contains(msg, check) {
			t.Errorf("message missing %q:\n%s", check, msg)
		}
	}
}

func TestDeadUnlatchedSessionGuidanceClassification(t *testing.T) {
	t.Run("nil run yields no guidance", func(t *testing.T) {
		if got := deadUnlatchedSessionGuidance(nil); got != "" {
			t.Fatalf("guidance = %q, want empty", got)
		}
	})

	t.Run("no recorded identity yields no guidance", func(t *testing.T) {
		run := &model.Run{
			IssueID: "i", RunID: "r",
			Events: []*model.Event{model.NewStatusEvent(model.StatusWaiting)},
		}
		if got := deadUnlatchedSessionGuidance(run); got != "" {
			t.Fatalf("guidance = %q, want empty (revive preconditions own this wording)", got)
		}
	})

	t.Run("reaped run yields no guidance", func(t *testing.T) {
		run := deadUnlatchedRun(t, "issue-reaped", "run-reaped")
		run.Events = append(run.Events, model.NewDaemonNoticeEvent("session_reaped", map[string]string{
			"generation": "1",
		}))
		if !run.SessionReaped() {
			t.Fatal("expected run to be SessionReaped()")
		}
		if got := deadUnlatchedSessionGuidance(run); got != "" {
			t.Fatalf("guidance = %q, want empty (the revive path owns reaped sessions)", got)
		}
	})

	t.Run("dead unlatched run yields full guidance", func(t *testing.T) {
		run := deadUnlatchedRun(t, "issue-dead", "run-dead")
		got := deadUnlatchedSessionGuidance(run)
		if got == "" {
			t.Fatal("guidance empty, want dead-unlatched explanation")
		}
		assertDeadUnlatchedGuidance(t, got, "issue-dead#run-dead")
	})
}

func TestDecorateSessionNotFoundKeepsUnrelatedErrors(t *testing.T) {
	run := deadUnlatchedRun(t, "issue-dead", "run-dead")
	err := fmt.Errorf("worker lease timed out")
	if got := decorateSessionNotFound(err, run); got != err {
		t.Fatalf("decorated unrelated error: %v", got)
	}
}

// The send entry verb: a dead-but-unlatched session must fail with the full
// explanation, not the bare "session not found (run may not be active)".
func TestSendMessageDeadUnlatchedSessionExplainsEscapePath(t *testing.T) {
	run := deadUnlatchedRun(t, "issue-dead", "run-dead")
	st := &mockStore{
		runs:   map[string]*model.Run{"issue-dead#run-dead": run},
		issues: map[string]*model.Issue{},
	}
	server := newTestServer(t, st)

	mockMux := &mockSendMux{hasSession: false, muxType: multiplexer.TypeTmux}
	prevForType := getSendMultiplexerForType
	getSendMultiplexerForType = func(multiplexer.Type) sendMultiplexer { return mockMux }
	defer func() { getSendMultiplexerForType = prevForType }()

	resp := server.handleProtoSendMessage(&orchpb.SendMessageRequest{
		IssueId: "issue-dead",
		RunId:   "run-dead",
		Message: "hello?",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})

	if resp.Ok {
		t.Fatal("handleProtoSendMessage() Ok = true, want false")
	}
	if !strings.Contains(resp.Error, "not found (run may not be active)") {
		t.Fatalf("error lost the session-not-found verdict: %s", resp.Error)
	}
	assertDeadUnlatchedGuidance(t, resp.Error, "issue-dead#run-dead")
}

// A session-not-found on a run WITHOUT a recorded identity keeps the plain
// error: this change decorates only the dead-but-unlatched classification.
func TestSendMessageSessionNotFoundWithoutIdentityStaysPlain(t *testing.T) {
	run := &model.Run{
		IssueID:     "issue-plain",
		RunID:       "run-plain",
		Agent:       string(agent.AgentClaude),
		Multiplexer: string(multiplexer.TypeTmux),
		Status:      model.StatusWaiting,
	}
	st := &mockStore{
		runs:   map[string]*model.Run{"issue-plain#run-plain": run},
		issues: map[string]*model.Issue{},
	}
	server := newTestServer(t, st)

	mockMux := &mockSendMux{hasSession: false, muxType: multiplexer.TypeTmux}
	prevForType := getSendMultiplexerForType
	getSendMultiplexerForType = func(multiplexer.Type) sendMultiplexer { return mockMux }
	defer func() { getSendMultiplexerForType = prevForType }()

	resp := server.handleProtoSendMessage(&orchpb.SendMessageRequest{
		IssueId: "issue-plain",
		RunId:   "run-plain",
		Message: "hello?",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})

	if resp.Ok {
		t.Fatal("handleProtoSendMessage() Ok = true, want false")
	}
	if !strings.Contains(resp.Error, "not found (run may not be active)") {
		t.Fatalf("error = %q, want session-not-found verdict", resp.Error)
	}
	if strings.Contains(resp.Error, "restart-from") {
		t.Fatalf("error = %q, must not carry dead-unlatched guidance without a recorded identity", resp.Error)
	}
}

// The attach entry verb: the same dead-but-unlatched state must carry the
// same explanation next to the session_not_found verdict.
func TestGetAttachInfoDeadUnlatchedSessionExplainsEscapePath(t *testing.T) {
	run := deadUnlatchedRun(t, "issue-dead", "run-dead")
	st := &mockStore{
		runs:   map[string]*model.Run{"issue-dead#run-dead": run},
		issues: map[string]*model.Issue{},
	}
	server := newTestServer(t, st)

	prevChecker := getAttachSessionChecker
	getAttachSessionChecker = func(multiplexer.Type) (attachSessionChecker, error) {
		return &mockSendMux{hasSession: false, muxType: multiplexer.TypeTmux}, nil
	}
	defer func() { getAttachSessionChecker = prevChecker }()

	resp := server.handleProtoGetAttachInfo(&orchpb.GetAttachInfoRequest{
		IssueId: "issue-dead",
		RunId:   "run-dead",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})

	if resp.Ok {
		t.Fatal("handleProtoGetAttachInfo() Ok = true, want false")
	}
	if resp.Error != "session_not_found" {
		t.Fatalf("error = %q, want session_not_found", resp.Error)
	}
	attachResp := resp.GetGetAttachInfo()
	if attachResp == nil {
		t.Fatal("expected GetAttachInfo response payload")
	}
	if attachResp.SessionGoneGuidance == "" {
		t.Fatal("SessionGoneGuidance empty, want dead-unlatched explanation")
	}
	assertDeadUnlatchedGuidance(t, attachResp.SessionGoneGuidance, "issue-dead#run-dead")
}

// Attach on a session-not-found WITHOUT a recorded identity keeps the
// guidance field empty (the CLI then shows only the existing message).
func TestGetAttachInfoSessionNotFoundWithoutIdentityHasNoGuidance(t *testing.T) {
	run := &model.Run{
		IssueID:     "issue-plain",
		RunID:       "run-plain",
		Agent:       string(agent.AgentClaude),
		Multiplexer: string(multiplexer.TypeTmux),
		Status:      model.StatusWaiting,
	}
	st := &mockStore{
		runs:   map[string]*model.Run{"issue-plain#run-plain": run},
		issues: map[string]*model.Issue{},
	}
	server := newTestServer(t, st)

	prevChecker := getAttachSessionChecker
	getAttachSessionChecker = func(multiplexer.Type) (attachSessionChecker, error) {
		return &mockSendMux{hasSession: false, muxType: multiplexer.TypeTmux}, nil
	}
	defer func() { getAttachSessionChecker = prevChecker }()

	resp := server.handleProtoGetAttachInfo(&orchpb.GetAttachInfoRequest{
		IssueId: "issue-plain",
		RunId:   "run-plain",
		Context: &orchpb.RequestContext{ProjectId: testProjectID},
	})

	if resp.Ok {
		t.Fatal("handleProtoGetAttachInfo() Ok = true, want false")
	}
	if resp.Error != "session_not_found" {
		t.Fatalf("error = %q, want session_not_found", resp.Error)
	}
	attachResp := resp.GetGetAttachInfo()
	if attachResp == nil {
		t.Fatal("expected GetAttachInfo response payload")
	}
	if attachResp.SessionGoneGuidance != "" {
		t.Fatalf("SessionGoneGuidance = %q, want empty without a recorded identity", attachResp.SessionGoneGuidance)
	}
}
