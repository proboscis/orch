package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

type CommandFunc func(name string, args ...string) *exec.Cmd

type commandFuncExecutor struct {
	command CommandFunc
}

func NewCommandFuncExecutor(command CommandFunc) Executor {
	if command == nil {
		return NewLocalExecutor()
	}
	return &commandFuncExecutor{command: command}
}

func (e *commandFuncExecutor) Run(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	output, _, err := e.RunCommand(ctx, cmd, args, RunOptions{})
	return output, err
}

func (e *commandFuncExecutor) RunWithStatus(ctx context.Context, cmd string, args ...string) ([]byte, int, error) {
	return e.RunCommand(ctx, cmd, args, RunOptions{})
}

// RunCommand honors ctx cancellation/deadline by killing the child process:
// callers (multiplexer operations on the daemon/worker control path) must be
// able to bound external commands — an unbounded child block here freezes the
// whole worker loop (observed with `zellij attach --create-background`
// hanging forever in TTY-less environments).
func (e *commandFuncExecutor) RunCommand(ctx context.Context, cmd string, args []string, opts RunOptions) ([]byte, int, error) {
	c := e.command(cmd, args...)
	if opts.Dir != "" {
		c.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		c.Env = opts.Env
	}
	if opts.Stdin != nil {
		c.Stdin = opts.Stdin
	}

	var combined *bytes.Buffer
	if opts.Stdout != nil || opts.Stderr != nil {
		c.Stdout = opts.Stdout
		c.Stderr = opts.Stderr
	} else {
		combined = &bytes.Buffer{}
		c.Stdout = combined
		c.Stderr = combined
	}

	if err := c.Start(); err != nil {
		return nil, exitCode(err), err
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- c.Wait() }()

	if ctx == nil {
		ctx = context.Background()
	}

	var err error
	select {
	case err = <-waitCh:
	case <-ctx.Done():
		if c.Process != nil {
			_ = c.Process.Kill()
		}
		<-waitCh // reap the killed child
		err = fmt.Errorf("command %q did not finish before its deadline: %w", cmd, ctx.Err())
	}

	var output []byte
	if combined != nil {
		output = combined.Bytes()
	}
	if err != nil {
		return output, exitCode(err), err
	}
	return output, 0, nil
}
