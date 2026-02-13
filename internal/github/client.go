package github

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Client wraps gh CLI interactions.
type Client interface {
	Run(args ...string) ([]byte, error)
	RunInDir(dir string, args ...string) ([]byte, error)
	IsAvailable() bool
}

type cliClient struct{}

func NewCLIClient() Client {
	return &cliClient{}
}

func (c *cliClient) IsAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func (c *cliClient) Run(args ...string) ([]byte, error) {
	return c.RunInDir("", args...)
}

func (c *cliClient) RunInDir(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh command failed: %w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}
