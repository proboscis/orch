package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/orchapi"
)

type mockWaitAPI struct {
	result     *orchapi.WaitForRunsResult
	err        error
	gotRefs    []string
	gotTimeout int
}

func (m *mockWaitAPI) WaitForRuns(ctx context.Context, refs []string, timeoutSeconds int) (*orchapi.WaitForRunsResult, error) {
	m.gotRefs = append([]string(nil), refs...)
	m.gotTimeout = timeoutSeconds
	return m.result, m.err
}

func newWaitDepsForTest(api waitAPI) (*waitDeps, *bytes.Buffer, *bytes.Buffer, *[]int) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCodes := []int{}

	return &waitDeps{
		getAPI: func() (waitAPI, error) { return api, nil },
		stdout: stdout,
		stderr: stderr,
		exit: func(code int) {
			exitCodes = append(exitCodes, code)
		},
	}, stdout, stderr, &exitCodes
}

func TestRunWaitWithDepsOutputsJSON(t *testing.T) {
	api := &mockWaitAPI{
		result: &orchapi.WaitForRunsResult{
			RunID:   "242f5d",
			Status:  orchapi.RunStatusPROpen,
			IssueID: "ISSUE-TRD-067-10",
			PRURL:   "https://example.test/pr/123",
		},
	}
	deps, stdout, stderr, exitCodes := newWaitDepsForTest(api)

	err := runWaitWithDeps(context.Background(), []string{"242f5d", "380b5d"}, &waitOptions{Timeout: 3600}, deps)
	if err != nil {
		t.Fatalf("runWaitWithDeps() error = %v", err)
	}

	if got := *exitCodes; len(got) != 0 {
		t.Fatalf("exit codes = %v, want none", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if got, want := api.gotTimeout, 3600; got != want {
		t.Fatalf("timeout = %d, want %d", got, want)
	}
	if got, want := api.gotRefs, []string{"242f5d", "380b5d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
	t.Logf("wait output: %s", strings.TrimSpace(stdout.String()))

	var result waitCommandResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if result.RunID != "242f5d" {
		t.Fatalf("run_id = %q, want %q", result.RunID, "242f5d")
	}
	if result.Status != "pr_open" {
		t.Fatalf("status = %q, want %q", result.Status, "pr_open")
	}
	if result.Issue != "ISSUE-TRD-067-10" {
		t.Fatalf("issue = %q, want %q", result.Issue, "ISSUE-TRD-067-10")
	}
	if result.PRURL != "https://example.test/pr/123" {
		t.Fatalf("pr_url = %q, want %q", result.PRURL, "https://example.test/pr/123")
	}
}

func TestRunWaitWithDepsTimeoutExits124(t *testing.T) {
	api := &mockWaitAPI{err: errors.Join(errors.New("timed out waiting for runs"), orchapi.ErrTimeout)}
	deps, _, stderr, exitCodes := newWaitDepsForTest(api)

	err := runWaitWithDeps(context.Background(), []string{"242f5d"}, &waitOptions{Timeout: 5}, deps)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	if got := *exitCodes; len(got) != 1 || got[0] != waitExitTimeout {
		t.Fatalf("exit codes = %v, want [%d]", got, waitExitTimeout)
	}
	if got := stderr.String(); got == "" {
		t.Fatal("expected stderr output for timeout")
	}
}

func TestRunWaitWithDepsGenericErrorExits1(t *testing.T) {
	api := &mockWaitAPI{err: errors.New("daemon unavailable")}
	deps, _, _, exitCodes := newWaitDepsForTest(api)

	err := runWaitWithDeps(context.Background(), []string{"242f5d"}, &waitOptions{}, deps)
	if err == nil {
		t.Fatal("expected error")
	}

	if got := *exitCodes; len(got) != 1 || got[0] != 1 {
		t.Fatalf("exit codes = %v, want [1]", got)
	}
}

func TestRunWaitWithDepsRejectsNegativeTimeout(t *testing.T) {
	api := &mockWaitAPI{}
	deps, _, _, exitCodes := newWaitDepsForTest(api)

	err := runWaitWithDeps(context.Background(), []string{"242f5d"}, &waitOptions{Timeout: -1}, deps)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if got := *exitCodes; len(got) != 1 || got[0] != 1 {
		t.Fatalf("exit codes = %v, want [1]", got)
	}
	if api.gotRefs != nil {
		t.Fatalf("api should not be called, got refs %v", api.gotRefs)
	}
}
