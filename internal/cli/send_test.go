package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/orchapi"
)

func TestNewSendCmd(t *testing.T) {
	cmd := newSendCmd()

	if cmd.Use != "send <RUN_REF> <MESSAGE>" {
		t.Errorf("unexpected use: %s", cmd.Use)
	}

	if cmd.Short != "Send a message to a running agent" {
		t.Errorf("unexpected short: %s", cmd.Short)
	}

	// Verify flags
	noEnterFlag := cmd.Flags().Lookup("no-enter")
	if noEnterFlag == nil {
		t.Error("missing --no-enter flag")
	}
}

func TestSendCmdRequiresArgs(t *testing.T) {
	cmd := newSendCmd()

	// Should require exactly 2 args
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("expected error with no args")
	}

	if err := cmd.Args(cmd, []string{"ref"}); err == nil {
		t.Error("expected error with 1 arg")
	}

	if err := cmd.Args(cmd, []string{"ref", "message"}); err != nil {
		t.Errorf("unexpected error with 2 args: %v", err)
	}

	if err := cmd.Args(cmd, []string{"ref", "message", "extra"}); err == nil {
		t.Error("expected error with 3 args")
	}
}

func TestSendCmdDryRunFlag(t *testing.T) {
	cmd := newSendCmd()

	// Verify --dry-run flag exists
	dryRunFlag := cmd.Flags().Lookup("dry-run")
	if dryRunFlag == nil {
		t.Error("missing --dry-run flag")
	}

	if dryRunFlag.DefValue != "false" {
		t.Errorf("expected --dry-run default to be false, got %s", dryRunFlag.DefValue)
	}
}

func TestSendCmdLongDescriptionGuidance(t *testing.T) {
	cmd := newSendCmd()

	if !strings.Contains(cmd.Long, "primary way to interact with waiting runs") {
		t.Fatalf("expected send command help to emphasize waiting-run interaction")
	}
	if !strings.Contains(cmd.Long, "Do NOT use orch restart-from") {
		t.Fatalf("expected send command help to warn against orch restart-from")
	}
}

func TestFormatSendFailureMessageIncludesEscalationPath(t *testing.T) {
	run := &orchapi.Run{
		IssueID:      "orch-451",
		RunID:        "20260226-123456",
		WorktreePath: "/tmp/worktree",
	}

	msg := formatSendFailureMessage(errors.New("daemon error: session not found"), run)

	checks := []string{
		"daemon error: session not found",
		"orch capture orch-451#20260226-123456",
		"orch ps",
		"tmux list-sessions",
		"zellij list-sessions",
		"/tmp/worktree/ORCH_PROMPT.md",
		"tmux send-keys",
		"zellij action write-chars",
		"Do NOT use orch restart-from - the run is likely still alive.",
	}

	for _, check := range checks {
		if !strings.Contains(msg, check) {
			t.Fatalf("expected message to contain %q, got: %s", check, msg)
		}
	}
}
