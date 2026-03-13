package executor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type helperPayload struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

func helperCommandContext(_ context.Context, name string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestSSHExecutorHelperProcess", "--", name}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_SSH_EXECUTOR_HELPER=1")
	return cmd
}

func TestSSHExecutorHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_SSH_EXECUTOR_HELPER") != "1" {
		return
	}

	args := os.Args
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == -1 || sep+1 >= len(args) {
		os.Exit(2)
	}

	payload := helperPayload{
		Name: args[sep+1],
		Args: args[sep+2:],
	}
	_ = json.NewEncoder(os.Stdout).Encode(payload)
	os.Exit(0)
}

func TestSSHExecutorRunBuildsControlMasterCommand(t *testing.T) {
	e := NewSSHExecutor("mac")
	e.SocketDir = filepath.Join(t.TempDir(), "sockets")
	e.commandContext = helperCommandContext

	output, err := e.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload helperPayload
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("decode helper payload: %v\noutput=%q", err, string(output))
	}

	if payload.Name != "ssh" {
		t.Fatalf("command name = %q, want ssh", payload.Name)
	}
	if len(payload.Args) < 6 {
		t.Fatalf("ssh args too short: %v", payload.Args)
	}

	if payload.Args[0] != "-o" || payload.Args[1] != "ControlMaster=auto" {
		t.Fatalf("missing control master args: %v", payload.Args)
	}
	if payload.Args[2] != "-o" || payload.Args[3] != "ControlPath="+filepath.Join(e.SocketDir, "%C") {
		t.Fatalf("unexpected control path args: %v", payload.Args)
	}
	if payload.Args[4] != "-o" || payload.Args[5] != "ControlPersist=300" {
		t.Fatalf("unexpected control persist args: %v", payload.Args)
	}

	hostArg := payload.Args[len(payload.Args)-2]
	if hostArg != "mac" {
		t.Fatalf("host arg = %q, want mac", hostArg)
	}

	remoteCmd := payload.Args[len(payload.Args)-1]
	if !strings.Contains(remoteCmd, "'echo' 'hello'") {
		t.Fatalf("remote command = %q, want quoted echo", remoteCmd)
	}

	if _, err := os.Stat(e.SocketDir); err != nil {
		t.Fatalf("socket dir missing: %v", err)
	}
}

func TestSSHExecutorRunCommandIncludesDirAndEnv(t *testing.T) {
	e := NewSSHExecutor("builder")
	e.SocketDir = filepath.Join(t.TempDir(), "sockets")
	e.commandContext = helperCommandContext

	output, _, err := e.RunCommand(context.Background(), "tmux", []string{"new-session", "-s", "my session"}, RunOptions{
		Dir: "/tmp/work dir",
		Env: []string{"A=1", "B=two words"},
	})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}

	var payload helperPayload
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("decode helper payload: %v\noutput=%q", err, string(output))
	}

	remoteCmd := payload.Args[len(payload.Args)-1]
	if !strings.HasPrefix(remoteCmd, "cd '/tmp/work dir' && ") {
		t.Fatalf("remote command missing cd prefix: %q", remoteCmd)
	}
	if !strings.Contains(remoteCmd, "'env' 'A=1' 'B=two words' 'tmux' 'new-session' '-s' 'my session'") {
		t.Fatalf("remote command missing env+tmux invocation: %q", remoteCmd)
	}
}

func TestSSHExecutorRequiresHost(t *testing.T) {
	e := NewSSHExecutor("")
	_, _, err := e.RunCommand(context.Background(), "echo", []string{"hello"}, RunOptions{})
	if err == nil {
		t.Fatalf("RunCommand() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ssh host is required") {
		t.Fatalf("error = %v, want host required", err)
	}
}
