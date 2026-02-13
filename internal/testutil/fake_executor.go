package testutil

import (
	"fmt"
	"os"
	"os/exec"
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
