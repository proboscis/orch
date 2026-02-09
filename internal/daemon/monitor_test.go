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

	"github.com/s22625/orch/internal/model"
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
}

func (m *mockStoreForUpdate) ResolveIssue(issueID string) (*model.Issue, error) {
	if m.resolveIssueErr != nil {
		return nil, m.resolveIssueErr
	}
	return m.issue, nil
}

func (m *mockStoreForUpdate) SetIssueStatus(issueID string, status model.IssueStatus) error {
	m.setIssueStatusCalls = append(m.setIssueStatusCalls, struct {
		issueID string
		status  model.IssueStatus
	}{issueID, status})
	return m.setIssueStatusErr
}

func (m *mockStoreForUpdate) AppendEvent(ref *model.RunRef, event *model.Event) error {
	m.appendEventCalls++
	return m.appendEventErr
}

func (m *mockStoreForUpdate) ListIssues() ([]*model.Issue, error) { return nil, nil }
func (m *mockStoreForUpdate) CreateRun(string, string, map[string]string) (*model.Run, error) {
	return nil, nil
}
func (m *mockStoreForUpdate) ListRuns(*store.ListRunsFilter) ([]*model.Run, error) { return nil, nil }
func (m *mockStoreForUpdate) GetRun(*model.RunRef) (*model.Run, error)             { return nil, nil }
func (m *mockStoreForUpdate) GetRunByShortID(string) (*model.Run, error)           { return nil, nil }
func (m *mockStoreForUpdate) GetLatestRun(string) (*model.Run, error)              { return nil, nil }
func (m *mockStoreForUpdate) RootPath() string                                     { return "" }
func (m *mockStoreForUpdate) DeleteRun(ref *model.RunRef) error                    { return nil }
func (m *mockStoreForUpdate) UpdateIssue(issue *model.Issue) error                 { return nil }
func (m *mockStoreForUpdate) ValidateIssueFiles(issueID string) (*store.ValidationResult, error) {
	return nil, nil
}
func (m *mockStoreForUpdate) WriteAgentPrompt(ref *model.RunRef, content string) error { return nil }
func (m *mockStoreForUpdate) ReadAgentPrompt(ref *model.RunRef) (string, error)        { return "", nil }
func (m *mockStoreForUpdate) CreateIssue(issue *model.Issue) error                     { return nil }

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
			name:              "StatusBlocked does not resolve",
			status:            model.StatusBlocked,
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

			err := d.updateStatus(run, tt.status, st)
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

	err := d.updateStatus(run, model.StatusDone, st)
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

	err := d.updateStatus(run, model.StatusDone, st)
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

	err := d.monitorRun(run, st)
	if err != nil {
		t.Fatalf("monitorRun() error = %v", err)
	}

	if st.appendEventCalls != 0 {
		t.Errorf("expected no AppendEvent calls for canceled run, got %d", st.appendEventCalls)
	}

	state := d.runStates[run.IssueID+"#"+run.RunID]
	if state != nil {
		t.Error("expected no state to be created for canceled run")
	}
}

func TestCheckPRMergedWithURLReturnsURLWhenFound(t *testing.T) {
	d := newTestDaemon()
	run := &model.Run{
		IssueID: "test-issue",
		RunID:   "run-1",
		PRUrl:   "",
		Branch:  "feature/test",
	}

	merged, url := d.checkPRMergedWithURL(run, nil)

	if merged && url == "" {
		t.Error("expected non-empty URL when merged PR is found")
	}
	_ = merged
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
	if err := d.monitorRun(run, st); err != nil {
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
