package executor

import (
	"context"
	"io"
)

type RunOptions struct {
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type Executor interface {
	Run(ctx context.Context, cmd string, args ...string) ([]byte, error)
	RunWithStatus(ctx context.Context, cmd string, args ...string) ([]byte, int, error)
	RunCommand(ctx context.Context, cmd string, args []string, opts RunOptions) ([]byte, int, error)
}
