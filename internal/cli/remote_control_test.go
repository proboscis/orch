package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/orchapi"
)

func TestCaptureRemoteFromInfoTmux(t *testing.T) {
	orig := runSSHOutputCommand
	t.Cleanup(func() { runSSHOutputCommand = orig })

	var gotArgs []string
	runSSHOutputCommand = func(args []string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		return []byte("line1\nline2\n"), nil
	}

	result, err := captureRemoteFromInfo(&orchapi.AttachInfo{
		IssueID:     "orch-1",
		RunID:       "run-1",
		SessionName: "sess-1",
		TargetHost:  "user@mac",
		Multiplexer: orchapi.MultiplexerTmux,
	}, 25)
	if err != nil {
		t.Fatalf("captureRemoteFromInfo() error = %v", err)
	}
	if result.Source != "tmux" || result.Content != "line1\nline2\n" {
		t.Fatalf("unexpected capture result: %+v", result)
	}

	want := []string{"-T", "user@mac", "tmux", "capture-pane", "-t", "sess-1", "-p", "-S", "-25"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("ssh args = %v, want %v", gotArgs, want)
	}
}

func TestCaptureRemoteFromInfoOpenCode(t *testing.T) {
	orig := runSSHOutputCommand
	t.Cleanup(func() { runSSHOutputCommand = orig })

	var gotArgs []string
	runSSHOutputCommand = func(args []string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		return []byte("--- [ASSISTANT] ---\nremote opencode"), nil
	}

	result, err := captureRemoteFromInfo(&orchapi.AttachInfo{
		IssueID:           "orch-2",
		RunID:             "run-2",
		TargetHost:        "zeus",
		Agent:             "opencode",
		ServerPort:        4111,
		OpenCodeSessionID: "ses-remote",
		WorktreePath:      "/remote/worktree",
	}, 50)
	if err != nil {
		t.Fatalf("captureRemoteFromInfo() error = %v", err)
	}
	if !strings.Contains(result.Content, "remote opencode") {
		t.Fatalf("unexpected capture content: %q", result.Content)
	}
	if result.Source != "opencode" {
		t.Fatalf("source = %q, want opencode", result.Source)
	}
	if gotArgs[0] != "-T" || gotArgs[1] != "zeus" {
		t.Fatalf("unexpected ssh args: %v", gotArgs)
	}
	if len(gotArgs) < 5 || !strings.Contains(gotArgs[4], "python3 - <<'PY'") {
		t.Fatalf("expected inline python helper in ssh args, got %v", gotArgs)
	}
	if !strings.Contains(gotArgs[4], "time.sleep(0.15)") {
		t.Fatalf("expected retrying python helper in ssh args, got %v", gotArgs)
	}
	if !strings.Contains(gotArgs[4], "max_lines") || !strings.Contains(gotArgs[4], "50") {
		t.Fatalf("expected remote formatter to receive line limit, got args=%v", gotArgs)
	}
}

func TestSendRemoteFromInfoTmux(t *testing.T) {
	orig := runSSHOutputCommand
	t.Cleanup(func() { runSSHOutputCommand = orig })

	var calls [][]string
	runSSHOutputCommand = func(args []string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	}

	err := sendRemoteFromInfo(&orchapi.AttachInfo{
		IssueID:     "orch-3",
		RunID:       "run-3",
		SessionName: "sess-3",
		TargetHost:  "user@mac",
		Agent:       "codex",
		Multiplexer: orchapi.MultiplexerTmux,
	}, "please continue", false)
	if err != nil {
		t.Fatalf("sendRemoteFromInfo() error = %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("ssh call count = %d, want 2", len(calls))
	}
	wantFirst := []string{"-T", "user@mac", "tmux", "send-keys", "-t", "sess-3", "-l", "please continue"}
	wantSecond := []string{"-T", "user@mac", "tmux", "send-keys", "-t", "sess-3", "Enter"}
	if !reflect.DeepEqual(calls[0], wantFirst) {
		t.Fatalf("first ssh args = %v, want %v", calls[0], wantFirst)
	}
	if !reflect.DeepEqual(calls[1], wantSecond) {
		t.Fatalf("second ssh args = %v, want %v", calls[1], wantSecond)
	}
}

func TestSendRemoteFromInfoOpenCode(t *testing.T) {
	orig := runSSHOutputCommand
	t.Cleanup(func() { runSSHOutputCommand = orig })

	var gotArgs []string
	runSSHOutputCommand = func(args []string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		return nil, nil
	}

	err := sendRemoteFromInfo(&orchapi.AttachInfo{
		IssueID:           "orch-4",
		RunID:             "run-4",
		TargetHost:        "zeus",
		Agent:             "opencode",
		ServerPort:        4123,
		OpenCodeSessionID: "ses-4",
		WorktreePath:      "/srv/repo",
	}, `please "continue"`, false)
	if err != nil {
		t.Fatalf("sendRemoteFromInfo() error = %v", err)
	}

	if len(gotArgs) < 5 {
		t.Fatalf("unexpected ssh args: %v", gotArgs)
	}
	if gotArgs[0] != "-T" || gotArgs[1] != "zeus" || gotArgs[2] != "sh" || gotArgs[3] != "-lc" {
		t.Fatalf("unexpected ssh args prefix: %v", gotArgs)
	}
	script := gotArgs[4]
	if !strings.Contains(script, "ORCH_REMOTE_PY_ARGS=") || !strings.Contains(script, "python3 - <<'PY'") {
		t.Fatalf("expected python remote helper, got: %s", script)
	}
	if !strings.Contains(script, "4123") || !strings.Contains(script, "ses-4") || !strings.Contains(script, "/srv/repo") {
		t.Fatalf("missing session/worktree details in script: %s", script)
	}
	if !strings.Contains(script, `sys.argv = ["-"] + json.loads(os.environ["ORCH_REMOTE_PY_ARGS"])`) || !strings.Contains(script, "method=\"POST\"") || !strings.Contains(script, "Content-Type") {
		t.Fatalf("expected inline python helper script, got %s", script)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
