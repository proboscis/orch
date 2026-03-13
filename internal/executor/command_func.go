package executor

import (
	"context"
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

func (e *commandFuncExecutor) RunCommand(_ context.Context, cmd string, args []string, opts RunOptions) ([]byte, int, error) {
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
