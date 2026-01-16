package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client is a client for communicating with the daemon
type Client struct {
	vaultPath string
}

// NewClient creates a new daemon client
func NewClient(vaultPath string) *Client {
	return &Client{vaultPath: vaultPath}
}

// IsAvailable checks if the daemon is available
func (c *Client) IsAvailable() bool {
	return IsDaemonSocketAvailable(c.vaultPath)
}

// ListRuns calls the daemon's list_runs API
func (c *Client) ListRuns(issueID string, status []string, limit int, cursor string) (*ListRunsResponse, error) {
	req := SendRequest{
		Type:      "list_runs",
		IssueID:   issueID,
		Status:    status,
		Limit:     limit,
		Cursor:    cursor,
		VaultPath: c.vaultPath,
	}

	var resp ListRunsResponse
	if err := c.call(&req, &resp); err != nil {
		return nil, err
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// ListIssues calls the daemon's list_issues API
func (c *Client) ListIssues(status []string, limit int, cursor string) (*ListIssuesResponse, error) {
	req := SendRequest{
		Type:      "list_issues",
		Status:    status,
		Limit:     limit,
		Cursor:    cursor,
		VaultPath: c.vaultPath,
	}

	var resp ListIssuesResponse
	if err := c.call(&req, &resp); err != nil {
		return nil, err
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// GetRun calls the daemon's get_run API
func (c *Client) GetRun(issueID, runID string) (*GetRunResponse, error) {
	req := SendRequest{
		Type:      "get_run",
		IssueID:   issueID,
		RunID:     runID,
		VaultPath: c.vaultPath,
	}

	var resp GetRunResponse
	if err := c.call(&req, &resp); err != nil {
		return nil, err
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// GetIssue calls the daemon's get_issue API
func (c *Client) GetIssue(issueID string) (*GetIssueResponse, error) {
	req := SendRequest{
		Type:      "get_issue",
		IssueID:   issueID,
		VaultPath: c.vaultPath,
	}

	var resp GetIssueResponse
	if err := c.call(&req, &resp); err != nil {
		return nil, err
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// StopRun calls the daemon's stop_run API
func (c *Client) StopRun(issueID, runID string, all, force bool) (*StopRunResponse, error) {
	req := SendRequest{
		Type:      "stop_run",
		IssueID:   issueID,
		RunID:     runID,
		All:       all,
		Force:     force,
		VaultPath: c.vaultPath,
	}

	var resp StopRunResponse
	if err := c.call(&req, &resp); err != nil {
		return nil, err
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// ResolveIssue calls the daemon's resolve_issue API
func (c *Client) ResolveIssue(issueID string, force bool) error {
	req := SendRequest{
		Type:      "resolve_issue",
		IssueID:   issueID,
		Force:     force,
		VaultPath: c.vaultPath,
	}

	var resp ResolveIssueResponse
	if err := c.call(&req, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	return nil
}

// CreateIssue calls the daemon's create_issue API
func (c *Client) CreateIssue(issueID, title, summary, body string) (*CreateIssueResponse, error) {
	req := SendRequest{
		Type:      "create_issue",
		IssueID:   issueID,
		Title:     title,
		Summary:   summary,
		Body:      body,
		VaultPath: c.vaultPath,
	}

	var resp CreateIssueResponse
	if err := c.call(&req, &resp); err != nil {
		return nil, err
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// GetAttachInfo calls the daemon's attach_info API
func (c *Client) GetAttachInfo(issueID, runID string) (*AttachInfoResponse, error) {
	req := SendRequest{
		Type:      "attach_info",
		IssueID:   issueID,
		RunID:     runID,
		VaultPath: c.vaultPath,
	}

	var resp AttachInfoResponse
	if err := c.call(&req, &resp); err != nil {
		return nil, err
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// call makes a request to the daemon
func (c *Client) call(req *SendRequest, resp interface{}) error {
	socketPath := SocketFilePath(c.vaultPath)

	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(resp); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	return nil
}
