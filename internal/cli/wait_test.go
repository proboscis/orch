package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/orchapi"
)

type mockWaitAPI struct {
	orchapi.OrchAPI
	waitForStatusFunc func(ctx context.Context, ref orchapi.RunRef, status orchapi.RunStatus, timeout time.Duration) (*orchapi.Run, error)
}

func (m *mockWaitAPI) WaitForStatus(ctx context.Context, ref orchapi.RunRef, status orchapi.RunStatus, timeout time.Duration) (*orchapi.Run, error) {
	return m.waitForStatusFunc(ctx, ref, status, timeout)
}

func TestParseWaitUntilStatus(t *testing.T) {
	tests := []struct {
		input   string
		want    orchapi.RunStatus
		wantErr string
	}{
		{input: "pr_open", want: orchapi.RunStatusPROpen},
		{input: "done", want: orchapi.RunStatusDone},
		{input: "waiting", want: orchapi.RunStatusWaiting},
		{input: "failed", want: orchapi.RunStatusFailed},
		{input: "running", wantErr: "--until must be one of: pr_open, done, waiting, failed"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseWaitUntilStatus(tt.input)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("parseWaitUntilStatus(%q) error = %v, want %q", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWaitUntilStatus(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parseWaitUntilStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRunWaitWithDepsCallsAPI(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true

	var (
		gotRef     orchapi.RunRef
		gotStatus  orchapi.RunStatus
		gotTimeout time.Duration
	)

	mockAPI := &mockWaitAPI{
		waitForStatusFunc: func(ctx context.Context, ref orchapi.RunRef, status orchapi.RunStatus, timeout time.Duration) (*orchapi.Run, error) {
			gotRef = ref
			gotStatus = status
			gotTimeout = timeout
			return &orchapi.Run{
				IssueID: "orch-123",
				RunID:   "20260412-101500",
				Status:  status,
			}, nil
		},
	}

	deps := &waitDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mockAPI, nil
		},
	}

	err := runWaitWithDeps(context.Background(), "orch-123#20260412-101500", &waitOptions{
		Until:   "pr_open",
		Timeout: 42,
	}, deps)
	if err != nil {
		t.Fatalf("runWaitWithDeps() error = %v", err)
	}

	if gotRef.IssueID != "orch-123" || gotRef.RunID != "20260412-101500" || gotRef.ShortID != "" {
		t.Fatalf("ref = %+v, want full run ref", gotRef)
	}
	if gotStatus != orchapi.RunStatusPROpen {
		t.Fatalf("status = %q, want %q", gotStatus, orchapi.RunStatusPROpen)
	}
	if gotTimeout != 42*time.Second {
		t.Fatalf("timeout = %v, want %v", gotTimeout, 42*time.Second)
	}
}

func TestRunWaitWithDepsSupportsShortID(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true

	mockAPI := &mockWaitAPI{
		waitForStatusFunc: func(ctx context.Context, ref orchapi.RunRef, status orchapi.RunStatus, timeout time.Duration) (*orchapi.Run, error) {
			if ref.ShortID != "2f668e" || ref.IssueID != "" || ref.RunID != "" {
				t.Fatalf("unexpected ref: %+v", ref)
			}
			return &orchapi.Run{
				IssueID: "orch-123",
				RunID:   "20260412-101500",
				Status:  status,
			}, nil
		},
	}

	deps := &waitDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mockAPI, nil
		},
	}

	if err := runWaitWithDeps(context.Background(), "2f668e", &waitOptions{Until: "waiting"}, deps); err != nil {
		t.Fatalf("runWaitWithDeps() error = %v", err)
	}
}

func TestRunWaitWithDepsRejectsNegativeTimeout(t *testing.T) {
	resetGlobalOpts(t)

	err := runWaitWithDeps(context.Background(), "orch-123", &waitOptions{
		Until:   "done",
		Timeout: -1,
	}, &waitDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			t.Fatal("getAPI should not be called")
			return nil, nil
		},
	})
	if err == nil || err.Error() != "--timeout must be >= 0" {
		t.Fatalf("error = %v, want %q", err, "--timeout must be >= 0")
	}
}

func TestRunWaitWithDepsJSONOutput(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.JSON = true

	mockAPI := &mockWaitAPI{
		waitForStatusFunc: func(ctx context.Context, ref orchapi.RunRef, status orchapi.RunStatus, timeout time.Duration) (*orchapi.Run, error) {
			return &orchapi.Run{
				IssueID: "orch-123",
				RunID:   "20260412-101500",
				Status:  orchapi.RunStatusDone,
			}, nil
		},
	}

	deps := &waitDeps{
		getAPI: func() (orchapi.OrchAPI, error) {
			return mockAPI, nil
		},
	}

	out := captureStdout(t, func() {
		if err := runWaitWithDeps(context.Background(), "orch-123", &waitOptions{Until: "done"}, deps); err != nil {
			t.Fatalf("runWaitWithDeps() error = %v", err)
		}
	})

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal output: %v\noutput=%s", err, out)
	}

	if payload["ok"] != true {
		t.Fatalf("ok = %v, want true", payload["ok"])
	}
	if payload["issue_id"] != "orch-123" {
		t.Fatalf("issue_id = %v, want %q", payload["issue_id"], "orch-123")
	}
	if payload["run_id"] != "20260412-101500" {
		t.Fatalf("run_id = %v, want %q", payload["run_id"], "20260412-101500")
	}
	if payload["status"] != "done" {
		t.Fatalf("status = %v, want %q", payload["status"], "done")
	}
}

func TestNewWaitCmdRequiresUntilFlag(t *testing.T) {
	cmd := newWaitCmd()
	err := cmd.ParseFlags([]string{})
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	flag := cmd.Flag("until")
	if flag == nil {
		t.Fatal("expected until flag")
	}
	if !strings.Contains(flag.Usage, "pr_open|done|waiting|failed") {
		t.Fatalf("unexpected until flag usage: %q", flag.Usage)
	}
}
