package executor

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Multiplexer operations on the daemon/worker control path bound external
// commands with a context deadline; the executor must actually enforce it by
// killing the child (observed failure: `zellij attach --create-background`
// hanging forever and freezing the whole worker loop).
func TestCommandFuncExecutorHonorsContextDeadline(t *testing.T) {
	e := NewCommandFuncExecutor(exec.Command)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := e.RunCommand(ctx, "sleep", []string{"30"}, RunOptions{})
	if err == nil {
		t.Fatal("expected deadline error for a command outliving its context")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("kill took %s, want immediate after the 100ms deadline", elapsed)
	}
}

func TestCommandFuncExecutorCompletesWithinDeadline(t *testing.T) {
	e := NewCommandFuncExecutor(exec.Command)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, code, err := e.RunCommand(ctx, "echo", []string{"hello"}, RunOptions{})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(string(out), "hello") {
		t.Fatalf("output = %q, want it to contain hello", out)
	}
}

func TestCommandFuncExecutorNilContext(t *testing.T) {
	e := NewCommandFuncExecutor(exec.Command)

	out, code, err := e.RunCommand(nil, "echo", []string{"ok"}, RunOptions{})
	if err != nil || code != 0 || !strings.Contains(string(out), "ok") {
		t.Fatalf("RunCommand(nil ctx) = (%q, %d, %v), want ok", out, code, err)
	}
}
