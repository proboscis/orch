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

func TestZellijMultiplexer_SwitchClient_SameSession(t *testing.T) {
	// When inside a Zellij session and switching to the same session
	os.Setenv("ZELLIJ", "1")
	os.Setenv("ZELLIJ_SESSION_NAME", "my-session")
	t.Cleanup(func() {
		os.Unsetenv("ZELLIJ")
		os.Unsetenv("ZELLIJ_SESSION_NAME")
	})

	zm := NewZellijMultiplexer()
	// Switching to the same session should be a no-op
	err := zm.SwitchClient("my-session")
	if err != nil {
		t.Fatalf("SwitchClient same session: unexpected error: %v", err)
	}
}

func TestZellijMultiplexer_SwitchClient_CrossSession(t *testing.T) {
	// When inside a Zellij session and trying to switch to a different session
	os.Setenv("ZELLIJ", "1")
	os.Setenv("ZELLIJ_SESSION_NAME", "current-session")
	t.Cleanup(func() {
		os.Unsetenv("ZELLIJ")
		os.Unsetenv("ZELLIJ_SESSION_NAME")
	})

	zm := NewZellijMultiplexer()
	// Switching to a different session should return ErrCrossSessionAttach
	err := zm.SwitchClient("other-session")
	if err == nil {
		t.Fatal("expected error when switching to different session from inside Zellij")
	}
	if !errors.Is(err, ErrCrossSessionAttach) {
		t.Fatalf("expected ErrCrossSessionAttach, got: %v", err)
	}
}

func TestZellijMultiplexer_SwitchClient_NotInSession(t *testing.T) {
	// When not inside a Zellij session, should try to attach normally
	os.Unsetenv("ZELLIJ")
	os.Unsetenv("ZELLIJ_SESSION_NAME")

	exec := &fakeExecutor{calls: []fakeCall{{exitCode: 0}}}
	orig := execCommand
	execCommand = exec.Command
	t.Cleanup(func() { execCommand = orig })

	zm := NewZellijMultiplexer()
	err := zm.SwitchClient("target-session")
	if err != nil {
		t.Fatalf("SwitchClient not in session: unexpected error: %v", err)
	}

	if len(exec.recorded) != 1 {
		t.Fatalf("expected 1 command, got %d", len(exec.recorded))
	}
	call := exec.recorded[0]
	if call.name != "zellij" {
		t.Fatalf("expected command 'zellij', got: %s", call.name)
	}
	if len(call.args) < 1 || call.args[0] != "attach" {
		t.Fatalf("expected 'zellij attach', got args: %v", call.args)
	}
}

func TestZellijMultiplexer_SetOption_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	// SetOption returns ErrUnsupported for zellij (no equivalent)
	err := zm.SetOption("sess", "option", "value")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
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

func TestZellijMultiplexer_ListPaneCommands_Unsupported(t *testing.T) {
	zm := NewZellijMultiplexer()
	// ListPaneCommands returns ErrUnsupported for zellij (no equivalent)
	_, err := zm.ListPaneCommands()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got: %v", err)
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
	// Zellij cannot reliably detect agent liveness, so always returns (false, false) = unknown
	zm := NewZellijMultiplexer()
	alive, known := zm.AgentAlive("test-session", nil)
	if alive || known {
		t.Fatalf("AgentAlive = (%v, %v), want (false, false) for unknown", alive, known)
	}
}

func TestZellijMultiplexer_AgentAlive_Missing(t *testing.T) {
	// Zellij always returns unknown, regardless of session existence
	zm := NewZellijMultiplexer()
	alive, known := zm.AgentAlive("nonexistent-session", nil)
	if alive || known {
		t.Fatalf("AgentAlive = (%v, %v), want (false, false) for unknown", alive, known)
	}
}

func TestShortenSessionName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantSame bool
	}{
		{
			name:     "short name unchanged",
			input:    "short",
			wantLen:  5,
			wantSame: true,
		},
		{
			name:     "exactly at limit unchanged",
			input:    "1234567890123456789012345",
			wantLen:  25,
			wantSame: true,
		},
		{
			name:     "one over limit gets shortened",
			input:    "12345678901234567890123456",
			wantLen:  25,
			wantSame: false,
		},
		{
			name:     "long name shortened to limit",
			input:    "run-dummy-test-001-20260123-022021",
			wantLen:  25,
			wantSame: false,
		},
		{
			name:     "very long name shortened to limit",
			input:    "this-is-a-very-very-very-long-session-name-that-exceeds-the-limit",
			wantLen:  25,
			wantSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortenSessionName(tt.input)
			if len(result) != tt.wantLen {
				t.Errorf("shortenSessionName(%q) len = %d, want %d; result = %q",
					tt.input, len(result), tt.wantLen, result)
			}
			if tt.wantSame && result != tt.input {
				t.Errorf("shortenSessionName(%q) = %q, want unchanged", tt.input, result)
			}
			if !tt.wantSame && result == tt.input {
				t.Errorf("shortenSessionName(%q) unchanged, want shortened", tt.input)
			}
		})
	}
}

func TestShortenSessionName_Deterministic(t *testing.T) {
	input := "run-dummy-test-001-20260123-022021"
	result1 := shortenSessionName(input)
	result2 := shortenSessionName(input)
	if result1 != result2 {
		t.Errorf("shortenSessionName not deterministic: %q != %q", result1, result2)
	}
}

func TestShortenSessionName_UniqueForDifferentInputs(t *testing.T) {
	input1 := "run-dummy-test-001-20260123-022021"
	input2 := "run-dummy-test-001-20260123-022022"
	result1 := shortenSessionName(input1)
	result2 := shortenSessionName(input2)
	if result1 == result2 {
		t.Errorf("different inputs produced same output: %q", result1)
	}
}
