package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type SSHExecutor struct {
	Host           string
	SocketDir      string
	ControlPersist string
	commandContext func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func NewSSHExecutor(host string) *SSHExecutor {
	return &SSHExecutor{
		Host:           strings.TrimSpace(host),
		SocketDir:      defaultSSHSocketDir(),
		ControlPersist: "300",
		commandContext: exec.CommandContext,
	}
}

func defaultSSHSocketDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return filepath.Join(home, ".ssh", "orch-sockets")
}

func (e *SSHExecutor) Run(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	output, _, err := e.RunCommand(ctx, cmd, args, RunOptions{})
	return output, err
}

func (e *SSHExecutor) RunWithStatus(ctx context.Context, cmd string, args ...string) ([]byte, int, error) {
	return e.RunCommand(ctx, cmd, args, RunOptions{})
}

func (e *SSHExecutor) RunCommand(ctx context.Context, cmd string, args []string, opts RunOptions) ([]byte, int, error) {
	if strings.TrimSpace(e.Host) == "" {
		return nil, -1, fmt.Errorf("ssh host is required")
	}
	if err := e.ensureSocketDir(); err != nil {
		return nil, -1, err
	}

	remoteCommand := buildRemoteCommand(cmd, args, opts)
	sshArgs := e.sshArgs(remoteCommand)

	runner := e.commandContext
	if runner == nil {
		runner = exec.CommandContext
	}

	c := runner(ctx, "ssh", sshArgs...)
	if opts.Stdin != nil {
		c.Stdin = opts.Stdin
	}
	if opts.Stdout != nil {
		c.Stdout = opts.Stdout
	}
	if opts.Stderr != nil {
		c.Stderr = opts.Stderr
	}

	if opts.Stdout != nil || opts.Stderr != nil {
		err := c.Run()
		return nil, exitCode(err), err
	}

	output, err := c.CombinedOutput()
	return output, exitCode(err), err
}

func (e *SSHExecutor) ensureSocketDir() error {
	socketDir := strings.TrimSpace(e.SocketDir)
	if socketDir == "" {
		socketDir = defaultSSHSocketDir()
		e.SocketDir = socketDir
	}
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return fmt.Errorf("create ssh socket directory %q: %w", socketDir, err)
	}
	return nil
}

func (e *SSHExecutor) sshArgs(remoteCommand string) []string {
	controlPersist := strings.TrimSpace(e.ControlPersist)
	if controlPersist == "" {
		controlPersist = "300"
	}
	controlPath := filepath.Join(e.SocketDir, "%C")
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + controlPath,
		"-o", "ControlPersist=" + controlPersist,
		e.Host,
		remoteCommand,
	}
}

func buildRemoteCommand(cmd string, args []string, opts RunOptions) string {
	commandTokens := make([]string, 0, 2+len(opts.Env)+len(args))
	if len(opts.Env) > 0 {
		commandTokens = append(commandTokens, "env")
		for _, kv := range opts.Env {
			kv = strings.TrimSpace(kv)
			if kv == "" {
				continue
			}
			commandTokens = append(commandTokens, kv)
		}
	}
	commandTokens = append(commandTokens, cmd)
	commandTokens = append(commandTokens, args...)

	invocation := quoteTokens(commandTokens)
	if strings.TrimSpace(opts.Dir) == "" {
		return invocation
	}
	return "cd " + shellQuote(opts.Dir) + " && " + invocation
}

func quoteTokens(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, token := range tokens {
		quoted = append(quoted, shellQuote(token))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
