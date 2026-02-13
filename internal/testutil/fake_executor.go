package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

type FakeCall struct {
	Output   string
	ExitCode int
}

type FakeExecutor struct {
	Calls []FakeCall
	idx   int
}

func (f *FakeExecutor) Command(name string, args ...string) *exec.Cmd {
	call := FakeCall{}
	if f.idx < len(f.Calls) {
		call = f.Calls[f.idx]
	}
	f.idx++

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", name)
	cmd.Args = append(cmd.Args, args...)
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		fmt.Sprintf("FAKE_CMD_OUTPUT=%s", call.Output),
		fmt.Sprintf("FAKE_CMD_EXIT_CODE=%d", call.ExitCode),
	)
	return cmd
}

// TestHelperProcess provides a reusable helper process for FakeExecutor-based
// command tests. Add `func TestHelperProcess(t *testing.T) { testutil.TestHelperProcess(t) }`
// in the package under test to enable it.
func TestHelperProcess(t *testing.T) {
	t.Helper()

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
