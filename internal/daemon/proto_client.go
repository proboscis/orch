package daemon

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/xdg"
	"google.golang.org/protobuf/proto"
)

type ProtoClient struct {
	projectRoot string
	issuesRoot  string
	timeout     time.Duration
}

func NewProtoClient(projectRoot string) *ProtoClient {
	return &ProtoClient{
		projectRoot: projectRoot,
		timeout:     10 * time.Second,
	}
}

func NewProtoClientWithIssuesRoot(projectRoot, issuesRoot string) *ProtoClient {
	return &ProtoClient{
		projectRoot: projectRoot,
		issuesRoot:  issuesRoot,
		timeout:     10 * time.Second,
	}
}

func (c *ProtoClient) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

func (c *ProtoClient) IsAvailable() bool {
	return IsDaemonSocketAvailable("") && IsRunning("")
}

func (c *ProtoClient) sendRequest(req *orchpb.Request) (*orchpb.Response, error) {
	socketPath := xdg.SocketPath()

	conn, err := net.DialTimeout("unix", socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(c.timeout))

	data, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := conn.Write(lenBuf); err != nil {
		return nil, fmt.Errorf("failed to write length: %w", err)
	}
	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write data: %w", err)
	}

	respLenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, respLenBuf); err != nil {
		return nil, fmt.Errorf("failed to read response length: %w", err)
	}
	respLen := binary.BigEndian.Uint32(respLenBuf)

	respData := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respData); err != nil {
		return nil, fmt.Errorf("failed to read response data: %w", err)
	}

	var resp orchpb.Response
	if err := proto.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

func (c *ProtoClient) Ping() error {
	req := &orchpb.Request{
		Request: &orchpb.Request_Ping{Ping: &orchpb.PingRequest{}},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Ok {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	return nil
}

func (c *ProtoClient) ListRuns(issueID string, status []string, limit int, cursor string) (*ListRunsResponse, error) {
	protoStatuses := make([]orchpb.RunStatus, 0, len(status))
	for _, s := range status {
		protoStatuses = append(protoStatuses, stringToProtoRunStatus(s))
	}

	req := &orchpb.Request{
		Request: &orchpb.Request_ListRuns{
			ListRuns: &orchpb.ListRunsRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				Status:     protoStatuses,
				Limit:      int32(limit),
				Cursor:     cursor,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	listResp := resp.GetListRuns()
	if listResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	runs := make([]*RunSummary, len(listResp.Runs))
	for i, r := range listResp.Runs {
		runs[i] = protoRunToSummary(r)
	}

	var nextCursor *string
	if listResp.NextCursor != "" {
		nextCursor = &listResp.NextCursor
	}

	return &ListRunsResponse{
		OK:         true,
		Runs:       runs,
		NextCursor: nextCursor,
		Total:      int(listResp.Total),
	}, nil
}

func (c *ProtoClient) ListRunsAll(status []string, limit int, cursor string) (*ListRunsResponse, error) {
	return c.ListRuns("", status, limit, cursor)
}

func (c *ProtoClient) GetRun(issueID, runID string) (*GetRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetRun{
			GetRun: &orchpb.GetRunRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	getResp := resp.GetGetRun()
	if getResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &GetRunResponse{
		OK:  true,
		Run: protoRunToFull(getResp.Run, getResp.Events),
	}, nil
}

func (c *ProtoClient) GetRunByShortID(shortID string) (*GetRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetRunByShortId{
			GetRunByShortId: &orchpb.GetRunByShortIDRequest{
				IssuesRoot: c.issuesRoot,
				ShortId:    shortID,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	getResp := resp.GetGetRunByShortId()
	if getResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &GetRunResponse{
		OK:  true,
		Run: protoRunToFull(getResp.Run, getResp.Events),
	}, nil
}

func (c *ProtoClient) ListIssues(status []string, limit int, cursor string) (*ListIssuesResponse, error) {
	protoStatuses := make([]orchpb.IssueStatus, 0, len(status))
	for _, s := range status {
		protoStatuses = append(protoStatuses, stringToProtoIssueStatus(s))
	}

	req := &orchpb.Request{
		Request: &orchpb.Request_ListIssues{
			ListIssues: &orchpb.ListIssuesRequest{
				IssuesRoot: c.issuesRoot,
				Status:     protoStatuses,
				Limit:      int32(limit),
				Cursor:     cursor,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	listResp := resp.GetListIssues()
	if listResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	issues := make([]*IssueSummary, len(listResp.Issues))
	for i, iss := range listResp.Issues {
		issues[i] = protoIssueToSummary(iss)
	}

	var nextCursor *string
	if listResp.NextCursor != "" {
		nextCursor = &listResp.NextCursor
	}

	return &ListIssuesResponse{
		OK:         true,
		Issues:     issues,
		NextCursor: nextCursor,
		Total:      int(listResp.Total),
	}, nil
}

func (c *ProtoClient) GetIssue(issueID string) (*GetIssueResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetIssue{
			GetIssue: &orchpb.GetIssueRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	getResp := resp.GetGetIssue()
	if getResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &GetIssueResponse{
		OK:    true,
		Issue: protoIssueToFull(getResp.Issue),
	}, nil
}

func (c *ProtoClient) CreateIssue(issueID, title, summary, body string) (*CreateIssueResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_CreateIssue{
			CreateIssue: &orchpb.CreateIssueRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				Title:      title,
				Body:       body,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	createResp := resp.GetCreateIssue()
	if createResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &CreateIssueResponse{
		OK:      true,
		IssueID: issueID,
		Path:    createResp.Path,
	}, nil
}

func (c *ProtoClient) CloseIssue(issueID, comment string) (*CloseIssueResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_CloseIssue{
			CloseIssue: &orchpb.CloseIssueRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &CloseIssueResponse{
		OK:      true,
		IssueID: issueID,
	}, nil
}

func (c *ProtoClient) StartRun(issueID, agent, model string) (*StartRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_StartRun{
			StartRun: &orchpb.StartRunRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				Agent:      agent,
				Model:      model,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	startResp := resp.GetStartRun()
	if startResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &StartRunResponse{
		OK:           true,
		RunID:        startResp.RunId,
		Branch:       startResp.Branch,
		WorktreePath: startResp.WorktreePath,
		TmuxSession:  startResp.TmuxSession,
	}, nil
}

func (c *ProtoClient) StopRun(issueID, runID string, force bool) (*StopRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_StopRun{
			StopRun: &orchpb.StopRunRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	return &StopRunResponse{
		OK:           true,
		StoppedCount: 1,
	}, nil
}

func (c *ProtoClient) ResolveIssue(issueID string, force bool) (*ResolveIssueResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ResolveIssue{
			ResolveIssue: &orchpb.ResolveIssueRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				Force:      force,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	resolveResp := resp.GetResolveIssue()
	if resolveResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &ResolveIssueResponse{
		OK:      true,
		IssueID: resolveResp.IssueId,
	}, nil
}

func (c *ProtoClient) GetAttachInfo(issueID, runID, shortID string) (*GetAttachInfoResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetAttachInfo{
			GetAttachInfo: &orchpb.GetAttachInfoRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	attachResp := resp.GetGetAttachInfo()
	if !resp.Ok && attachResp == nil {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}
	if attachResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &GetAttachInfoResponse{
		OK:           resp.Ok,
		Error:        resp.Error,
		IssueID:      issueID,
		RunID:        runID,
		TmuxSession:  attachResp.SessionName,
		Multiplexer:  protoMultiplexerToString(attachResp.Multiplexer),
		WorktreePath: attachResp.WorktreePath,
	}, nil
}

func (c *ProtoClient) RegisterMonitor(pid int, monitorType, view, project, tmuxSession string) (*RegisterMonitorResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_RegisterMonitor{
			RegisterMonitor: &orchpb.RegisterMonitorRequest{
				Pid:         int32(pid),
				MonitorType: monitorType,
				View:        view,
				Project:     project,
				SessionName: tmuxSession,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	regResp := resp.GetRegisterMonitor()
	if regResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &RegisterMonitorResponse{
		OK:        true,
		MonitorID: regResp.MonitorId,
	}, nil
}

func (c *ProtoClient) UnregisterMonitor(monitorID string) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_UnregisterMonitor{
			UnregisterMonitor: &orchpb.UnregisterMonitorRequest{
				MonitorId: monitorID,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Ok {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	return nil
}

func (c *ProtoClient) MonitorHeartbeat(monitorID string) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_Heartbeat{
			Heartbeat: &orchpb.HeartbeatRequest{
				MonitorId: monitorID,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Ok {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	return nil
}

func (c *ProtoClient) ListMonitors(projectRoot string, all bool) (*ListMonitorsResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ListMonitors{
			ListMonitors: &orchpb.ListMonitorsRequest{
				Project: projectRoot,
				All:     all,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	listResp := resp.GetListMonitors()
	if listResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	monitors := make([]*MonitorConnection, len(listResp.Monitors))
	for i, m := range listResp.Monitors {
		monitors[i] = &MonitorConnection{
			ID:          m.Id,
			PID:         int(m.Pid),
			Type:        m.Type,
			View:        m.View,
			Project:     m.Project,
			TmuxSession: m.SessionName,
			StartedAt:   time.Unix(m.StartedAtUnix, 0),
			LastSeen:    time.Unix(m.LastHeartbeatUnix, 0),
		}
	}

	return &ListMonitorsResponse{
		OK:       true,
		Monitors: monitors,
	}, nil
}

func (c *ProtoClient) KillMonitor(monitorID string, killAll bool, global bool, projectRoot string) (*KillMonitorResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_KillMonitor{
			KillMonitor: &orchpb.KillMonitorRequest{
				MonitorId: monitorID,
				All:       killAll,
				Global:    global,
				Project:   projectRoot,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	killResp := resp.GetKillMonitor()
	if killResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &KillMonitorResponse{
		OK:          true,
		KilledCount: int(killResp.KilledCount),
	}, nil
}

func (c *ProtoClient) GetControlAgentLaunch(projectRoot, agentType string, newSession bool) (*GetControlAgentLaunchResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetControlAgentLaunch{
			GetControlAgentLaunch: &orchpb.GetControlAgentLaunchRequest{
				ProjectRoot: projectRoot,
				Agent:       agentType,
				NewSession:  newSession,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	launchResp := resp.GetGetControlAgentLaunch()
	if launchResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &GetControlAgentLaunchResponse{
		OK:         true,
		Command:    launchResp.Command,
		PromptFile: launchResp.PromptFile,
		Port:       int(launchResp.Port),
		SessionID:  launchResp.SessionId,
	}, nil
}

func stringToProtoRunStatus(s string) orchpb.RunStatus {
	switch s {
	case "queued":
		return orchpb.RunStatus_RUN_STATUS_QUEUED
	case "booting":
		return orchpb.RunStatus_RUN_STATUS_BOOTING
	case "running":
		return orchpb.RunStatus_RUN_STATUS_RUNNING
	case "blocked":
		return orchpb.RunStatus_RUN_STATUS_BLOCKED
	case "blocked_api":
		return orchpb.RunStatus_RUN_STATUS_BLOCKED_API
	case "pr_open":
		return orchpb.RunStatus_RUN_STATUS_PR_OPEN
	case "done":
		return orchpb.RunStatus_RUN_STATUS_DONE
	case "failed":
		return orchpb.RunStatus_RUN_STATUS_FAILED
	case "canceled":
		return orchpb.RunStatus_RUN_STATUS_CANCELED
	default:
		return orchpb.RunStatus_RUN_STATUS_UNSPECIFIED
	}
}

func stringToProtoIssueStatus(s string) orchpb.IssueStatus {
	switch s {
	case "open":
		return orchpb.IssueStatus_ISSUE_STATUS_OPEN
	case "resolved":
		return orchpb.IssueStatus_ISSUE_STATUS_RESOLVED
	case "closed":
		return orchpb.IssueStatus_ISSUE_STATUS_CLOSED
	default:
		return orchpb.IssueStatus_ISSUE_STATUS_UNSPECIFIED
	}
}

func protoRunStatusToString(s orchpb.RunStatus) string {
	switch s {
	case orchpb.RunStatus_RUN_STATUS_QUEUED:
		return "queued"
	case orchpb.RunStatus_RUN_STATUS_BOOTING:
		return "booting"
	case orchpb.RunStatus_RUN_STATUS_RUNNING:
		return "running"
	case orchpb.RunStatus_RUN_STATUS_BLOCKED:
		return "blocked"
	case orchpb.RunStatus_RUN_STATUS_BLOCKED_API:
		return "blocked_api"
	case orchpb.RunStatus_RUN_STATUS_PR_OPEN:
		return "pr_open"
	case orchpb.RunStatus_RUN_STATUS_DONE:
		return "done"
	case orchpb.RunStatus_RUN_STATUS_FAILED:
		return "failed"
	case orchpb.RunStatus_RUN_STATUS_CANCELED:
		return "canceled"
	default:
		return "unknown"
	}
}

func protoIssueStatusToString(s orchpb.IssueStatus) string {
	switch s {
	case orchpb.IssueStatus_ISSUE_STATUS_OPEN:
		return "open"
	case orchpb.IssueStatus_ISSUE_STATUS_RESOLVED:
		return "resolved"
	case orchpb.IssueStatus_ISSUE_STATUS_CLOSED:
		return "closed"
	default:
		return "unknown"
	}
}

func protoMultiplexerToString(m orchpb.Multiplexer) string {
	switch m {
	case orchpb.Multiplexer_MULTIPLEXER_TMUX:
		return "tmux"
	case orchpb.Multiplexer_MULTIPLEXER_ZELLIJ:
		return "zellij"
	default:
		return ""
	}
}

func protoBranchStateToString(s orchpb.BranchState) string {
	switch s {
	case orchpb.BranchState_BRANCH_STATE_CLEAN:
		return "clean"
	case orchpb.BranchState_BRANCH_STATE_DIRTY:
		return "dirty"
	case orchpb.BranchState_BRANCH_STATE_MERGED:
		return "merged"
	case orchpb.BranchState_BRANCH_STATE_CONFLICT:
		return "conflict"
	default:
		return ""
	}
}

func protoRunToSummary(r *orchpb.Run) *RunSummary {
	if r == nil {
		return nil
	}

	var diffStats *DiffStatsJSON
	if r.DiffStats != nil {
		diffStats = &DiffStatsJSON{
			Additions:    int(r.DiffStats.Additions),
			Deletions:    int(r.DiffStats.Deletions),
			FilesChanged: int(r.DiffStats.FilesChanged),
			Files:        r.DiffStats.Files,
		}
	}

	return &RunSummary{
		IssueID:        r.IssueId,
		RunID:          r.RunId,
		ShortID:        computeShortID(r.IssueId, r.RunId),
		Status:         protoRunStatusToString(r.Status),
		Agent:          r.Agent,
		Model:          r.Model,
		Branch:         r.Branch,
		WorktreePath:   r.WorktreePath,
		TmuxSession:    r.TmuxSession,
		Multiplexer:    protoMultiplexerToString(r.Multiplexer),
		PRUrl:          r.PrUrl,
		DiffStats:      diffStats,
		BranchState:    protoBranchStateToString(r.BranchState),
		ElapsedSeconds: int(r.ElapsedSeconds),
		ElapsedDisplay: r.ElapsedDisplay,
		StartedAt:      formatUnixTime(r.StartedAtUnix),
		UpdatedAt:      formatUnixTime(r.UpdatedAtUnix),
		URI:            fmt.Sprintf("orch://run/%s/%s", r.IssueId, r.RunId),
	}
}

func protoRunToFull(r *orchpb.Run, events []*orchpb.Event) *RunFull {
	if r == nil {
		return nil
	}

	var diffStats *DiffStatsJSON
	if r.DiffStats != nil {
		diffStats = &DiffStatsJSON{
			Additions:    int(r.DiffStats.Additions),
			Deletions:    int(r.DiffStats.Deletions),
			FilesChanged: int(r.DiffStats.FilesChanged),
			Files:        r.DiffStats.Files,
		}
	}

	eventJSON := make([]*EventJSON, len(events))
	for i, e := range events {
		eventJSON[i] = &EventJSON{
			Timestamp: formatUnixTime(e.TimestampUnix),
			Type:      e.Type,
			Name:      e.Name,
			Attrs:     e.Attrs,
		}
	}

	return &RunFull{
		IssueID:           r.IssueId,
		RunID:             r.RunId,
		ShortID:           computeShortID(r.IssueId, r.RunId),
		Status:            protoRunStatusToString(r.Status),
		Agent:             r.Agent,
		Model:             r.Model,
		Branch:            r.Branch,
		WorktreePath:      r.WorktreePath,
		TmuxSession:       r.TmuxSession,
		Multiplexer:       protoMultiplexerToString(r.Multiplexer),
		PRUrl:             r.PrUrl,
		ServerPort:        int(r.ServerPort),
		OpenCodeSessionID: r.OpencodeSessionId,
		ContinuedFrom:     r.ContinuedFrom,
		DiffStats:         diffStats,
		BranchState:       protoBranchStateToString(r.BranchState),
		ElapsedSeconds:    int(r.ElapsedSeconds),
		ElapsedDisplay:    r.ElapsedDisplay,
		StartedAt:         formatUnixTime(r.StartedAtUnix),
		UpdatedAt:         formatUnixTime(r.UpdatedAtUnix),
		URI:               fmt.Sprintf("orch://run/%s/%s", r.IssueId, r.RunId),
		Events:            eventJSON,
	}
}

func protoIssueToSummary(i *orchpb.Issue) *IssueSummary {
	if i == nil {
		return nil
	}

	return &IssueSummary{
		ID:         i.Id,
		Title:      i.Title,
		Summary:    i.Summary,
		Status:     protoIssueStatusToString(i.Status),
		Tags:       i.Tags,
		URI:        fmt.Sprintf("orch://issue/%s", i.Id),
		ModifiedAt: formatUnixTime(i.ModifiedAtUnix),
	}
}

func protoIssueToFull(i *orchpb.Issue) *IssueFull {
	if i == nil {
		return nil
	}

	return &IssueFull{
		ID:      i.Id,
		Title:   i.Title,
		Summary: i.Summary,
		Status:  protoIssueStatusToString(i.Status),
		Body:    i.Body,
		Tags:    i.Tags,
		URI:     fmt.Sprintf("orch://issue/%s", i.Id),
	}
}

func computeShortID(issueID, runID string) string {
	ref := fmt.Sprintf("%s#%s", issueID, runID)
	h := fnvHash(ref)
	return fmt.Sprintf("%06x", h&0xFFFFFF)
}

func fnvHash(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}

func formatUnixTime(unix int64) string {
	if unix == 0 {
		return ""
	}
	return time.Unix(unix, 0).Format(time.RFC3339)
}

func (c *ProtoClient) AppendEvent(issueID, runID, eventType, eventName string, attrs map[string]string, source string) (*AppendEventResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_AppendEvent{
			AppendEvent: &orchpb.AppendEventRequest{
				IssuesRoot:  c.issuesRoot,
				IssueId:     issueID,
				RunId:       runID,
				EventType:   eventType,
				EventName:   eventName,
				EventAttrs:  attrs,
				EventSource: source,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	appendResp := resp.GetAppendEvent()
	if appendResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &AppendEventResponse{
		OK:      true,
		Skipped: appendResp.Skipped,
		Reason:  appendResp.Reason,
	}, nil
}

func (c *ProtoClient) AppendStatusEvent(issueID, runID string, status string, source string) error {
	resp, err := c.AppendEvent(issueID, runID, "status", status, nil, source)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}
	return nil
}

func (c *ProtoClient) AppendArtifactEvent(issueID, runID, artifactName string, attrs map[string]string, source string) error {
	resp, err := c.AppendEvent(issueID, runID, "artifact", artifactName, attrs, source)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}
	return nil
}

func (c *ProtoClient) GetOpenCodeServer(projectRoot string) (*GetOpenCodeServerResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_EnsureOpencodeServer{
			EnsureOpencodeServer: &orchpb.EnsureOpenCodeServerRequest{
				ProjectRoot: projectRoot,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	serverResp := resp.GetEnsureOpencodeServer()
	if serverResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &GetOpenCodeServerResponse{
		OK:      true,
		Port:    int(serverResp.Port),
		Healthy: serverResp.AlreadyRunning,
	}, nil
}

func (c *ProtoClient) RegisterRepo(projectRoot string) (string, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_RegisterRepo{
			RegisterRepo: &orchpb.RegisterRepoRequest{
				ProjectRoot: projectRoot,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Ok {
		return "", fmt.Errorf("daemon error: %s", resp.Error)
	}

	regResp := resp.GetRegisterRepo()
	if regResp == nil {
		return "", fmt.Errorf("unexpected response type")
	}

	return regResp.RepoId, nil
}

func (c *ProtoClient) ListRepos() ([]map[string]string, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ListRepos{
			ListRepos: &orchpb.ListReposRequest{},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	listResp := resp.GetListRepos()
	if listResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	repos := make([]map[string]string, len(listResp.Repos))
	for i, r := range listResp.Repos {
		repos[i] = map[string]string{
			"repo_id":      r.Id,
			"project_root": r.ProjectRoot,
			"issues_root":  r.IssuesRoot,
		}
	}

	return repos, nil
}
