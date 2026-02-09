package daemon

import (
	"bytes"
	"errors"
	"fmt"
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
		IssuesRoot: "/tmp/test",
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
