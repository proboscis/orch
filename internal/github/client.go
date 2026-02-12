package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

type PR struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	State  string `json:"state"`
}

// Client defines GitHub CLI boundaries used by backend and daemon layers.
type Client interface {
	IsAvailable() bool
	Run(args ...string) ([]byte, error)
	LookupPRByHead(repoRoot, branch string) (*PR, error)
	LookupPRByURL(prURL string) (*PR, error)
}

type CLIClient struct{}

func NewCLIClient() Client {
	return &CLIClient{}
}

func (c *CLIClient) IsAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func (c *CLIClient) Run(args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh command failed: %w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func (c *CLIClient) LookupPRByHead(repoRoot, branch string) (*PR, error) {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--state", "all", "--json", "url,number,state", "--limit", "1")
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var prs []PR
	if err := json.Unmarshal(output, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

func (c *CLIClient) LookupPRByURL(prURL string) (*PR, error) {
	cmd := exec.Command("gh", "pr", "view", prURL, "--json", "url,number,state")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var pr PR
	if err := json.Unmarshal(output, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}
