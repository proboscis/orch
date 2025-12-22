package cli

import (
	"testing"
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
