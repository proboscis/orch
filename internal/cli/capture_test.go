package cli

import (
	"testing"
)

func TestNewCaptureCmd(t *testing.T) {
	cmd := newCaptureCmd()

	if cmd.Use != "capture <RUN_REF>" {
		t.Errorf("unexpected use: %s", cmd.Use)
	}

	if cmd.Short != "Capture output from a running agent" {
		t.Errorf("unexpected short: %s", cmd.Short)
	}

	// Verify flags
	linesFlag := cmd.Flags().Lookup("lines")
	if linesFlag == nil {
		t.Error("missing --lines flag")
	}

	if linesFlag.DefValue != "100" {
		t.Errorf("unexpected default for --lines: %s", linesFlag.DefValue)
	}
}

func TestCaptureCmdRequiresArgs(t *testing.T) {
	cmd := newCaptureCmd()

	// Should require exactly 1 arg
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("expected error with no args")
	}

	if err := cmd.Args(cmd, []string{"ref"}); err != nil {
		t.Errorf("unexpected error with 1 arg: %v", err)
	}

	if err := cmd.Args(cmd, []string{"ref", "extra"}); err == nil {
		t.Error("expected error with 2 args")
	}
}
