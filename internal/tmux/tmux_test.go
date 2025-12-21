package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

type fakeCall struct {
	output   string
	exitCode int
}

type recordedCall struct {
	name string
	args []string
	cmd  *exec.Cmd
}

type fakeExecutor struct {
	calls    []fakeCall
	recorded []recordedCall
	index    int
}

func (f *fakeExecutor) Command(name string, args ...string) *exec.Cmd {
	call := fakeCall{exitCode: 0}
	if f.index < len(f.calls) {
		call = f.calls[f.index]
	}
	f.index++

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", name)
	cmd.Args = append(cmd.Args, args...)
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		fmt.Sprintf("FAKE_CMD_OUTPUT=%s", call.output),
		fmt.Sprintf("FAKE_CMD_EXIT_CODE=%d", call.exitCode),
	)

	rec := recordedCall{name: name, args: append([]string(nil), args...), cmd: cmd}
	f.recorded = append(f.recorded, rec)
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	if output := os.Getenv("FAKE_CMD_OUTPUT"); output != "" {
		_, _ = fmt.Fprint(os.Stdout, output)
	}

	code := 0
	if raw := os.Getenv("FAKE_CMD_EXIT_CODE"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			code = v
		}
	}

	os.Exit(code)
}

func TestHasSession(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	if !HasSession("demo") {
		t.Fatal("expected session to exist")
	}

	if len(exec.recorded) != 1 {
		t.Fatalf("expected 1 call, got %d", len(exec.recorded))
	}
	call := exec.recorded[0]
	if call.name != "tmux" {
		t.Fatalf("command = %q, want %q", call.name, "tmux")
	}
	if !equalArgs(call.args, []string{"has-session", "-t", "demo"}) {
		t.Fatalf("args = %v, want %v", call.args, []string{"has-session", "-t", "demo"})
	}
}

func TestHasSessionMissing(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 1}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	if HasSession("demo") {
		t.Fatal("expected session to be missing")
	}
}

func TestCapturePane(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "line1\nline2\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	out, err := CapturePane("sess", 5)
	if err != nil {
		t.Fatalf("CapturePane error: %v", err)
	}
	if out != "line1\nline2\n" {
		t.Fatalf("output = %q", out)
	}
	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"capture-pane", "-t", "sess", "-p", "-S", "-5"}) {
		t.Fatalf("args = %v", call.args)
	}
}

func TestListSessions(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "one\ntwo\n\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	sessions, err := ListSessions()
	if err != nil {
		t.Fatalf("ListSessions error: %v", err)
	}
	if len(sessions) != 2 || sessions[0] != "one" || sessions[1] != "two" {
		t.Fatalf("unexpected sessions: %v", sessions)
	}
}

func TestListSessionsError(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 1}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	if _, err := ListSessions(); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewSessionSendsKeys(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}, {exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	err := NewSession(&SessionConfig{
		SessionName: "sess",
		WorkDir:     "/work",
		Command:     "echo hi",
		Env:         []string{"FOO=bar"},
	})
	if err != nil {
		t.Fatalf("NewSession error: %v", err)
	}

	if len(exec.recorded) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(exec.recorded))
	}

	first := exec.recorded[0]
	if !equalArgs(first.args, []string{"new-session", "-d", "-s", "sess", "-c", "/work"}) {
		t.Fatalf("new-session args = %v", first.args)
	}
	if !envHas(first.cmd.Env, "FOO=bar") {
		t.Fatalf("missing env in new-session: %v", first.cmd.Env)
	}

	second := exec.recorded[1]
	if !equalArgs(second.args, []string{"send-keys", "-t", "sess", "echo hi", "Enter"}) {
		t.Fatalf("send-keys args = %v", second.args)
	}
}

func TestMoveWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	if err := MoveWindow("sess", "issues", 1); err != nil {
		t.Fatalf("MoveWindow error: %v", err)
	}

	if len(exec.recorded) != 1 {
		t.Fatalf("expected 1 call, got %d", len(exec.recorded))
	}
	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"move-window", "-s", "sess:issues", "-t", "sess:1"}) {
		t.Fatalf("move-window args = %v", call.args)
	}
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func envHas(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
