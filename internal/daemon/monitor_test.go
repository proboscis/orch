package daemon

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/pr"
	"github.com/s22625/orch/internal/store"
)

func newTestDaemon() *Daemon {
	return &Daemon{
		logger:        log.New(io.Discard, "", 0),
		runStates:     make(map[string]*RunState),
		lastFetchAt:   make(map[string]time.Time),
		fetchInFlight: make(map[string]bool),
	}
}

func TestHashContentIgnoresStatusBar(t *testing.T) {
	outputA := strings.Join([]string{"line1", "line2", "line3", "line4", "line5", "line6"}, "\n")
	outputB := strings.Join([]string{"line1", "line2", "line3", "line4", "line5", "changed"}, "\n")

	if hashContent(outputA) != hashContent(outputB) {
		t.Fatal("expected hash to ignore last 5 lines")
	}
}

func TestRunStateRecordPromptSignalDebounce(t *testing.T) {
	state := &RunState{}

	if state.recordPromptSignal(true) {
		t.Fatal("expected first prompt observation to be unstable")
	}
	if state.PromptStreak != 1 {
		t.Fatalf("prompt streak after first prompt = %d, want 1", state.PromptStreak)
	}

	if !state.recordPromptSignal(true) {
		t.Fatal("expected second consecutive prompt observation to become stable")
	}
	if state.PromptStreak != 2 {
		t.Fatalf("prompt streak after second prompt = %d, want 2", state.PromptStreak)
	}

	if state.recordPromptSignal(false) {
		t.Fatal("expected non-prompt observation to clear stable state")
	}
	if state.PromptStreak != 0 {
		t.Fatalf("prompt streak after reset = %d, want 0", state.PromptStreak)
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

// mockStoreForUpdate is a mock store for testing updateStatus
type mockStoreForUpdate struct {
	issue               *model.Issue
	resolveIssueErr     error
	setIssueStatusErr   error
	appendEventErr      error
	setIssueStatusCalls []struct {
		issueID string
		status  model.IssueStatus
	}
	appendEventCalls int
	lastAppendedEvent *model.Event
}

func (m *mockStoreForUpdate) ResolveIssue(issueID model.IssueID) (*model.Issue, error) {
	if m.resolveIssueErr != nil {
		return nil, m.resolveIssueErr
	}
	return m.issue, nil
}

func (m *mockStoreForUpdate) SetIssueStatus(issueID model.IssueID, status model.IssueStatus) error {
	m.setIssueStatusCalls = append(m.setIssueStatusCalls, struct {
		issueID string
		status  model.IssueStatus
	}{string(issueID), status})
	return m.setIssueStatusErr
}

func (m *mockStoreForUpdate) AppendEvent(ref *model.RunRef, event *model.Event) error {
	m.appendEventCalls++
	m.lastAppendedEvent = event
	return m.appendEventErr
}

func (m *mockStoreForUpdate) ListIssues() ([]*model.Issue, error) { return nil, nil }
func (m *mockStoreForUpdate) CreateRun(model.IssueID, model.RunID, map[string]string) (*model.Run, error) {
	return nil, nil
}
func (m *mockStoreForUpdate) CreateRunForExistingIssue(model.IssueID, model.RunID, map[string]string) (*model.Run, error) {
	return nil, nil
}
func (m *mockStoreForUpdate) ListRuns(*store.ListRunsFilter) ([]*model.Run, error) { return nil, nil }
func (m *mockStoreForUpdate) GetRun(*model.RunRef) (*model.Run, error)             { return nil, nil }
func (m *mockStoreForUpdate) GetRunByShortID(model.ShortID) (*model.Run, error)    { return nil, nil }
func (m *mockStoreForUpdate) GetLatestRun(model.IssueID) (*model.Run, error)       { return nil, nil }
func (m *mockStoreForUpdate) RootPath() string                                     { return "" }
func (m *mockStoreForUpdate) DeleteRun(ref *model.RunRef) error                    { return nil }
func (m *mockStoreForUpdate) UpdateIssue(issue *model.Issue) error                 { return nil }
func (m *mockStoreForUpdate) ValidateIssueFiles(issueID model.IssueID) (*store.ValidationResult, error) {
	return nil, nil
}
func (m *mockStoreForUpdate) WriteAgentPrompt(ref *model.RunRef, content string) error { return nil }
func (m *mockStoreForUpdate) ReadAgentPrompt(ref *model.RunRef) (string, error)        { return "", nil }
func (m *mockStoreForUpdate) CreateIssue(issue *model.Issue) error                     { return nil }

// mockStoreWithRun returns a fixed run from GetRun so updateStatus can see
// the current persisted status.
type mockStoreWithRun struct {
	mockStoreForUpdate
	run *model.Run
}

func (m *mockStoreWithRun) GetRun(*model.RunRef) (*model.Run, error) { return m.run, nil }

func TestUpdateStatusSameStatusIsNoOp(t *testing.T) {
	d := newTestDaemon()
	run := &model.Run{IssueID: "i1", RunID: "r1", Status: model.StatusUnknown}
	st := &mockStoreWithRun{
		mockStoreForUpdate: mockStoreForUpdate{issue: &model.Issue{ID: "i1", Status: model.IssueStatusOpen}},
		run:                run,
	}

	if err := d.updateStatus(run, model.StatusUnknown, "", st); err != nil {
		t.Fatalf("updateStatus() error = %v", err)
	}
	if st.appendEventCalls != 0 {
		t.Fatalf("re-affirming the current status must not append events, got %d", st.appendEventCalls)
	}

	if err := d.updateStatus(run, model.StatusPROpen, "", st); err != nil {
		t.Fatalf("updateStatus() error = %v", err)
	}
	if st.appendEventCalls != 1 {
		t.Fatalf("expected a real transition to append one event, got %d", st.appendEventCalls)
	}
}

// updateStatus persists the verdict reason as the status event's
// model.AttrStatusReason attribute (run-state-machine.md §5 "Status reasons").
func TestUpdateStatusPersistsReason(t *testing.T) {
	d := newTestDaemon()
	run := &model.Run{IssueID: "i1", RunID: "r1", Status: model.StatusRunning}
	st := &mockStoreWithRun{
		mockStoreForUpdate: mockStoreForUpdate{issue: &model.Issue{ID: "i1", Status: model.IssueStatusOpen}},
		run:                run,
	}

	if err := d.updateStatus(run, model.StatusUnknown, model.StatusReasonNeverAlive, st); err != nil {
		t.Fatalf("updateStatus() error = %v", err)
	}
	ev := st.lastAppendedEvent
	if ev == nil || ev.Type != model.EventTypeStatus {
		t.Fatalf("expected a status event append, got %+v", ev)
	}
	if got := ev.Attrs[model.AttrStatusReason]; got != model.StatusReasonNeverAlive {
		t.Fatalf("status event reason = %q, want %q", got, model.StatusReasonNeverAlive)
	}

	// An empty reason must not introduce an attrs map entry.
	run2 := &model.Run{IssueID: "i1", RunID: "r2", Status: model.StatusRunning}
	st.run = run2
	if err := d.updateStatus(run2, model.StatusWaiting, "", st); err != nil {
		t.Fatalf("updateStatus() error = %v", err)
	}
	if _, ok := st.lastAppendedEvent.Attrs[model.AttrStatusReason]; ok {
		t.Fatalf("empty reason must not write a reason attribute, got %+v", st.lastAppendedEvent.Attrs)
	}
}

func TestNoteRunFeedbackResetsPromptDebounce(t *testing.T) {
	d := newTestDaemon()
	run := &model.Run{IssueID: "i1", RunID: "r1"}

	state := d.getOrCreateState(run)
	state.PromptStreak = 5 // idle at prompt for several captures

	d.noteRunFeedback(run)

	if state.PromptStreak != 0 {
		t.Fatalf("expected prompt streak reset after feedback, got %d", state.PromptStreak)
	}
	// A lingering prompt on the very next capture must not be "stable" yet.
	if state.recordPromptSignal(true) {
		t.Fatal("single post-feedback prompt capture must not count as a stable prompt")
	}
}

func TestRunLivenessFromMonitorState(t *testing.T) {
	d := newTestDaemon()
	run := &model.Run{IssueID: "i1", RunID: "r1"}

	if alive, known := d.runLiveness(run); alive || known {
		t.Fatalf("unobserved run must be unknown, got alive=%v known=%v", alive, known)
	}

	state := d.getOrCreateState(run)
	if alive, known := d.runLiveness(run); alive || known {
		t.Fatalf("run with no observations yet must stay unknown, got alive=%v known=%v", alive, known)
	}

	state.WasAlive = true
	if alive, known := d.runLiveness(run); !alive || !known {
		t.Fatalf("observed-alive run must report alive, got alive=%v known=%v", alive, known)
	}

	state.DeadCheckCount = 1
	if alive, known := d.runLiveness(run); alive || !known {
		t.Fatalf("run failing dead checks must report not alive, got alive=%v known=%v", alive, known)
	}
}

func TestUpdateStatusAutoResolve(t *testing.T) {
	tests := []struct {
		name              string
		status            model.Status
		issueStatus       model.IssueStatus
		wantSetStatusCall bool
	}{
		{
			name:              "StatusDone resolves open issue",
			status:            model.StatusDone,
			issueStatus:       model.IssueStatusOpen,
			wantSetStatusCall: true,
		},
		{
			name:              "StatusDone skips already resolved issue",
			status:            model.StatusDone,
			issueStatus:       model.IssueStatusResolved,
			wantSetStatusCall: false,
		},
		{
			name:              "StatusDone skips closed issue",
			status:            model.StatusDone,
			issueStatus:       model.IssueStatusClosed,
			wantSetStatusCall: false,
		},
		{
			name:              "non-done status does not resolve",
			status:            model.StatusRunning,
			issueStatus:       model.IssueStatusOpen,
			wantSetStatusCall: false,
		},
		{
			name:              "StatusWaiting does not resolve",
			status:            model.StatusWaiting,
			issueStatus:       model.IssueStatusOpen,
			wantSetStatusCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestDaemon()
			st := &mockStoreForUpdate{
				issue: &model.Issue{ID: "test-issue", Status: tt.issueStatus},
			}
			run := &model.Run{IssueID: "test-issue", RunID: "run-1"}

			err := d.updateStatus(run, tt.status, "", st)
			if err != nil {
				t.Fatalf("updateStatus() error = %v", err)
			}

			gotCalls := len(st.setIssueStatusCalls)
			if tt.wantSetStatusCall && gotCalls != 1 {
				t.Errorf("expected SetIssueStatus to be called once, got %d calls", gotCalls)
			}
			if !tt.wantSetStatusCall && gotCalls != 0 {
				t.Errorf("expected SetIssueStatus not to be called, got %d calls", gotCalls)
			}
			if tt.wantSetStatusCall && gotCalls == 1 {
				if st.setIssueStatusCalls[0].status != model.IssueStatusResolved {
					t.Errorf("expected status %v, got %v", model.IssueStatusResolved, st.setIssueStatusCalls[0].status)
				}
			}
		})
	}
}

func TestUpdateStatusSetIssueStatusErrorSwallowed(t *testing.T) {
	d := newTestDaemon()
	st := &mockStoreForUpdate{
		issue:             &model.Issue{ID: "test-issue", Status: model.IssueStatusOpen},
		setIssueStatusErr: io.ErrUnexpectedEOF, // simulate error
	}
	run := &model.Run{IssueID: "test-issue", RunID: "run-1"}

	err := d.updateStatus(run, model.StatusDone, "", st)
	if err != nil {
		t.Fatalf("updateStatus() should not fail when SetIssueStatus fails, got error = %v", err)
	}

	if st.appendEventCalls != 1 {
		t.Errorf("expected AppendEvent to be called once, got %d", st.appendEventCalls)
	}
}

func TestUpdateStatusAppendEventError(t *testing.T) {
	d := newTestDaemon()
	st := &mockStoreForUpdate{
		appendEventErr: io.ErrUnexpectedEOF,
	}
	run := &model.Run{IssueID: "test-issue", RunID: "run-1"}

	err := d.updateStatus(run, model.StatusDone, "", st)
	if err == nil {
		t.Fatal("updateStatus() should fail when AppendEvent fails")
	}

	// SetIssueStatus should not be called if AppendEvent fails
	if len(st.setIssueStatusCalls) != 0 {
		t.Errorf("expected SetIssueStatus not to be called when AppendEvent fails, got %d calls", len(st.setIssueStatusCalls))
	}
}

func TestMonitorRunSkipsCanceledStatus(t *testing.T) {
	d := newTestDaemon()
	st := &mockStoreForUpdate{
		issue: &model.Issue{ID: "test-issue", Status: model.IssueStatusOpen},
	}
	run := &model.Run{
		IssueID: "test-issue",
		RunID:   "run-1",
		Status:  model.StatusCanceled,
		PRUrl:   "https://github.com/org/repo/pull/123",
	}

	err := d.monitorRun(run, st, "", "")
	if err != nil {
		t.Fatalf("monitorRun() error = %v", err)
	}

	if st.appendEventCalls != 0 {
		t.Errorf("expected no AppendEvent calls for canceled run, got %d", st.appendEventCalls)
	}

	state := d.runStates[run.Ref().String()]
	if state != nil {
		t.Error("expected no state to be created for canceled run")
	}
}

func TestCheckPROutcomeReturnsClosedWithURL(t *testing.T) {
	d := newTestDaemon()
	d.lookupPRInfoByURLFn = func(prURL string) (*pr.Info, error) {
		return &pr.Info{URL: prURL, Number: 123, State: "CLOSED"}, nil
	}
	run := &model.Run{
		IssueID: "test-issue",
		RunID:   "run-1",
		PRUrl:   "https://github.com/org/repo/pull/123",
	}

	outcome, url := d.checkPROutcome(run, nil)

	if outcome != prOutcomeClosed {
		t.Fatalf("outcome = %s, want %s", outcome, prOutcomeClosed)
	}
	if url != run.PRUrl {
		t.Fatalf("url = %q, want %q", url, run.PRUrl)
	}
}

func TestMonitorRunClosedPRTransitionsToCanceledAndStopsPolling(t *testing.T) {
	d := newTestDaemon()
	prURL := "https://github.com/org/repo/pull/123"
	lookups := 0
	d.lookupPRInfoByURLFn = func(gotURL string) (*pr.Info, error) {
		lookups++
		if gotURL != prURL {
			t.Fatalf("lookup URL = %q, want %q", gotURL, prURL)
		}
		return &pr.Info{URL: gotURL, Number: 123, State: "CLOSED"}, nil
	}

	run := &model.Run{
		IssueID:   "test-issue",
		RunID:     "run-1",
		Status:    model.StatusPROpen,
		PRUrl:     prURL,
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	st := &mockStoreRecordingEvents{
		mockStoreForUpdate: mockStoreForUpdate{issue: &model.Issue{ID: "test-issue", Status: model.IssueStatusOpen}},
		run:                run,
	}

	if err := d.monitorRun(run, st, "", ""); err != nil {
		t.Fatalf("monitorRun() error = %v", err)
	}

	if run.Status != model.StatusCanceled {
		t.Fatalf("run.Status = %s, want %s", run.Status, model.StatusCanceled)
	}
	if lookups != 1 {
		t.Fatalf("expected one PR lookup, got %d", lookups)
	}
	if len(st.events) != 2 {
		t.Fatalf("expected pr_closed artifact + canceled status events, got %d", len(st.events))
	}
	if st.events[0].Type != model.EventTypeArtifact || st.events[0].Name != "pr_closed" {
		t.Fatalf("first event = %s/%s, want artifact/pr_closed", st.events[0].Type, st.events[0].Name)
	}
	if st.events[0].Attrs["url"] != prURL {
		t.Fatalf("pr_closed url = %q, want %q", st.events[0].Attrs["url"], prURL)
	}
	if st.events[1].Type != model.EventTypeStatus || st.events[1].Name != string(model.StatusCanceled) {
		t.Fatalf("second event = %s/%s, want status/%s", st.events[1].Type, st.events[1].Name, model.StatusCanceled)
	}

	eventsAfterFirstCycle := len(st.events)
	updatedAfterFirstCycle := run.UpdatedAt
	if err := d.monitorRun(run, st, "", ""); err != nil {
		t.Fatalf("monitorRun() second cycle error = %v", err)
	}
	if len(st.events) != eventsAfterFirstCycle {
		t.Fatalf("second cycle wrote %d new events", len(st.events)-eventsAfterFirstCycle)
	}
	if !run.UpdatedAt.Equal(updatedAfterFirstCycle) {
		t.Fatalf("UpdatedAt moved on second cycle: got %s, want %s", run.UpdatedAt, updatedAfterFirstCycle)
	}
	if lookups != 1 {
		t.Fatalf("terminal run should not be polled again, got %d lookups", lookups)
	}
}

func TestMonitorRunMergedPRTransitionsToDone(t *testing.T) {
	d := newTestDaemon()
	prURL := "https://github.com/org/repo/pull/124"
	d.lookupPRInfoByURLFn = func(gotURL string) (*pr.Info, error) {
		if gotURL != prURL {
			t.Fatalf("lookup URL = %q, want %q", gotURL, prURL)
		}
		return &pr.Info{URL: gotURL, Number: 124, State: "MERGED"}, nil
	}

	run := &model.Run{
		IssueID: "test-issue",
		RunID:   "run-1",
		Status:  model.StatusPROpen,
		PRUrl:   prURL,
	}
	st := &mockStoreRecordingEvents{
		mockStoreForUpdate: mockStoreForUpdate{issue: &model.Issue{ID: "test-issue", Status: model.IssueStatusOpen}},
		run:                run,
	}

	if err := d.monitorRun(run, st, "", ""); err != nil {
		t.Fatalf("monitorRun() error = %v", err)
	}

	if run.Status != model.StatusDone {
		t.Fatalf("run.Status = %s, want %s", run.Status, model.StatusDone)
	}
	if len(st.events) != 1 {
		t.Fatalf("expected done status event only, got %d events", len(st.events))
	}
	if st.events[0].Type != model.EventTypeStatus || st.events[0].Name != string(model.StatusDone) {
		t.Fatalf("event = %s/%s, want status/%s", st.events[0].Type, st.events[0].Name, model.StatusDone)
	}
}

func TestInferStatusFromGitStateMapsPROutcomes(t *testing.T) {
	tests := []struct {
		name      string
		prState   string
		wantState model.Status
	}{
		{name: "open", prState: "OPEN", wantState: model.StatusPROpen},
		{name: "merged", prState: "MERGED", wantState: model.StatusDone},
		{name: "closed", prState: "CLOSED", wantState: model.StatusCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestDaemon()
			d.lookupPRInfoFn = func(repoRoot, branch string) (*pr.Info, error) {
				if branch != "feature/test" {
					t.Fatalf("branch = %q, want feature/test", branch)
				}
				return &pr.Info{URL: "https://github.com/org/repo/pull/125", Number: 125, State: tt.prState}, nil
			}
			run := &model.Run{
				IssueID:      "test-issue",
				RunID:        "run-1",
				Branch:       "feature/test",
				WorktreePath: ".",
			}
			st := &mockStoreRecordingEvents{
				mockStoreForUpdate: mockStoreForUpdate{issue: &model.Issue{ID: "test-issue", Status: model.IssueStatusOpen}},
				run:                run,
			}

			got := d.inferStatusFromGitState(run, st, true, "")
			if got != tt.wantState {
				t.Fatalf("inferStatusFromGitState() = %s, want %s", got, tt.wantState)
			}
		})
	}
}

func TestCaptureFailureBackoffConnectionRefused(t *testing.T) {
	state := &RunState{}
	endpoint := "opencode:4097"
	now := time.Now()

	if state.shouldSkipCapture(endpoint, now) {
		t.Fatal("expected first capture attempt not to be skipped")
	}

	err := errors.New("dial tcp 127.0.0.1:4097: connect: connection refused")
	retryAt, shouldLog, suppressed := state.recordCaptureFailure(endpoint, err, now)

	if !shouldLog {
		t.Fatal("expected first failure to be logged")
	}
	if suppressed != 0 {
		t.Fatalf("expected no suppressed logs on first failure, got %d", suppressed)
	}
	if got := retryAt.Sub(now); got < captureRefusedNegativeCacheTTL {
		t.Fatalf("expected retry delay >= %s, got %s", captureRefusedNegativeCacheTTL, got)
	}

	if !state.shouldSkipCapture(endpoint, now.Add(5*time.Second)) {
		t.Fatal("expected capture attempt to be throttled during backoff")
	}
	if state.shouldSkipCapture(endpoint, retryAt.Add(time.Millisecond)) {
		t.Fatal("expected capture attempt to resume after backoff")
	}
}

func TestCaptureFailureLogDeduplication(t *testing.T) {
	state := &RunState{}
	endpoint := "opencode:4098"
	err := errors.New("dial tcp 127.0.0.1:4098: connect: connection refused")
	now := time.Now()

	_, shouldLog, _ := state.recordCaptureFailure(endpoint, err, now)
	if !shouldLog {
		t.Fatal("expected first failure to be logged")
	}

	_, shouldLog, _ = state.recordCaptureFailure(endpoint, err, now.Add(5*time.Second))
	if shouldLog {
		t.Fatal("expected duplicate failure log to be suppressed inside log interval")
	}

	_, shouldLog, suppressed := state.recordCaptureFailure(endpoint, err, now.Add(captureErrorLogInterval+time.Second))
	if !shouldLog {
		t.Fatal("expected failure log after interval")
	}
	if suppressed != 1 {
		t.Fatalf("expected one suppressed log to be reported, got %d", suppressed)
	}
}

func TestCaptureFailureEndpointChangeResetsBackoff(t *testing.T) {
	state := &RunState{}
	now := time.Now()

	_, _, _ = state.recordCaptureFailure("opencode:4097", errors.New("dial tcp 127.0.0.1:4097: connect: connection refused"), now)

	if !state.shouldSkipCapture("opencode:4097", now.Add(2*time.Second)) {
		t.Fatal("expected original endpoint to remain throttled")
	}
	if state.shouldSkipCapture("opencode:4099", now.Add(2*time.Second)) {
		t.Fatal("expected new endpoint to bypass old endpoint backoff")
	}
}

func TestMonitorRunOpenCodeCaptureSuccessResetsFailureState(t *testing.T) {
	worktreePath := "/tmp/orch-opencode-live"
	sessionID := "ses_live"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/global/health":
			_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
		case "/project/current":
			_, _ = io.WriteString(w, `{"id":"proj_test","worktree":"`+worktreePath+`"}`)
		case "/session/" + sessionID + "/message":
			_, _ = io.WriteString(w, `[{"info":{"id":"msg_1","sessionID":"`+sessionID+`","role":"assistant","createdAt":"2026-02-09T00:00:00Z"},"parts":[{"type":"text","text":"capture alive"}]}]`)
		case "/session/status":
			_, _ = io.WriteString(w, `{"`+sessionID+`":"busy"}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	port := testPortFromURL(t, server.URL)

	d := newTestDaemon()
	run := &model.Run{
		IssueID:           "orch-427",
		RunID:             "run-live",
		Agent:             "opencode",
		Status:            model.StatusRunning,
		WorktreePath:      worktreePath,
		ServerPort:        port,
		OpenCodeSessionID: sessionID,
	}
	state := d.getOrCreateState(run)
	state.CaptureEndpoint = "opencode:" + strconv.Itoa(port)
	state.CaptureFailureCount = 3
	state.CaptureRetryAt = time.Now().Add(-time.Second)
	state.CaptureErrorKey = "opencode:old|connection-refused"
	state.CaptureErrorLogAt = time.Now().Add(-2 * time.Minute)
	state.SuppressedCaptureLogs = 5

	st := &mockStoreForUpdate{issue: &model.Issue{ID: "orch-427", Status: model.IssueStatusOpen}}
	if err := d.monitorRun(run, st, "", ""); err != nil {
		t.Fatalf("monitorRun() error = %v", err)
	}

	if state.CaptureFailureCount != 0 {
		t.Fatalf("expected capture failure count reset, got %d", state.CaptureFailureCount)
	}
	if !state.CaptureRetryAt.IsZero() {
		t.Fatalf("expected capture retry time reset, got %s", state.CaptureRetryAt)
	}
	if state.CaptureEndpoint != "" {
		t.Fatalf("expected capture endpoint reset, got %q", state.CaptureEndpoint)
	}
	if state.SuppressedCaptureLogs != 0 {
		t.Fatalf("expected suppressed log counter reset, got %d", state.SuppressedCaptureLogs)
	}
	if state.LastOutput == "" {
		t.Fatal("expected successful capture to update last output")
	}
}

func TestMonitorRunOpenCodeSessionAliveDespiteProjectMismatch(t *testing.T) {
	worktreePath := "/tmp/orch-opencode-worktree"
	sessionID := "ses_waiting"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/global/health":
			_, _ = io.WriteString(w, `{"healthy":true,"version":"test"}`)
		case "/project/current":
			_, _ = io.WriteString(w, `{"id":"proj_test","worktree":"/tmp/repo-root"}`)
		case "/session/status":
			_, _ = io.WriteString(w, `{"`+sessionID+`":"idle"}`)
		case "/session/" + sessionID + "/message":
			_, _ = io.WriteString(w, `[{"info":{"id":"msg_wait","sessionID":"`+sessionID+`","role":"assistant","createdAt":"2026-03-12T00:00:00Z"},"parts":[{"type":"text","text":"waiting"}]}]`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	port := testPortFromURL(t, server.URL)

	d := newTestDaemon()
	run := &model.Run{
		IssueID:           "orch-437",
		RunID:             "run-zeus-opencode",
		Agent:             "opencode",
		Status:            model.StatusWaiting,
		WorktreePath:      worktreePath,
		ServerPort:        port,
		OpenCodeSessionID: sessionID,
	}
	state := d.getOrCreateState(run)
	state.WasAlive = false
	state.DeadCheckCount = deadChecksBeforeFailed - 1

	st := &mockStoreForUpdate{issue: &model.Issue{ID: "orch-437", Status: model.IssueStatusOpen}}
	if err := d.monitorRun(run, st, "", ""); err != nil {
		t.Fatalf("monitorRun() error = %v", err)
	}

	if !state.WasAlive {
		t.Fatal("expected run to be marked alive from session status")
	}
	if state.DeadCheckCount != 0 {
		t.Fatalf("expected dead check count reset, got %d", state.DeadCheckCount)
	}
	if st.appendEventCalls != 0 {
		t.Fatalf("expected no failure status event, got %d append calls", st.appendEventCalls)
	}
}

func testPortFromURL(t *testing.T, rawURL string) int {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return port
}

// --- worker-delegated run monitoring (cross-host) ---

type mockStoreRecordingEvents struct {
	mockStoreForUpdate
	run    *model.Run
	events []*model.Event
}

func (m *mockStoreRecordingEvents) GetRun(ref *model.RunRef) (*model.Run, error) {
	if m.run != nil {
		return m.run, nil
	}
	return m.mockStoreForUpdate.GetRun(ref)
}

func (m *mockStoreRecordingEvents) AppendEvent(ref *model.RunRef, event *model.Event) error {
	m.events = append(m.events, event)
	if m.run != nil {
		m.run.Events = append(m.run.Events, event)
		m.run.UpdatedAt = event.Timestamp
		if event.Type == model.EventTypeStatus {
			status, err := model.NormalizeStatus(event.Name)
			if err != nil {
				return err
			}
			m.run.Status = status
		}
	}
	return m.mockStoreForUpdate.AppendEvent(ref, event)
}

type mockStoreForMonitorAll struct {
	mockStoreRecordingEvents
	listRunsFilters []*store.ListRunsFilter
}

func (m *mockStoreForMonitorAll) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	if filter == nil {
		filter = &store.ListRunsFilter{}
	}
	filterCopy := *filter
	filterCopy.Status = append([]model.Status(nil), filter.Status...)
	m.listRunsFilters = append(m.listRunsFilters, &filterCopy)

	if m.run == nil {
		return nil, nil
	}
	if len(filter.Status) > 0 && !statusListContains(filter.Status, m.run.Status) {
		return nil, nil
	}
	return []*model.Run{m.run}, nil
}

func statusListContains(statuses []model.Status, status model.Status) bool {
	for _, candidate := range statuses {
		if candidate == status {
			return true
		}
	}
	return false
}

func newRemoteTestRun(status model.Status) *model.Run {
	return &model.Run{
		IssueID:        "ISSUE-REMOTE-1",
		RunID:          "run-1",
		Agent:          "codex",
		Status:         status,
		Target:         "mac",
		TargetHost:     "CA-1",
		TargetWorkerID: "host-CA-1",
		SessionName:    "run-ISSUE-REMOTE-1-run-1",
		Multiplexer:    "tmux",
	}
}

func TestMonitorAllObservesQueuedNeverAliveRun(t *testing.T) {
	cases := []struct {
		name        string
		startedAt   time.Time
		wantUnknown bool
	}{
		{
			name:      "inside grace",
			startedAt: time.Now(),
		},
		{
			name:        "after grace",
			startedAt:   time.Now().Add(-2 * neverAliveVerdictGrace),
			wantUnknown: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDaemon()
			d.socketServer = NewSocketServer(func(string) (store.Store, error) {
				return nil, nil
			}, d.logger)
			d.socketServer.currentWorkerID = "host-local"

			run := newRemoteTestRun(model.StatusQueued)
			run.IssueID = "ISSUE-QUEUED-1"
			run.RunID = model.RunID("run-" + strings.ReplaceAll(tc.name, " ", "-"))
			run.StartedAt = tc.startedAt
			run.Branch = ""
			run.PRUrl = ""

			st := &mockStoreForMonitorAll{
				mockStoreRecordingEvents: mockStoreRecordingEvents{
					mockStoreForUpdate: mockStoreForUpdate{
						issue: &model.Issue{ID: run.IssueID, Status: model.IssueStatusOpen},
					},
					run: run,
				},
			}
			registerRepoContextForTest(t, d.socketServer, "project-queued", "", st)

			captures := 0
			d.remoteCaptureFn = func(*model.Run, string, string, int) (string, error) {
				captures++
				return "", errors.New("session_not_found")
			}

			for i := 0; i < deadChecksBeforeFailed; i++ {
				d.monitorAll()
				if state := d.runStates[run.Ref().String()]; state != nil {
					state.RemoteCaptureAt = time.Time{}
				}
			}

			if len(st.listRunsFilters) == 0 {
				t.Fatal("monitorAll must list runs from the repo store")
			}
			if !statusListContains(st.listRunsFilters[0].Status, model.StatusQueued) {
				t.Fatalf("monitorAll status filter = %v, want queued included", st.listRunsFilters[0].Status)
			}
			if captures != deadChecksBeforeFailed {
				t.Fatalf("queued run must be observed each dead check, got %d captures want %d", captures, deadChecksBeforeFailed)
			}

			gotUnknown := false
			for _, event := range st.events {
				if event.Type == model.EventTypeStatus && event.Name == string(model.StatusUnknown) {
					gotUnknown = true
				}
			}
			if gotUnknown != tc.wantUnknown {
				t.Fatalf("unknown status event = %t, want %t", gotUnknown, tc.wantUnknown)
			}
		})
	}
}

func TestMonitorRemoteRunDetectsPROpenFromWorkerCapture(t *testing.T) {
	d := newTestDaemon()
	captures := 0
	d.remoteCaptureFn = func(run *model.Run, projectID, projectRoot string, lines int) (string, error) {
		captures++
		return "work done\nopened https://github.com/org/repo/pull/99 for review\n", nil
	}

	run := newRemoteTestRun(model.StatusRunning)
	st := &mockStoreRecordingEvents{mockStoreForUpdate: mockStoreForUpdate{issue: &model.Issue{ID: "ISSUE-REMOTE-1", Status: model.IssueStatusOpen}}}
	state := d.getOrCreateState(run)
	mgr := agent.GetManager(run)

	if err := d.monitorRemoteRun(run, st, "proj", "/root", state, mgr); err != nil {
		t.Fatalf("monitorRemoteRun() error = %v", err)
	}

	if captures != 1 {
		t.Fatalf("expected exactly one worker capture, got %d", captures)
	}
	if !state.WasAlive {
		t.Fatal("expected successful capture to mark run alive")
	}
	if !state.PRRecorded {
		t.Fatal("expected PR URL in captured output to be recorded")
	}
	if st.appendEventCalls != 2 {
		t.Fatalf("expected artifact + status events, got %d AppendEvent calls", st.appendEventCalls)
	}

	// Within the remote capture interval the daemon must not issue another lease.
	if err := d.monitorRemoteRun(run, st, "proj", "/root", state, mgr); err != nil {
		t.Fatalf("monitorRemoteRun() second call error = %v", err)
	}
	if captures != 1 {
		t.Fatalf("expected second call within interval to skip capture, got %d captures", captures)
	}
}

func TestMonitorRemoteRunInfraErrorBacksOffWithoutDeathCount(t *testing.T) {
	d := newTestDaemon()
	d.remoteCaptureFn = func(run *model.Run, projectID, projectRoot string, lines int) (string, error) {
		return "", errors.New(`no active worker available for target "host-CA-1"; start orch-worker with --worker-id host-CA-1 on the target host`)
	}

	run := newRemoteTestRun(model.StatusRunning)
	st := &mockStoreForUpdate{issue: &model.Issue{ID: "ISSUE-REMOTE-1", Status: model.IssueStatusOpen}}
	state := d.getOrCreateState(run)
	mgr := agent.GetManager(run)

	if err := d.monitorRemoteRun(run, st, "proj", "/root", state, mgr); err != nil {
		t.Fatalf("monitorRemoteRun() error = %v", err)
	}

	if state.DeadCheckCount != 0 {
		t.Fatalf("worker outage must not count toward agent death, got dead count %d", state.DeadCheckCount)
	}
	if st.appendEventCalls != 0 {
		t.Fatalf("expected no status events on worker outage, got %d", st.appendEventCalls)
	}
	if state.CaptureFailureCount != 1 {
		t.Fatalf("expected capture failure backoff to engage, got count %d", state.CaptureFailureCount)
	}
	if state.CaptureRetryAt.IsZero() {
		t.Fatal("expected capture retry backoff to be scheduled")
	}
}

func TestMonitorRemoteRunSessionGoneNeverAliveMarksUnknown(t *testing.T) {
	d := newTestDaemon()
	d.remoteCaptureFn = func(run *model.Run, projectID, projectRoot string, lines int) (string, error) {
		return "", errors.New("session run-ISSUE-REMOTE-1-run-1 not found (run may not be active)")
	}

	run := newRemoteTestRun(model.StatusRunning)
	st := &mockStoreRecordingEvents{mockStoreForUpdate: mockStoreForUpdate{issue: &model.Issue{ID: "ISSUE-REMOTE-1", Status: model.IssueStatusOpen}}}
	state := d.getOrCreateState(run)
	mgr := agent.GetManager(run)

	for i := 0; i < deadChecksBeforeFailed; i++ {
		// A session-gone reply is a completed worker round-trip and is paced
		// like a successful capture; rewind the pacing clock between checks.
		state.RemoteCaptureAt = time.Now().Add(-2 * remoteCaptureInterval)
		if err := d.monitorRemoteRun(run, st, "proj", "/root", state, mgr); err != nil {
			t.Fatalf("monitorRemoteRun() call %d error = %v", i+1, err)
		}
	}

	if state.DeadCheckCount != deadChecksBeforeFailed {
		t.Fatalf("expected %d dead checks, got %d", deadChecksBeforeFailed, state.DeadCheckCount)
	}
	if st.appendEventCalls != 1 {
		t.Fatalf("expected exactly one status event after dead checks, got %d", st.appendEventCalls)
	}
}

func TestMonitorRemoteRunSessionGonePacesLeaseRoundTrips(t *testing.T) {
	d := newTestDaemon()
	captures := 0
	d.remoteCaptureFn = func(run *model.Run, projectID, projectRoot string, lines int) (string, error) {
		captures++
		return "", errors.New("session run-ISSUE-REMOTE-1-run-1 not found (run may not be active)")
	}

	run := newRemoteTestRun(model.StatusRunning)
	st := &mockStoreForUpdate{issue: &model.Issue{ID: "ISSUE-REMOTE-1", Status: model.IssueStatusOpen}}
	state := d.getOrCreateState(run)
	mgr := agent.GetManager(run)

	for i := 0; i < 3; i++ {
		if err := d.monitorRemoteRun(run, st, "proj", "/root", state, mgr); err != nil {
			t.Fatalf("monitorRemoteRun() call %d error = %v", i+1, err)
		}
	}

	if captures != 1 {
		t.Fatalf("dead-session checks must be paced by the remote capture interval, got %d lease round-trips", captures)
	}
	if state.DeadCheckCount != 1 {
		t.Fatalf("expected a single dead check within one interval, got %d", state.DeadCheckCount)
	}
}

func TestMonitorRemoteRunSessionGoneWithRecordedPRInfersPROpen(t *testing.T) {
	d := newTestDaemon()
	d.remoteCaptureFn = func(run *model.Run, projectID, projectRoot string, lines int) (string, error) {
		return "", errors.New("session run-ISSUE-REMOTE-1-run-1 not found (run may not be active)")
	}

	run := newRemoteTestRun(model.StatusRunning)
	run.Branch = "issue/ISSUE-REMOTE-1/run-1"
	// Non-GitHub URL keeps the lookup offline; the recorded PR itself is the
	// completion evidence and must preserve pr_open.
	run.PRUrl = "https://gitlab.com/org/repo/merge_requests/9"
	st := &mockStoreRecordingEvents{mockStoreForUpdate: mockStoreForUpdate{issue: &model.Issue{ID: "ISSUE-REMOTE-1", Status: model.IssueStatusOpen}}}
	state := d.getOrCreateState(run)
	mgr := agent.GetManager(run)

	for i := 0; i < deadChecksBeforeFailed; i++ {
		state.RemoteCaptureAt = time.Now().Add(-2 * remoteCaptureInterval)
		if err := d.monitorRemoteRun(run, st, "proj", "/root", state, mgr); err != nil {
			t.Fatalf("monitorRemoteRun() call %d error = %v", i+1, err)
		}
	}

	if len(st.events) != 1 {
		t.Fatalf("expected exactly one status event, got %d", len(st.events))
	}
	if got := st.events[0].Name; got != string(model.StatusPROpen) {
		t.Fatalf("expected pr_open inferred from recorded PR, got %s", got)
	}
}

func TestMonitorRemoteRunSessionGoneNeverAliveWithinGraceWaits(t *testing.T) {
	d := newTestDaemon()
	d.remoteCaptureFn = func(run *model.Run, projectID, projectRoot string, lines int) (string, error) {
		return "", errors.New("session run-ISSUE-REMOTE-1-run-1 not found (run may not be active)")
	}

	run := newRemoteTestRun(model.StatusBooting)
	run.StartedAt = time.Now() // just booted: worker has not created the session yet
	st := &mockStoreForUpdate{issue: &model.Issue{ID: "ISSUE-REMOTE-1", Status: model.IssueStatusOpen}}
	state := d.getOrCreateState(run)
	mgr := agent.GetManager(run)

	for i := 0; i < deadChecksBeforeFailed+2; i++ {
		state.RemoteCaptureAt = time.Now().Add(-2 * remoteCaptureInterval)
		if err := d.monitorRemoteRun(run, st, "proj", "/root", state, mgr); err != nil {
			t.Fatalf("monitorRemoteRun() call %d error = %v", i+1, err)
		}
	}

	if st.appendEventCalls != 0 {
		t.Fatalf("a booting run must not get an unknown verdict within the grace window, got %d status events", st.appendEventCalls)
	}
}

func TestMonitorRemoteRunSessionGoneAfterAliveMarksFailed(t *testing.T) {
	d := newTestDaemon()
	d.remoteCaptureFn = func(run *model.Run, projectID, projectRoot string, lines int) (string, error) {
		return "", errors.New("not_found")
	}

	run := newRemoteTestRun(model.StatusRunning)
	st := &mockStoreForUpdate{issue: &model.Issue{ID: "ISSUE-REMOTE-1", Status: model.IssueStatusOpen}}
	state := d.getOrCreateState(run)
	state.WasAlive = true
	mgr := agent.GetManager(run)

	for i := 0; i < deadChecksBeforeFailed; i++ {
		state.RemoteCaptureAt = time.Now().Add(-2 * remoteCaptureInterval)
		if err := d.monitorRemoteRun(run, st, "proj", "/root", state, mgr); err != nil {
			t.Fatalf("monitorRemoteRun() call %d error = %v", i+1, err)
		}
	}

	if st.appendEventCalls != 1 {
		t.Fatalf("expected exactly one failed status event, got %d", st.appendEventCalls)
	}
}

func TestIsRemoteSessionGoneClassification(t *testing.T) {
	gone := []string{
		"not_found",
		"session run-x-1 not found (run may not be active)",
		"failed to capture session run-x-1: no server running on /tmp/tmux-501/default",
		"can't find pane run-x-1",
	}
	for _, msg := range gone {
		if !isRemoteSessionGone(errors.New(msg)) {
			t.Errorf("expected session-gone classification for %q", msg)
		}
	}

	infra := []string{
		`no active worker available for target "host-CA-1"; start orch-worker with --worker-id host-CA-1 on the target host`,
		"worker lease timed out: lease-123",
		"lease not found: lease-123",
		`no local project mapping for project_id "x" on worker "host-CA-1"; run 'orch --remote= daemon repo register /path/to/repo' on that host`,
		"dial tcp 1.2.3.4:7777: connect: connection refused",
	}
	for _, msg := range infra {
		if isRemoteSessionGone(errors.New(msg)) {
			t.Errorf("expected infra classification for %q", msg)
		}
	}
}

func TestRunIsWorkerDelegatedGate(t *testing.T) {
	d := newTestDaemon()
	run := newRemoteTestRun(model.StatusRunning)

	if d.runIsWorkerDelegated(run) {
		t.Fatal("daemon without socket server must not delegate")
	}

	d.socketServer = &SocketServer{currentWorkerID: "host-zeus"}
	if !d.runIsWorkerDelegated(run) {
		t.Fatal("run targeting another worker must be delegated")
	}

	local := newRemoteTestRun(model.StatusRunning)
	local.TargetWorkerID = "host-zeus"
	if d.runIsWorkerDelegated(local) {
		t.Fatal("run targeting the daemon's own worker must stay local")
	}
}
