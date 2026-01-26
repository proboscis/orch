package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/s22625/orch/internal/xdg"
)

type Client struct {
	projectRoot string
	issuesRoot  string
	timeout     time.Duration
}

func NewClient(projectRoot string) *Client {
	return &Client{
		projectRoot: projectRoot,
		timeout:     10 * time.Second,
	}
}

func NewClientWithIssuesRoot(projectRoot, issuesRoot string) *Client {
	return &Client{
		projectRoot: projectRoot,
		issuesRoot:  issuesRoot,
		timeout:     10 * time.Second,
	}
}

func (c *Client) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

func (c *Client) IsAvailable() bool {
	return IsDaemonSocketAvailable("") && IsRunning("")
}

func (c *Client) sendRequest(req interface{}) (json.RawMessage, error) {
	// Use global socket path
	socketPath := xdg.SocketPath()

	conn, err := net.DialTimeout("unix", socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(c.timeout))

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	decoder := json.NewDecoder(conn)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return raw, nil
}

func (c *Client) sendRequestWithProjectRoot(req SendRequest) (json.RawMessage, error) {
	if req.ProjectRoot == "" {
		req.ProjectRoot = c.projectRoot
	}
	if req.IssuesRoot == "" && c.issuesRoot != "" {
		req.IssuesRoot = c.issuesRoot
	}
	return c.sendRequest(req)
}

// ListRuns lists runs from the daemon
func (c *Client) ListRuns(issueID string, status []string, limit int, cursor string) (*ListRunsResponse, error) {
	req := SendRequest{
		Type:    "list_runs",
		IssueID: issueID,
		Status:  status,
		Limit:   limit,
		Cursor:  cursor,
	}

	raw, err := c.sendRequestWithProjectRoot(req)
	if err != nil {
		return nil, err
	}

	var resp ListRunsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// ListRunsAll lists runs from all repos (cross-repo view).
func (c *Client) ListRunsAll(status []string, limit int, cursor string) (*ListRunsResponse, error) {
	req := SendRequest{
		Type:   "list_runs",
		Status: status,
		Limit:  limit,
		Cursor: cursor,
		All:    true,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp ListRunsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// ListIssues lists issues from the daemon
func (c *Client) ListIssues(status []string, limit int, cursor string) (*ListIssuesResponse, error) {
	req := SendRequest{
		Type:   "list_issues",
		Status: status,
		Limit:  limit,
		Cursor: cursor,
	}

	raw, err := c.sendRequestWithProjectRoot(req)
	if err != nil {
		return nil, err
	}

	var resp ListIssuesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// GetRun gets a run from the daemon
func (c *Client) GetRun(issueID, runID string) (*GetRunResponse, error) {
	req := SendRequest{
		Type:    "get_run",
		IssueID: issueID,
		RunID:   runID,
	}

	raw, err := c.sendRequestWithProjectRoot(req)
	if err != nil {
		return nil, err
	}

	var resp GetRunResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// GetIssue gets an issue from the daemon
func (c *Client) GetIssue(issueID string) (*GetIssueResponse, error) {
	req := SendRequest{
		Type:    "get_issue",
		IssueID: issueID,
	}

	raw, err := c.sendRequestWithProjectRoot(req)
	if err != nil {
		return nil, err
	}

	var resp GetIssueResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// StopRunRequest is the request for stop_run
type StopRunRequest struct {
	Type        string `json:"type"`
	IssueID     string `json:"issue_id"`
	RunID       string `json:"run_id,omitempty"`
	Force       bool   `json:"force,omitempty"`
	ProjectRoot string `json:"project_root,omitempty"`
	IssuesRoot  string `json:"issues_root,omitempty"`
}

// StopRunResponse is the response for stop_run
type StopRunResponse struct {
	OK           bool     `json:"ok"`
	Error        string   `json:"error,omitempty"`
	StoppedRuns  []string `json:"stopped_runs,omitempty"`
	StoppedCount int      `json:"stopped_count"`
}

// StopRun stops a run via the daemon
func (c *Client) StopRun(issueID, runID string, force bool) (*StopRunResponse, error) {
	req := StopRunRequest{
		Type:        "stop_run",
		IssueID:     issueID,
		RunID:       runID,
		Force:       force,
		ProjectRoot: c.projectRoot,
		IssuesRoot:  c.issuesRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp StopRunResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// ResolveIssueRequest is the request for resolve_issue
type ResolveIssueRequest struct {
	Type        string `json:"type"`
	IssueID     string `json:"issue_id"`
	Force       bool   `json:"force,omitempty"`
	ProjectRoot string `json:"project_root,omitempty"`
	IssuesRoot  string `json:"issues_root,omitempty"`
}

// ResolveIssueResponse is the response for resolve_issue
type ResolveIssueResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	IssueID string `json:"issue_id,omitempty"`
}

// ResolveIssue marks an issue as resolved via the daemon
func (c *Client) ResolveIssue(issueID string, force bool) (*ResolveIssueResponse, error) {
	req := ResolveIssueRequest{
		Type:        "resolve_issue",
		IssueID:     issueID,
		Force:       force,
		ProjectRoot: c.projectRoot,
		IssuesRoot:  c.issuesRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp ResolveIssueResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

type AppendEventRequest struct {
	Type        string            `json:"type"`
	IssueID     string            `json:"issue_id"`
	RunID       string            `json:"run_id"`
	EventType   string            `json:"event_type"`
	EventName   string            `json:"event_name"`
	EventAttrs  map[string]string `json:"event_attrs,omitempty"`
	EventSource string            `json:"event_source"`
	ProjectRoot string            `json:"project_root,omitempty"`
	IssuesRoot  string            `json:"issues_root,omitempty"`
}

type AppendEventResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (c *Client) AppendEvent(issueID, runID, eventType, eventName string, attrs map[string]string, source string) (*AppendEventResponse, error) {
	req := AppendEventRequest{
		Type:        "append_event",
		IssueID:     issueID,
		RunID:       runID,
		EventType:   eventType,
		EventName:   eventName,
		EventAttrs:  attrs,
		EventSource: source,
		ProjectRoot: c.projectRoot,
		IssuesRoot:  c.issuesRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp AppendEventResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resp, nil
}

func (c *Client) AppendStatusEvent(issueID, runID string, status string, source string) error {
	resp, err := c.AppendEvent(issueID, runID, "status", status, nil, source)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}
	return nil
}

func (c *Client) AppendArtifactEvent(issueID, runID, artifactName string, attrs map[string]string, source string) error {
	resp, err := c.AppendEvent(issueID, runID, "artifact", artifactName, attrs, source)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}
	return nil
}

// CreateIssueRequest is the request for create_issue
type CreateIssueRequest struct {
	Type        string `json:"type"`
	IssueID     string `json:"issue_id"`
	Title       string `json:"title,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Body        string `json:"body,omitempty"`
	ProjectRoot string `json:"project_root,omitempty"`
	IssuesRoot  string `json:"issues_root,omitempty"`
}

// CreateIssueResponse is the response for create_issue
type CreateIssueResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	IssueID string `json:"issue_id,omitempty"`
	Path    string `json:"path,omitempty"`
}

// CreateIssue creates an issue via the daemon
func (c *Client) CreateIssue(issueID, title, summary, body string) (*CreateIssueResponse, error) {
	req := CreateIssueRequest{
		Type:        "create_issue",
		IssueID:     issueID,
		Title:       title,
		Summary:     summary,
		Body:        body,
		ProjectRoot: c.projectRoot,
		IssuesRoot:  c.issuesRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp CreateIssueResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// CloseIssueRequest is the request for close_issue
type CloseIssueRequest struct {
	Type        string `json:"type"`
	IssueID     string `json:"issue_id"`
	Comment     string `json:"comment,omitempty"`
	ProjectRoot string `json:"project_root,omitempty"`
	IssuesRoot  string `json:"issues_root,omitempty"`
}

// CloseIssueResponse is the response for close_issue
type CloseIssueResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	IssueID string `json:"issue_id,omitempty"`
}

// CloseIssue closes an issue via the daemon
func (c *Client) CloseIssue(issueID, comment string) (*CloseIssueResponse, error) {
	req := CloseIssueRequest{
		Type:        "close_issue",
		IssueID:     issueID,
		Comment:     comment,
		ProjectRoot: c.projectRoot,
		IssuesRoot:  c.issuesRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp CloseIssueResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// GetAttachInfoRequest is the request for get_attach_info
type GetAttachInfoRequest struct {
	Type        string `json:"type"`
	IssueID     string `json:"issue_id"`
	RunID       string `json:"run_id,omitempty"`
	ShortID     string `json:"short_id,omitempty"`
	ProjectRoot string `json:"project_root,omitempty"`
	IssuesRoot  string `json:"issues_root,omitempty"`
}

// GetAttachInfoResponse is the response for get_attach_info
type GetAttachInfoResponse struct {
	OK                bool   `json:"ok"`
	Error             string `json:"error,omitempty"`
	IssueID           string `json:"issue_id,omitempty"`
	RunID             string `json:"run_id,omitempty"`
	Agent             string `json:"agent,omitempty"`
	TmuxSession       string `json:"tmux_session,omitempty"`
	Multiplexer       string `json:"multiplexer,omitempty"`
	WorktreePath      string `json:"worktree_path,omitempty"`
	ServerPort        int    `json:"server_port,omitempty"`
	OpenCodeSessionID string `json:"opencode_session_id,omitempty"`
	Branch            string `json:"branch,omitempty"`
}

// GetAttachInfo gets attach information for a run
func (c *Client) GetAttachInfo(issueID, runID, shortID string) (*GetAttachInfoResponse, error) {
	req := GetAttachInfoRequest{
		Type:        "get_attach_info",
		IssueID:     issueID,
		RunID:       runID,
		ShortID:     shortID,
		ProjectRoot: c.projectRoot,
		IssuesRoot:  c.issuesRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp GetAttachInfoResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// GetRunByShortIDRequest is the request for get_run_by_short_id
type GetRunByShortIDRequest struct {
	Type        string `json:"type"`
	ShortID     string `json:"short_id"`
	ProjectRoot string `json:"project_root,omitempty"`
	IssuesRoot  string `json:"issues_root,omitempty"`
}

// GetRunByShortID gets a run by short ID via the daemon
func (c *Client) GetRunByShortID(shortID string) (*GetRunResponse, error) {
	req := GetRunByShortIDRequest{
		Type:        "get_run_by_short_id",
		ShortID:     shortID,
		ProjectRoot: c.projectRoot,
		IssuesRoot:  c.issuesRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp GetRunResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

func (c *Client) RegisterMonitor(pid int, monitorType, view, project, tmuxSession string) (*RegisterMonitorResponse, error) {
	req := SendRequest{
		Type:        "register_monitor",
		Limit:       pid,
		Title:       monitorType,
		Summary:     view,
		ProjectRoot: project,
		Body:        tmuxSession,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp RegisterMonitorResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

func (c *Client) UnregisterMonitor(monitorID string) error {
	req := SendRequest{
		Type:    "unregister_monitor",
		ShortID: monitorID,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	var resp SendResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	return nil
}

func (c *Client) MonitorHeartbeat(monitorID string) error {
	req := SendRequest{
		Type:    "monitor_heartbeat",
		ShortID: monitorID,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	var resp HeartbeatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	return nil
}

func (c *Client) ListMonitors(projectRoot string, all bool) (*ListMonitorsResponse, error) {
	req := SendRequest{
		Type:        "list_monitors",
		ProjectRoot: projectRoot,
		Force:       all,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp ListMonitorsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

func (c *Client) KillMonitor(monitorID string, killAll bool, global bool, projectRoot string) (*KillMonitorResponse, error) {
	cursorVal := ""
	if global {
		cursorVal = "global"
	}
	req := SendRequest{
		Type:        "kill_monitor",
		ShortID:     monitorID,
		Force:       killAll,
		Cursor:      cursorVal,
		ProjectRoot: projectRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp KillMonitorResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// GetOpenCodeServer requests or retrieves an opencode server for a project.
func (c *Client) GetOpenCodeServer(projectRoot string) (*GetOpenCodeServerResponse, error) {
	req := SendRequest{
		Type:        "ensure_opencode_server",
		ProjectRoot: projectRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp GetOpenCodeServerResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}

// RegisterRepo registers a project with the daemon.
func (c *Client) RegisterRepo(projectRoot string) (string, error) {
	req := SendRequest{
		Type:        "register_repo",
		ProjectRoot: projectRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	var resp struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error,omitempty"`
		RepoID string `json:"repo_id,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return "", fmt.Errorf("daemon error: %s", resp.Error)
	}

	return resp.RepoID, nil
}

// ListRepos returns list of registered repos from the daemon.
func (c *Client) ListRepos() ([]map[string]string, error) {
	req := SendRequest{
		Type: "list_repos",
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp struct {
		OK    bool                `json:"ok"`
		Error string              `json:"error,omitempty"`
		Repos []map[string]string `json:"repos,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return resp.Repos, nil
}

// GetControlAgentLaunchRequest is the request for get_control_agent_launch
type GetControlAgentLaunchRequest struct {
	Type        string `json:"type"`
	ProjectRoot string `json:"project_root"`
	AgentType   string `json:"agent_type,omitempty"`
	NewSession  bool   `json:"force,omitempty"` // Using "force" to match SendRequest.Force field
	IssuesRoot  string `json:"issues_root,omitempty"`
}

// GetControlAgentLaunchResponse is the response for get_control_agent_launch
type GetControlAgentLaunchResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Command    string `json:"command,omitempty"`
	PromptFile string `json:"prompt_file,omitempty"`
	Port       int    `json:"port,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Agent      string `json:"agent,omitempty"`
}

// GetControlAgentLaunch gets the launch command and configuration for the control agent.
// It writes the control prompt file, resolves agent configuration, and returns
// a ready-to-execute command.
func (c *Client) GetControlAgentLaunch(projectRoot, agentType string, newSession bool) (*GetControlAgentLaunchResponse, error) {
	req := GetControlAgentLaunchRequest{
		Type:        "get_control_agent_launch",
		ProjectRoot: projectRoot,
		AgentType:   agentType,
		NewSession:  newSession,
		IssuesRoot:  c.issuesRoot,
	}

	raw, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	var resp GetControlAgentLaunchResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &resp, nil
}
