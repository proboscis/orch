package multiplexer

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
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

func TestTmuxMultiplexer_Type(t *testing.T) {
	tm := NewTmuxMultiplexer()
	if tm.Type() != TypeTmux {
		t.Fatalf("Type() = %v, want %v", tm.Type(), TypeTmux)
	}
}

func TestTmuxMultiplexer_IsAvailable(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if !tm.IsAvailable() {
		t.Fatal("expected tmux to be available")
	}

	call := exec.recorded[0]
	if call.name != "tmux" {
		t.Fatalf("command = %q, want %q", call.name, "tmux")
	}
	if !equalArgs(call.args, []string{"-V"}) {
		t.Fatalf("args = %v, want %v", call.args, []string{"-V"})
	}
}

func TestTmuxMultiplexer_IsAvailable_NotInstalled(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 1}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if tm.IsAvailable() {
		t.Fatal("expected tmux to not be available")
	}
}

func TestTmuxMultiplexer_IsInsideSession(t *testing.T) {
	tm := NewTmuxMultiplexer()

	// When not inside tmux
	os.Unsetenv("TMUX")
	if tm.IsInsideSession() {
		t.Fatal("expected not to be inside session")
	}

	// When inside tmux
	os.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	t.Cleanup(func() { os.Unsetenv("TMUX") })
	if !tm.IsInsideSession() {
		t.Fatal("expected to be inside session")
	}
}

func TestTmuxMultiplexer_HasSession(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if !tm.HasSession("demo") {
		t.Fatal("expected session to exist")
	}

	call := exec.recorded[0]
	if call.name != "tmux" {
		t.Fatalf("command = %q, want %q", call.name, "tmux")
	}
	if !equalArgs(call.args, []string{"has-session", "-t", "demo"}) {
		t.Fatalf("args = %v, want %v", call.args, []string{"has-session", "-t", "demo"})
	}
}

func TestTmuxMultiplexer_HasSession_Missing(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 1}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if tm.HasSession("demo") {
		t.Fatal("expected session to be missing")
	}
}

func TestTmuxMultiplexer_NewSession(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	err := tm.NewSession(&SessionConfig{
		SessionName: "sess",
		WorkDir:     "/work",
		Command:     "echo hi",
		Env:         []string{"FOO=bar"},
		WindowName:  "main",
	})
	if err != nil {
		t.Fatalf("NewSession error: %v", err)
	}

	if len(exec.recorded) != 1 {
		t.Fatalf("expected 1 call, got %d", len(exec.recorded))
	}

	first := exec.recorded[0]
	if !equalArgs(first.args, []string{"new-session", "-d", "-s", "sess", "-e", "FOO=bar", "-c", "/work", "-n", "main", "echo hi"}) {
		t.Fatalf("new-session args = %v", first.args)
	}
	if envHas(first.cmd.Env, "FOO=bar") {
		t.Fatalf("expected tmux session env to be passed via -e, not client env: %v", first.cmd.Env)
	}
}

func TestTmuxMultiplexer_NewSession_ShellCommandUsesSendKeys(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}, {exitCode: 0}, {exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	err := tm.NewSession(&SessionConfig{
		SessionName: "sess",
		WorkDir:     "/work",
		Command:     "zsh",
		Env:         []string{"FOO=bar"},
		WindowName:  "main",
	})
	if err != nil {
		t.Fatalf("NewSession error: %v", err)
	}

	if len(exec.recorded) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(exec.recorded))
	}

	first := exec.recorded[0]
	if !equalArgs(first.args, []string{"new-session", "-d", "-s", "sess", "-e", "FOO=bar", "-c", "/work", "-n", "main"}) {
		t.Fatalf("new-session args = %v", first.args)
	}
	if envHas(first.cmd.Env, "FOO=bar") {
		t.Fatalf("expected tmux session env to be passed via -e, not client env: %v", first.cmd.Env)
	}

	second := exec.recorded[1]
	if !equalArgs(second.args, []string{"send-keys", "-t", "sess", "-l", "zsh"}) {
		t.Fatalf("send-keys literal args = %v", second.args)
	}
	third := exec.recorded[2]
	if !equalArgs(third.args, []string{"send-keys", "-t", "sess", "Enter"}) {
		t.Fatalf("send-keys Enter args = %v", third.args)
	}
}

func TestTmuxMultiplexer_KillSession(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.KillSession("sess"); err != nil {
		t.Fatalf("KillSession error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"kill-session", "-t", "sess"}) {
		t.Fatalf("kill-session args = %v", call.args)
	}
}

func TestTmuxMultiplexer_ListSessions(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "one\ntwo\n\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	sessions, err := tm.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions error: %v", err)
	}
	if len(sessions) != 2 || sessions[0] != "one" || sessions[1] != "two" {
		t.Fatalf("unexpected sessions: %v", sessions)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"list-sessions", "-F", "#{session_name}"}) {
		t.Fatalf("list-sessions args = %v", call.args)
	}
}

func TestTmuxMultiplexer_ListWindows(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "0:dashboard:@1\n2:run:@2\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	windows, err := tm.ListWindows("sess")
	if err != nil {
		t.Fatalf("ListWindows error: %v", err)
	}

	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}
	if windows[0].Index != 0 || windows[0].Name != "dashboard" || windows[0].ID != "@1" {
		t.Fatalf("unexpected first window: %+v", windows[0])
	}
	if windows[1].Index != 2 || windows[1].Name != "run" || windows[1].ID != "@2" {
		t.Fatalf("unexpected second window: %+v", windows[1])
	}
}

func TestTmuxMultiplexer_NewWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}, {exitCode: 0}, {exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.NewWindow("sess", "work", "/home", "ls -la"); err != nil {
		t.Fatalf("NewWindow error: %v", err)
	}

	first := exec.recorded[0]
	if !equalArgs(first.args, []string{"new-window", "-t", "sess", "-n", "work", "-c", "/home"}) {
		t.Fatalf("new-window args = %v", first.args)
	}
}

func TestTmuxMultiplexer_SelectWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SelectWindow("sess", 2); err != nil {
		t.Fatalf("SelectWindow error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"select-window", "-t", "sess:2"}) {
		t.Fatalf("select-window args = %v", call.args)
	}
}

func TestTmuxMultiplexer_SelectWindowByID(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SelectWindowByID("@7"); err != nil {
		t.Fatalf("SelectWindowByID error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"select-window", "-t", "@7"}) {
		t.Fatalf("select-window args = %v", call.args)
	}
}

func TestTmuxMultiplexer_RenameWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.RenameWindow("sess", 3, "run-123"); err != nil {
		t.Fatalf("RenameWindow error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"rename-window", "-t", "sess:3", "run-123"}) {
		t.Fatalf("rename-window args = %v", call.args)
	}
}

func TestTmuxMultiplexer_ListPanes(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "%1:0:runs:orch\n%2:1:issues:orch\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	panes, err := tm.ListPanes("sess:0")
	if err != nil {
		t.Fatalf("ListPanes error: %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(panes))
	}
	if panes[0].ID != "%1" || panes[0].Index != 0 || panes[0].Title != "runs" || panes[0].Command != "orch" {
		t.Fatalf("unexpected pane: %+v", panes[0])
	}
}

func TestTmuxMultiplexer_SplitWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "%3\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	paneID, err := tm.SplitWindow("sess:0.0", true, 25)
	if err != nil {
		t.Fatalf("SplitWindow error: %v", err)
	}
	if paneID != "%3" {
		t.Fatalf("pane id = %q", paneID)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"split-window", "-d", "-t", "sess:0.0", "-P", "-F", "#{pane_id}", "-v", "-p", "25"}) {
		t.Fatalf("split-window args = %v", call.args)
	}
}

func TestTmuxMultiplexer_SelectPane(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SelectPane("%1"); err != nil {
		t.Fatalf("SelectPane error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"select-pane", "-t", "%1"}) {
		t.Fatalf("select-pane args = %v", call.args)
	}
}

func TestTmuxMultiplexer_SetPaneTitle(t *testing.T) {
	// SetPaneTitle: 1) display-message to get current pane, 2) select-pane to set title, 3) restore focus
	exec := &fakeExecutor{calls: []fakeCall{{output: "%2\n"}, {exitCode: 0}, {exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SetPaneTitle("%1", "chat"); err != nil {
		t.Fatalf("SetPaneTitle error: %v", err)
	}

	if len(exec.recorded) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(exec.recorded))
	}
	first := exec.recorded[0]
	if !equalArgs(first.args, []string{"display-message", "-p", "#{pane_id}"}) {
		t.Fatalf("display-message args = %v", first.args)
	}
	second := exec.recorded[1]
	if !equalArgs(second.args, []string{"select-pane", "-t", "%1", "-T", "chat"}) {
		t.Fatalf("select-pane args = %v", second.args)
	}
	// Third call restores focus to original pane (%2)
	third := exec.recorded[2]
	if !equalArgs(third.args, []string{"select-pane", "-t", "%2"}) {
		t.Fatalf("restore select-pane args = %v", third.args)
	}
}

func TestTmuxMultiplexer_KillPane(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.KillPane("%1"); err != nil {
		t.Fatalf("KillPane error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"kill-pane", "-t", "%1"}) {
		t.Fatalf("kill-pane args = %v", call.args)
	}
}

func TestTmuxMultiplexer_SwapPane(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SwapPane("%1", "%2"); err != nil {
		t.Fatalf("SwapPane error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"swap-pane", "-s", "%1", "-t", "%2"}) {
		t.Fatalf("swap-pane args = %v", call.args)
	}
}

func TestTmuxMultiplexer_SendKeys(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}, {exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SendKeys("sess", "hello world"); err != nil {
		t.Fatalf("SendKeys error: %v", err)
	}

	if len(exec.recorded) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(exec.recorded))
	}
	// First call should be literal
	if !equalArgs(exec.recorded[0].args, []string{"send-keys", "-t", "sess", "-l", "hello world"}) {
		t.Fatalf("first send-keys args = %v", exec.recorded[0].args)
	}
	// Second call should be Enter
	if !equalArgs(exec.recorded[1].args, []string{"send-keys", "-t", "sess", "Enter"}) {
		t.Fatalf("second send-keys args = %v", exec.recorded[1].args)
	}
}

func TestTmuxMultiplexer_SendKeysLiteral(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SendKeysLiteral("sess", "hello"); err != nil {
		t.Fatalf("SendKeysLiteral error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"send-keys", "-t", "sess", "-l", "hello"}) {
		t.Fatalf("send-keys args = %v", call.args)
	}
}

func TestTmuxMultiplexer_SendBracketedPaste(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}, {exitCode: 0}, {exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SendBracketedPaste("sess", "hello\nworld"); err != nil {
		t.Fatalf("SendBracketedPaste error: %v", err)
	}

	if len(exec.recorded) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(exec.recorded))
	}
	if got := exec.recorded[0].args; len(got) != 4 || got[0] != "set-buffer" || got[1] != "-b" || got[3] != "hello\nworld" {
		t.Fatalf("set-buffer args = %v", got)
	}
	if got := exec.recorded[1].args; len(got) != 6 || got[0] != "paste-buffer" || got[1] != "-b" || got[3] != "-p" || got[4] != "-t" || got[5] != "sess" {
		t.Fatalf("paste-buffer args = %v", got)
	}
	if exec.recorded[0].args[2] != exec.recorded[1].args[2] {
		t.Fatalf("buffer names mismatch: set=%q paste=%q", exec.recorded[0].args[2], exec.recorded[1].args[2])
	}
	if got := exec.recorded[2].args; len(got) != 3 || got[0] != "delete-buffer" || got[1] != "-b" {
		t.Fatalf("delete-buffer args = %v", got)
	}
	if exec.recorded[0].args[2] != exec.recorded[2].args[2] {
		t.Fatalf("buffer names mismatch: set=%q delete=%q", exec.recorded[0].args[2], exec.recorded[2].args[2])
	}
}

func TestTmuxMultiplexer_SendText(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SendText("sess", "hello world"); err != nil {
		t.Fatalf("SendText error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"send-keys", "-t", "sess", "hello world"}) {
		t.Fatalf("send-keys args = %v", call.args)
	}
}

func TestTmuxMultiplexer_CapturePane(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "line1\nline2\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	out, err := tm.CapturePane("sess", 5)
	if err != nil {
		t.Fatalf("CapturePane error: %v", err)
	}
	if out != "line1\nline2\n" {
		t.Fatalf("output = %q", out)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"capture-pane", "-t", "sess", "-p", "-S", "-5"}) {
		t.Fatalf("capture-pane args = %v", call.args)
	}
}

func TestTmuxMultiplexer_CurrentSession(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "orch-monitor\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	name, err := tm.CurrentSession()
	if err != nil {
		t.Fatalf("CurrentSession error: %v", err)
	}
	if name != "orch-monitor" {
		t.Fatalf("session = %q", name)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"display-message", "-p", "#{session_name}"}) {
		t.Fatalf("display-message args = %v", call.args)
	}
}

func TestTmuxMultiplexer_SetOption(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.SetOption("sess", "@orch_chat_pane", "%1"); err != nil {
		t.Fatalf("SetOption error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"set-option", "-t", "sess", "@orch_chat_pane", "%1"}) {
		t.Fatalf("set-option args = %v", call.args)
	}
}

func TestTmuxMultiplexer_GetOption(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "%1\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	value, err := tm.GetOption("sess", "@orch_chat_pane")
	if err != nil {
		t.Fatalf("GetOption error: %v", err)
	}
	if value != "%1" {
		t.Fatalf("option value = %q", value)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"show-option", "-t", "sess", "-v", "@orch_chat_pane"}) {
		t.Fatalf("show-option args = %v", call.args)
	}
}

func TestTmuxMultiplexer_LinkWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.LinkWindow("source", 1, "target", 2); err != nil {
		t.Fatalf("LinkWindow error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"link-window", "-s", "source:1", "-t", "target:2"}) {
		t.Fatalf("link-window args = %v", call.args)
	}
}

func TestTmuxMultiplexer_LinkWindowByID(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.LinkWindowByID("@7", "sess", 3); err != nil {
		t.Fatalf("LinkWindowByID error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"link-window", "-s", "@7", "-t", "sess:3"}) {
		t.Fatalf("link-window args = %v", call.args)
	}
}

func TestTmuxMultiplexer_UnlinkWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	if err := tm.UnlinkWindow("sess", 2); err != nil {
		t.Fatalf("UnlinkWindow error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"unlink-window", "-t", "sess:2"}) {
		t.Fatalf("unlink-window args = %v", call.args)
	}
}

func TestTmuxMultiplexer_ListPaneCommands(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "run-1\tclaude\nrun-1\tzsh\nrun-2\tbash\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	tm := NewTmuxMultiplexer()
	commands, err := tm.ListPaneCommands()
	if err != nil {
		t.Fatalf("ListPaneCommands error: %v", err)
	}
	want := map[string][]string{
		"run-1": {"claude", "zsh"},
		"run-2": {"bash"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"list-panes", "-a", "-F", "#{session_name}\t#{pane_current_command}"}) {
		t.Fatalf("list-panes args = %v", call.args)
	}
}

func TestTmuxMultiplexer_AgentAlive(t *testing.T) {
	paneCommands := map[string][]string{
		"run-1": {"claude"},
		"run-2": {"zsh"},
		"run-3": {"bash", "gemini"},
	}

	tests := []struct {
		name      string
		session   string
		commands  map[string][]string
		wantAlive bool
		wantKnown bool
	}{
		{name: "alive", session: "run-1", commands: paneCommands, wantAlive: true, wantKnown: true},
		{name: "shell-only", session: "run-2", commands: paneCommands, wantAlive: false, wantKnown: true},
		{name: "mixed", session: "run-3", commands: paneCommands, wantAlive: true, wantKnown: true},
		{name: "missing-session", session: "missing", commands: paneCommands, wantAlive: false, wantKnown: true},
		{name: "empty-session", session: "", commands: paneCommands, wantAlive: false, wantKnown: false},
		{name: "unknown-commands", session: "run-1", commands: nil, wantAlive: false, wantKnown: false},
	}

	tm := NewTmuxMultiplexer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAlive, gotKnown := tm.AgentAlive(tt.session, tt.commands)
			if gotAlive != tt.wantAlive || gotKnown != tt.wantKnown {
				t.Fatalf("AgentAlive = (%v,%v), want (%v,%v)", gotAlive, gotKnown, tt.wantAlive, tt.wantKnown)
			}
		})
	}
}

func TestIsShellCommand(t *testing.T) {
	shells := []string{"bash", "zsh", "sh", "fish", "ksh", "tcsh", "dash", "pwsh", "powershell", "cmd", "cmd.exe", "nu", "elvish"}
	for _, shell := range shells {
		if !isShellCommand(shell) {
			t.Errorf("isShellCommand(%q) = false, want true", shell)
		}
	}

	nonShells := []string{"claude", "gemini", "orch", "vim", "python"}
	for _, cmd := range nonShells {
		if isShellCommand(cmd) {
			t.Errorf("isShellCommand(%q) = true, want false", cmd)
		}
	}

	// Edge cases
	if !isShellCommand("") {
		t.Error("isShellCommand(\"\") = false, want true (empty is treated as shell)")
	}
	if !isShellCommand("  bash  ") {
		t.Error("isShellCommand(\"  bash  \") = false, want true (with spaces)")
	}
}

func TestShouldPassCommandToNewSession(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantRun bool
	}{
		{name: "empty", input: "", wantRun: false},
		{name: "shell", input: "zsh", wantRun: false},
		{name: "shell with args", input: "bash -lc 'echo hi'", wantRun: false},
		{name: "absolute shell path", input: "/bin/zsh -i", wantRun: false},
		{name: "agent command", input: "codex --yolo 'hello'", wantRun: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPassCommandToNewSession(tt.input); got != tt.wantRun {
				t.Fatalf("shouldPassCommandToNewSession(%q) = %v, want %v", tt.input, got, tt.wantRun)
			}
		})
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
