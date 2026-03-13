package executor

import (
	"context"
	"os/exec"
)

type LocalExecutor struct{}

func NewLocalExecutor() Executor {
	return &LocalExecutor{}
}

func (e *LocalExecutor) Run(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	output, _, err := e.RunCommand(ctx, cmd, args, RunOptions{})
	return output, err
}

func (e *LocalExecutor) RunWithStatus(ctx context.Context, cmd string, args ...string) ([]byte, int, error) {
	return e.RunCommand(ctx, cmd, args, RunOptions{})
}

func (e *LocalExecutor) RunCommand(ctx context.Context, cmd string, args []string, opts RunOptions) ([]byte, int, error) {
	c := exec.CommandContext(ctx, cmd, args...)
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

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
