package multiplexer

import (
	"errors"
	"os"
	"testing"
)

func TestZellijMultiplexer_Type(t *testing.T) {
	zm := NewZellijMultiplexer()
	if zm.Type() != TypeZellij {
		t.Fatalf("Type() = %v, want %v", zm.Type(), TypeZellij)
	}
}

func TestZellijMultiplexer_IsAvailable(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if !zm.IsAvailable() {
		t.Fatal("expected zellij to be available")
	}

	call := exec.recorded[0]
	if call.name != "zellij" {
		t.Fatalf("command = %q, want %q", call.name, "zellij")
	}
	if !equalArgs(call.args, []string{"--version"}) {
		t.Fatalf("args = %v, want %v", call.args, []string{"--version"})
	}
}

func TestZellijMultiplexer_IsAvailable_NotInstalled(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 1}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if zm.IsAvailable() {
		t.Fatal("expected zellij to not be available")
	}
}

func TestZellijMultiplexer_IsInsideSession(t *testing.T) {
	zm := NewZellijMultiplexer()

	// When not inside zellij
	os.Unsetenv("ZELLIJ")
	if zm.IsInsideSession() {
		t.Fatal("expected not to be inside session")
	}

	// When inside zellij
	os.Setenv("ZELLIJ", "1")
	t.Cleanup(func() { os.Unsetenv("ZELLIJ") })
	if !zm.IsInsideSession() {
		t.Fatal("expected to be inside session")
	}
}

func TestZellijMultiplexer_HasSession(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "session1\nsession2\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if !zm.HasSession("session1") {
		t.Fatal("expected session to exist")
	}
}

func TestZellijMultiplexer_HasSession_Missing(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "session1\nsession2\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if zm.HasSession("missing") {
		t.Fatal("expected session to be missing")
	}
}

func TestZellijMultiplexer_ListSessions(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "session1\nsession2 (EXITED - ...)\nsession3\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	sessions, err := zm.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions error: %v", err)
	}
	// EXITED sessions should be filtered out
	if len(sessions) != 2 || sessions[0] != "session1" || sessions[1] != "session3" {
		t.Fatalf("unexpected sessions: %v", sessions)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"list-sessions", "-n"}) {
		t.Fatalf("list-sessions args = %v", call.args)
	}
}

func TestZellijMultiplexer_ListSessions_Empty(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "", exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	sessions, err := zm.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions error: %v", err)
	}
	if sessions != nil && len(sessions) != 0 {
		t.Fatalf("expected empty sessions, got: %v", sessions)
	}
}

func TestZellijMultiplexer_KillSession(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if err := zm.KillSession("sess"); err != nil {
		t.Fatalf("KillSession error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"kill-session", "sess"}) {
		t.Fatalf("kill-session args = %v", call.args)
	}
}

func TestZellijMultiplexer_NewWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if err := zm.NewWindow("sess", "work", "/home", ""); err != nil {
		t.Fatalf("NewWindow error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"--session", "sess", "action", "new-tab", "--cwd", "/home", "--name", "work"}) {
		t.Fatalf("new-tab args = %v", call.args)
	}
}

func TestZellijMultiplexer_SelectWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	// Zellij tabs are 1-indexed, so index 0 should become tab 1
	if err := zm.SelectWindow("sess", 0); err != nil {
		t.Fatalf("SelectWindow error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"--session", "sess", "action", "go-to-tab", "1"}) {
		t.Fatalf("go-to-tab args = %v", call.args)
	}
}

func TestZellijMultiplexer_RenameWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}, {exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if err := zm.RenameWindow("sess", 0, "newname"); err != nil {
		t.Fatalf("RenameWindow error: %v", err)
	}

	if len(exec.recorded) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(exec.recorded))
	}
	// First call selects the tab
	first := exec.recorded[0]
	if !equalArgs(first.args, []string{"--session", "sess", "action", "go-to-tab", "1"}) {
		t.Fatalf("go-to-tab args = %v", first.args)
	}
	// Second call renames it
	second := exec.recorded[1]
	if !equalArgs(second.args, []string{"--session", "sess", "action", "rename-tab", "newname"}) {
		t.Fatalf("rename-tab args = %v", second.args)
	}
}

func TestZellijMultiplexer_SendKeysLiteral(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if err := zm.SendKeysLiteral("sess", "hello"); err != nil {
		t.Fatalf("SendKeysLiteral error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"--session", "sess", "action", "write-chars", "--", "hello"}) {
		t.Fatalf("write-chars args = %v", call.args)
	}
}

func TestZellijMultiplexer_SendKeys(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}, {exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if err := zm.SendKeys("sess", "hello"); err != nil {
		t.Fatalf("SendKeys error: %v", err)
	}

	if len(exec.recorded) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(exec.recorded))
	}
	// First call is write-chars
	first := exec.recorded[0]
	if !equalArgs(first.args, []string{"--session", "sess", "action", "write-chars", "--", "hello"}) {
		t.Fatalf("write-chars args = %v", first.args)
	}
	// Second call sends Enter (keycode 10)
	second := exec.recorded[1]
	if !equalArgs(second.args, []string{"--session", "sess", "action", "write", "10"}) {
		t.Fatalf("write args = %v", second.args)
	}
}

func TestZellijMultiplexer_SplitWindow(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	_, err := zm.SplitWindow("sess:0", true, 25)
	if err != nil {
		t.Fatalf("SplitWindow error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"--session", "sess", "action", "new-pane", "--direction", "down"}) {
		t.Fatalf("new-pane args = %v", call.args)
	}
}

func TestZellijMultiplexer_SplitWindow_Horizontal(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	_, err := zm.SplitWindow("sess:0", false, 25)
	if err != nil {
		t.Fatalf("SplitWindow error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"--session", "sess", "action", "new-pane", "--direction", "right"}) {
		t.Fatalf("new-pane args = %v", call.args)
	}
}

func TestZellijMultiplexer_SplitWindow_NoSession(t *testing.T) {
	zm := NewZellijMultiplexer()
	_, err := zm.SplitWindow("", true, 25)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_SetPaneTitle(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if err := zm.SetPaneTitle("sess:0.1", "mytitle"); err != nil {
		t.Fatalf("SetPaneTitle error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"--session", "sess", "action", "rename-pane", "mytitle"}) {
		t.Fatalf("rename-pane args = %v", call.args)
	}
}

func TestZellijMultiplexer_SetPaneTitle_NoSession(t *testing.T) {
	zm := NewZellijMultiplexer()
	err := zm.SetPaneTitle("", "mytitle")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_KillPane(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	if err := zm.KillPane("sess:0.1"); err != nil {
		t.Fatalf("KillPane error: %v", err)
	}

	call := exec.recorded[0]
	if !equalArgs(call.args, []string{"--session", "sess", "action", "close-pane"}) {
		t.Fatalf("close-pane args = %v", call.args)
	}
}

func TestZellijMultiplexer_KillPane_NoSession(t *testing.T) {
	zm := NewZellijMultiplexer()
	err := zm.KillPane("")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_CurrentSession(t *testing.T) {
	// When inside a zellij session
	os.Setenv("ZELLIJ_SESSION_NAME", "my-session")
	t.Cleanup(func() { os.Unsetenv("ZELLIJ_SESSION_NAME") })

	zm := NewZellijMultiplexer()
	name, err := zm.CurrentSession()
	if err != nil {
		t.Fatalf("CurrentSession error: %v", err)
	}
	if name != "my-session" {
		t.Fatalf("session = %q, want %q", name, "my-session")
	}
}

func TestZellijMultiplexer_CurrentSession_NotInSession(t *testing.T) {
	os.Unsetenv("ZELLIJ_SESSION_NAME")

	zm := NewZellijMultiplexer()
	_, err := zm.CurrentSession()
	if err == nil {
		t.Fatal("expected error when not in session")
	}
}

func TestZellijMultiplexer_SetOption_NoOp(t *testing.T) {
	zm := NewZellijMultiplexer()
	// SetOption is a no-op for zellij, should not error
	if err := zm.SetOption("sess", "option", "value"); err != nil {
		t.Fatalf("SetOption error: %v", err)
	}
}

func TestZellijMultiplexer_GetOption_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	_, err := zm.GetOption("sess", "option")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_SelectPane_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	err := zm.SelectPane("%1")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_SwapPane_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	err := zm.SwapPane("%1", "%2")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_LinkWindow_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	err := zm.LinkWindow("source", 1, "target", 2)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_LinkWindowByID_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	err := zm.LinkWindowByID("@1", "sess", 2)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_UnlinkWindow_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	err := zm.UnlinkWindow("sess", 1)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_SelectWindowByID_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	err := zm.SelectWindowByID("@1")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_ListPaneCommands(t *testing.T) {
	zm := NewZellijMultiplexer()
	// ListPaneCommands should return empty map for zellij
	commands, err := zm.ListPaneCommands()
	if err != nil {
		t.Fatalf("ListPaneCommands error: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("expected empty commands, got %v", commands)
	}
}

func TestZellijMultiplexer_ListWindows_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	_, err := zm.ListWindows("sess")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_ListPanes_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	_, err := zm.ListPanes("sess:0")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
	}
}

func TestZellijMultiplexer_AgentAlive(t *testing.T) {
	// Test when session exists
	exec := &fakeExecutor{calls: []fakeCall{{output: "test-session\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	alive, known := zm.AgentAlive("test-session", nil)
	if !alive || !known {
		t.Fatalf("AgentAlive = (%v, %v), want (true, true)", alive, known)
	}
}

func TestZellijMultiplexer_AgentAlive_Missing(t *testing.T) {
	exec := &fakeExecutor{calls: []fakeCall{{output: "other-session\n"}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	alive, known := zm.AgentAlive("test-session", nil)
	// Zellij cannot determine pane commands, so when session doesn't exist,
	// both alive and known are false (we don't have enough info)
	if alive || known {
		t.Fatalf("AgentAlive = (%v, %v), want (false, false)", alive, known)
	}
}
