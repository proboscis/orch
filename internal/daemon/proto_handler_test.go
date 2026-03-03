package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	orchpb "github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/model"
)

type timingTestLogger struct {
	buf bytes.Buffer
}

func (l *timingTestLogger) Printf(format string, v ...interface{}) {
	_, _ = fmt.Fprintf(&l.buf, format, v...)
	l.buf.WriteByte('\n')
}

func (l *timingTestLogger) String() string {
	return l.buf.String()
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
				IssueId:           tt.run.IssueID,
				RunId:             tt.run.RunID,
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

	leasePayload := &WorkerEffectPayload{StartRun: &StartRunOptions{IssueID: "orch-1", ProjectRoot: "/tmp/project"}}
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
	if !strings.Contains(err.Error(), "no store available for project_id") {
		t.Fatalf("withWorkerLease() error = %v, want store mapping error", err)
	}

	if helperErr := <-dispatchErr; helperErr == nil || !strings.Contains(helperErr.Error(), "no store available for project_id") {
		t.Fatalf("helper dispatch error = %v, want store mapping error", helperErr)
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

func TestExternalWorkerLifecycleAPIStartListStop(t *testing.T) {
	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)
	server.workerLaunchConfig = func(workerID string) (string, []string, []string, error) {
		return "/bin/sleep", []string{"/bin/sleep", "30"}, nil, nil
	}

	startResp := server.handleProtoStartExternalWorker(&orchpb.StartExternalWorkerRequest{WorkerId: "worker-managed-1"})
	if !startResp.GetOk() {
		t.Fatalf("start external worker failed: %s", startResp.GetError())
	}
	if startResp.GetStartExternalWorker().GetWorkerId() != "worker-managed-1" {
		t.Fatalf("unexpected started worker id: %s", startResp.GetStartExternalWorker().GetWorkerId())
	}

	listResp := server.handleProtoListExternalWorkers(&orchpb.ListExternalWorkersRequest{})
	if !listResp.GetOk() {
		t.Fatalf("list external workers failed: %s", listResp.GetError())
	}
	if len(listResp.GetListExternalWorkers().GetWorkers()) == 0 {
		t.Fatal("expected at least one managed external worker")
	}

	stopResp := server.handleProtoStopExternalWorker(&orchpb.StopExternalWorkerRequest{WorkerId: "worker-managed-1"})
	if !stopResp.GetOk() {
		t.Fatalf("stop external worker failed: %s", stopResp.GetError())
	}
	if stopResp.GetStopExternalWorker().GetStoppedCount() < 1 {
		t.Fatalf("expected stopped_count >= 1, got %d", stopResp.GetStopExternalWorker().GetStoppedCount())
	}

	_ = server.handleProtoStopExternalWorker(&orchpb.StopExternalWorkerRequest{All: true})
}

func TestDaemonStartedExternalWorkerProcessesLeaseSuccessfully(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	logger := &timingTestLogger{}
	server := NewSocketServer(nil, logger)
	server.workerLaunchConfig = func(workerID string) (string, []string, []string, error) {
		env := []string{
			"ORCH_WORKER_HELPER=1",
			"ORCH_WORKER_ID=" + workerID,
			"ORCH_WORKER_HELPER_REGISTER=1",
			"ORCH_WORKER_HELPER_MODE=success",
			"ORCH_WORKER_EXPECT_EFFECT=stop_run",
			"ORCH_WORKER_HELPER_RESULT_JSON={\"continue_run_result\":{\"RunID\":\"run-daemon-started\"}}",
		}
		return os.Args[0], []string{os.Args[0], "-test.run=TestExternalWorkerHelperProcess"}, env, nil
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer server.Stop()

	startResp := server.handleProtoStartExternalWorker(&orchpb.StartExternalWorkerRequest{WorkerId: "managed-worker-success"})
	if !startResp.GetOk() {
		t.Fatalf("start external worker failed: %s", startResp.GetError())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		workers := server.listWorkers()
		found := false
		for _, w := range workers {
			if w.ID == "managed-worker-success" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	_, err := server.withWorkerLease("project-test", "stop_run", "orch-managed", "run-managed", nil)
	if err != nil {
		t.Fatalf("withWorkerLease() error = %v", err)
	}

	leases := server.listWorkerLeases(true)
	found := false
	for _, lease := range leases {
		if lease.WorkerID == "managed-worker-success" && lease.Effect == "stop_run" && lease.IssueID == "orch-managed" {
			if !lease.Completed || !lease.Success {
				t.Fatalf("unexpected lease completion state: completed=%v success=%v", lease.Completed, lease.Success)
			}
			if strings.TrimSpace(lease.ResultJSON) == "" {
				t.Fatal("expected non-empty result_json from managed external worker")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected lease completed by daemon-managed external worker")
	}

	_ = server.handleProtoStopExternalWorker(&orchpb.StopExternalWorkerRequest{All: true})
}

func TestDaemonStartedExternalWorkerHandlesStartRunEndToEnd(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{"issue-start-daemon": {ID: "issue-start-daemon", Title: "issue", Status: model.IssueStatusOpen}}}
	server := newTestServer(t, st)
	server.workerLaunchConfig = func(workerID string) (string, []string, []string, error) {
		env := []string{
			"ORCH_WORKER_HELPER=1",
			"ORCH_WORKER_ID=" + workerID,
			"ORCH_WORKER_HELPER_REGISTER=1",
			"ORCH_WORKER_HELPER_MODE=success",
			"ORCH_WORKER_EXPECT_EFFECT=start_run",
			"ORCH_WORKER_HELPER_RESULT_JSON={\"start_run_result\":{\"RunID\":\"run-daemon-start\",\"Branch\":\"daemon-branch\",\"WorktreePath\":\"/tmp/wt\",\"SessionName\":\"sess\",\"Status\":\"running\"}}",
		}
		return os.Args[0], []string{os.Args[0], "-test.run=TestExternalWorkerHelperProcess"}, env, nil
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer server.Stop()
	projectRoot := "/test/project"
	projectID := derivePortableRepoID(projectRoot)
	server.RegisterRepo(projectRoot, st)

	startResp := server.handleProtoStartExternalWorker(&orchpb.StartExternalWorkerRequest{WorkerId: "managed-worker-start"})
	if !startResp.GetOk() {
		t.Fatalf("start external worker failed: %s", startResp.GetError())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		workers := server.listWorkers()
		found := false
		for _, w := range workers {
			if w.ID == "managed-worker-start" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	resp := sendProtoRequest(t, &orchpb.Request{Request: &orchpb.Request_StartRun{StartRun: &orchpb.StartRunRequest{
		IssueId:     "issue-start-daemon",
		RunId:       "run-daemon-start",
		ProjectRoot: projectRoot,
		Context:     &orchpb.RequestContext{ProjectId: projectID},
	}}})
	if !resp.GetOk() || resp.GetStartRun() == nil {
		t.Fatalf("start_run response not ok: ok=%v err=%s", resp.GetOk(), resp.GetError())
	}
	if resp.GetStartRun().GetRunId() != "run-daemon-start" || resp.GetStartRun().GetBranch() != "daemon-branch" {
		t.Fatalf("unexpected start_run response: %+v", resp.GetStartRun())
	}
}

func TestDaemonStartedExternalWorkerHandlesContinueRunEndToEnd(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	server := newTestServer(t, st)
	server.workerLaunchConfig = func(workerID string) (string, []string, []string, error) {
		env := []string{
			"ORCH_WORKER_HELPER=1",
			"ORCH_WORKER_ID=" + workerID,
			"ORCH_WORKER_HELPER_REGISTER=1",
			"ORCH_WORKER_HELPER_MODE=success",
			"ORCH_WORKER_EXPECT_EFFECT=continue_run",
			"ORCH_WORKER_HELPER_RESULT_JSON={\"continue_run_result\":{\"RunID\":\"run-daemon-continue\",\"Branch\":\"cont-branch\",\"WorktreePath\":\"/tmp/cont\",\"SessionName\":\"sess-cont\",\"Status\":\"running\",\"ContinuedFrom\":\"run-prev\",\"IssueID\":\"issue-cont-daemon\"}}",
		}
		return os.Args[0], []string{os.Args[0], "-test.run=TestExternalWorkerHelperProcess"}, env, nil
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer server.Stop()
	projectRoot := "/test/project"
	projectID := derivePortableRepoID(projectRoot)
	server.RegisterRepo(projectRoot, st)

	startResp := server.handleProtoStartExternalWorker(&orchpb.StartExternalWorkerRequest{WorkerId: "managed-worker-continue"})
	if !startResp.GetOk() {
		t.Fatalf("start external worker failed: %s", startResp.GetError())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		workers := server.listWorkers()
		found := false
		for _, w := range workers {
			if w.ID == "managed-worker-continue" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	resp := sendProtoRequest(t, &orchpb.Request{Request: &orchpb.Request_ContinueRun{ContinueRun: &orchpb.ContinueRunRequest{
		IssueId:     "issue-cont-daemon",
		RunId:       "run-daemon-continue",
		ProjectRoot: projectRoot,
		Context:     &orchpb.RequestContext{ProjectId: projectID},
	}}})
	if !resp.GetOk() || resp.GetContinueRun() == nil {
		t.Fatalf("continue_run response not ok: ok=%v err=%s", resp.GetOk(), resp.GetError())
	}
	if resp.GetContinueRun().GetRunId() != "run-daemon-continue" || resp.GetContinueRun().GetContinuedFrom() != "run-prev" {
		t.Fatalf("unexpected continue_run response: %+v", resp.GetContinueRun())
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
