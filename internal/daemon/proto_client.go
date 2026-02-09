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
		timeout:     30 * time.Second,
	}
}

func NewProtoClientWithIssuesRoot(projectRoot, issuesRoot string) *ProtoClient {
	return &ProtoClient{
		projectRoot: projectRoot,
		issuesRoot:  issuesRoot,
		timeout:     30 * time.Second,
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

func (c *ProtoClient) ListRuns(filter *ListRunsFilter) (*ListRunsResponse, error) {
	var issueID string
	var status []string
	var limit int
	var cursor string
	var olderThan string

	if filter != nil {
		issueID = filter.IssueID
		status = filter.Status
		limit = filter.Limit
		cursor = filter.Cursor
		olderThan = filter.OlderThan
	}

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
				OlderThan:  olderThan,
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

func (c *ProtoClient) StartRun(opts *StartRunOptions) (*StartRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_StartRun{
			StartRun: &orchpb.StartRunRequest{
				IssuesRoot:     c.issuesRoot,
				IssueId:        opts.IssueID,
				RunId:          opts.RunID,
				Agent:          opts.Agent,
				AgentCmd:       opts.AgentCmd,
				AgentProfile:   opts.AgentProfile,
				Model:          opts.Model,
				ModelVariant:   opts.ModelVariant,
				BaseBranch:     opts.BaseBranch,
				Branch:         opts.Branch,
				WorktreeDir:    opts.WorktreeDir,
				NoPr:           opts.NoPR,
				PromptTemplate: opts.PromptTemplate,
				PrTargetBranch: opts.PRTargetBranch,
				DryRun:         opts.DryRun,
				Reuse:          opts.Reuse,
				Multiplexer:    opts.Multiplexer,
				ProjectRoot:    opts.ProjectRoot,
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
		Status:       startResp.Status,
	}, nil
}

func (c *ProtoClient) ContinueRun(opts *ContinueRunOptions) (*ContinueRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ContinueRun{
			ContinueRun: &orchpb.ContinueRunRequest{
				IssuesRoot:     c.issuesRoot,
				ProjectRoot:    opts.ProjectRoot,
				IssueId:        opts.IssueID,
				RunId:          opts.RunID,
				ShortId:        opts.ShortID,
				Branch:         opts.Branch,
				Agent:          opts.Agent,
				AgentCmd:       opts.AgentCmd,
				AgentProfile:   opts.AgentProfile,
				WorktreeDir:    opts.WorktreeDir,
				NoPr:           opts.NoPR,
				PromptTemplate: opts.PromptTemplate,
				PrTargetBranch: opts.PRTargetBranch,
				Multiplexer:    opts.Multiplexer,
				TmuxSession:    opts.TmuxSession,
				RepoRoot:       opts.RepoRoot,
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

	continueResp := resp.GetContinueRun()
	if continueResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &ContinueRunResponse{
		OK:            true,
		RunID:         continueResp.RunId,
		Branch:        continueResp.Branch,
		WorktreePath:  continueResp.WorktreePath,
		TmuxSession:   continueResp.TmuxSession,
		Status:        continueResp.Status,
		ContinuedFrom: continueResp.ContinuedFrom,
		IssueID:       continueResp.IssueId,
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
				ShortId:    shortID,
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
		OK:                resp.Ok,
		Error:             resp.Error,
		IssueID:           attachResp.IssueId,
		RunID:             attachResp.RunId,
		Agent:             attachResp.Agent,
		TmuxSession:       attachResp.SessionName,
		Multiplexer:       protoMultiplexerToString(attachResp.Multiplexer),
		WorktreePath:      attachResp.WorktreePath,
		ServerPort:        int(attachResp.ServerPort),
		OpenCodeSessionID: attachResp.OpencodeSessionId,
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
	case orchpb.BranchState_BRANCH_STATE_AHEAD:
		return "ahead"
	case orchpb.BranchState_BRANCH_STATE_BEHIND:
		return "behind"
	case orchpb.BranchState_BRANCH_STATE_DIVERGED:
		return "diverged"
	case orchpb.BranchState_BRANCH_STATE_SYNCED:
		return "synced"
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
		PRNumber:          int(r.PrNumber),
		PRState:           r.PrState,
		ServerPort:        int(r.ServerPort),
		OpenCodeSessionID: r.OpencodeSessionId,
		DiffStats:         diffStats,
		BranchState:       protoBranchStateToString(r.BranchState),
		ElapsedSeconds:    int(r.ElapsedSeconds),
		ElapsedDisplay:    r.ElapsedDisplay,
		StartedAt:         formatUnixTime(r.StartedAtUnix),
		UpdatedAt:         formatUnixTime(r.UpdatedAtUnix),
		URI:               fmt.Sprintf("orch://run/%s/%s", r.IssueId, r.RunId),
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
		PRNumber:          int(r.PrNumber),
		PRState:           r.PrState,
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

type DeleteRunResponse struct {
	IssueID         string
	RunID           string
	ShortID         string
	WorktreeRemoved bool
	BranchRemoved   bool
	SessionKilled   bool
}

func (c *ProtoClient) DeleteRun(issueID, runID, shortID string, withWorktree, withBranch, force bool) (*DeleteRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_DeleteRun{
			DeleteRun: &orchpb.DeleteRunRequest{
				IssuesRoot:   c.issuesRoot,
				IssueId:      issueID,
				RunId:        runID,
				ShortId:      shortID,
				WithWorktree: withWorktree,
				WithBranch:   withBranch,
				Force:        force,
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

	delResp := resp.GetDeleteRun()
	if delResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &DeleteRunResponse{
		IssueID:         delResp.IssueId,
		RunID:           delResp.RunId,
		ShortID:         delResp.ShortId,
		WorktreeRemoved: delResp.WorktreeRemoved,
		BranchRemoved:   delResp.BranchRemoved,
		SessionKilled:   delResp.SessionKilled,
	}, nil
}

func (c *ProtoClient) UpdateIssue(issueID, title, summary, body, status string) (*IssueFull, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_UpdateIssue{
			UpdateIssue: &orchpb.UpdateIssueRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				Title:      title,
				Summary:    summary,
				Body:       body,
				Status:     status,
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

	updateResp := resp.GetUpdateIssue()
	if updateResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return protoIssueToFull(updateResp.Issue), nil
}

type ValidateIssueFilesResponse struct {
	Total      int
	Valid      int
	Errors     []*ValidationResultItem
	Warnings   []*ValidationResultItem
	Duplicates []*DuplicateIDItem
}

type ValidationResultItem struct {
	File     string
	IssueID  string
	Errors   []*ValidationIssueItem
	Warnings []*ValidationIssueItem
}

type ValidationIssueItem struct {
	Code    string
	Message string
	Line    int
	Level   string
}

type DuplicateIDItem struct {
	ID    string
	Files []string
}

func (c *ProtoClient) ValidateIssueFiles(issueID string) (*ValidateIssueFilesResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ValidateIssueFiles{
			ValidateIssueFiles: &orchpb.ValidateIssueFilesRequest{
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

	valResp := resp.GetValidateIssueFiles()
	if valResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	result := &ValidateIssueFilesResponse{
		Total: int(valResp.Total),
		Valid: int(valResp.Valid),
	}

	for _, e := range valResp.Errors {
		item := &ValidationResultItem{
			File:    e.File,
			IssueID: e.IssueId,
		}
		for _, issue := range e.Errors {
			item.Errors = append(item.Errors, &ValidationIssueItem{
				Code:    issue.Code,
				Message: issue.Message,
				Line:    int(issue.Line),
				Level:   issue.Level,
			})
		}
		for _, issue := range e.Warnings {
			item.Warnings = append(item.Warnings, &ValidationIssueItem{
				Code:    issue.Code,
				Message: issue.Message,
				Line:    int(issue.Line),
				Level:   issue.Level,
			})
		}
		result.Errors = append(result.Errors, item)
	}

	for _, w := range valResp.Warnings {
		item := &ValidationResultItem{
			File:    w.File,
			IssueID: w.IssueId,
		}
		for _, issue := range w.Warnings {
			item.Warnings = append(item.Warnings, &ValidationIssueItem{
				Code:    issue.Code,
				Message: issue.Message,
				Line:    int(issue.Line),
				Level:   issue.Level,
			})
		}
		result.Warnings = append(result.Warnings, item)
	}

	for _, d := range valResp.Duplicates {
		result.Duplicates = append(result.Duplicates, &DuplicateIDItem{
			ID:    d.Id,
			Files: d.Files,
		})
	}

	return result, nil
}

func (c *ProtoClient) WriteAgentPrompt(issueID, runID, shortID, content string) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_WriteAgentPrompt{
			WriteAgentPrompt: &orchpb.WriteAgentPromptRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
				ShortId:    shortID,
				Content:    content,
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

func (c *ProtoClient) ReadAgentPrompt(issueID, runID, shortID string) (string, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ReadAgentPrompt{
			ReadAgentPrompt: &orchpb.ReadAgentPromptRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
				ShortId:    shortID,
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

	readResp := resp.GetReadAgentPrompt()
	if readResp == nil {
		return "", fmt.Errorf("unexpected response type")
	}

	return readResp.Content, nil
}

type RepairStateResponse struct {
	ProblemsFound int
	ProblemsFixed int
	Details       []string
}

func (c *ProtoClient) RepairState(dryRun, force bool) (*RepairStateResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_RepairState{
			RepairState: &orchpb.RepairStateRequest{
				DryRun: dryRun,
				Force:  force,
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

	repairResp := resp.GetRepairState()
	if repairResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &RepairStateResponse{
		ProblemsFound: int(repairResp.ProblemsFound),
		ProblemsFixed: int(repairResp.ProblemsFixed),
		Details:       repairResp.Details,
	}, nil
}

func (c *ProtoClient) GetDaemonLog(lines int) (string, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetDaemonLog{
			GetDaemonLog: &orchpb.GetDaemonLogRequest{
				Lines: int32(lines),
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

	logResp := resp.GetGetDaemonLog()
	if logResp == nil {
		return "", fmt.Errorf("unexpected response type")
	}

	return logResp.Content, nil
}

func (c *ProtoClient) ReadFile(path string) ([]byte, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ReadFile{
			ReadFile: &orchpb.ReadFileRequest{
				Path: path,
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

	readResp := resp.GetReadFile()
	if readResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return readResp.Content, nil
}

func (c *ProtoClient) WriteFile(path string, content []byte, perm uint32) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_WriteFile{
			WriteFile: &orchpb.WriteFileRequest{
				Path:    path,
				Content: content,
				Perm:    perm,
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

func (c *ProtoClient) CreateRun(issueID, runID string, metadata map[string]string) (*orchpb.CreateRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_CreateRun{
			CreateRun: &orchpb.CreateRunRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
				Metadata:   metadata,
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

	createResp := resp.GetCreateRun()
	if createResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return createResp, nil
}

func (c *ProtoClient) CaptureSession(issueID, runID string) (*CaptureSessionResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_CaptureSession{
			CaptureSession: &orchpb.CaptureSessionRequest{
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

	captureResp := resp.GetCaptureSession()
	if captureResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &CaptureSessionResponse{
		Content:   captureResp.Content,
		Timestamp: captureResp.TimestampUnix,
		Source:    captureResp.Source,
	}, nil
}

func (c *ProtoClient) SendMessage(issueID, runID, message string) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_SendMessage{
			SendMessage: &orchpb.SendMessageRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
				Message:    message,
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

func (c *ProtoClient) InjectInitialPrompt(issueID, runID, prompt string) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_InjectInitialPrompt{
			InjectInitialPrompt: &orchpb.InjectInitialPromptRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
				Prompt:     prompt,
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

func (c *ProtoClient) GetDiffStats(issueID, runID string) (*GetDiffStatsResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetDiffStats{
			GetDiffStats: &orchpb.GetDiffStatsRequest{
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

	statsResp := resp.GetGetDiffStats()
	if statsResp == nil || statsResp.DiffStats == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &GetDiffStatsResponse{
		Additions:    int(statsResp.DiffStats.Additions),
		Deletions:    int(statsResp.DiffStats.Deletions),
		FilesChanged: int(statsResp.DiffStats.FilesChanged),
		Files:        statsResp.DiffStats.Files,
	}, nil
}

func (c *ProtoClient) GetBranchState(issueID, runID string) (string, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetBranchState{
			GetBranchState: &orchpb.GetBranchStateRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
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

	branchResp := resp.GetGetBranchState()
	if branchResp == nil {
		return "", fmt.Errorf("unexpected response type")
	}

	return protoBranchStateToString(branchResp.State), nil
}

func (c *ProtoClient) GetDiff(issueID, runID string) (string, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetDiff{
			GetDiff: &orchpb.GetDiffRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
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

	diffResp := resp.GetGetDiff()
	if diffResp == nil {
		return "", fmt.Errorf("unexpected response type")
	}

	return diffResp.Diff, nil
}

func (c *ProtoClient) KillSession(sessionName string, muxType string) (bool, error) {
	var mux orchpb.Multiplexer
	switch muxType {
	case "tmux":
		mux = orchpb.Multiplexer_MULTIPLEXER_TMUX
	case "zellij":
		mux = orchpb.Multiplexer_MULTIPLEXER_ZELLIJ
	default:
		mux = orchpb.Multiplexer_MULTIPLEXER_UNSPECIFIED
	}

	req := &orchpb.Request{
		Request: &orchpb.Request_KillSession{
			KillSession: &orchpb.KillSessionRequest{
				SessionName: sessionName,
				Multiplexer: mux,
			},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return false, err
	}

	if !resp.Ok {
		return false, fmt.Errorf("daemon error: %s", resp.Error)
	}

	killResp := resp.GetKillSession()
	if killResp == nil {
		return false, fmt.Errorf("unexpected response type")
	}

	return killResp.Killed, nil
}

func (c *ProtoClient) ListSessions(muxType string) ([]string, error) {
	var mux orchpb.Multiplexer
	switch muxType {
	case "tmux":
		mux = orchpb.Multiplexer_MULTIPLEXER_TMUX
	case "zellij":
		mux = orchpb.Multiplexer_MULTIPLEXER_ZELLIJ
	default:
		mux = orchpb.Multiplexer_MULTIPLEXER_UNSPECIFIED
	}

	req := &orchpb.Request{
		Request: &orchpb.Request_ListSessions{
			ListSessions: &orchpb.ListSessionsRequest{
				Multiplexer: mux,
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

	listResp := resp.GetListSessions()
	if listResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return listResp.Sessions, nil
}

type ResumeRunResponse struct {
	SessionName string
}

func (c *ProtoClient) ResumeRun(issueID, runID, shortID string) (*ResumeRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ResumeRun{
			ResumeRun: &orchpb.ResumeRunRequest{
				IssuesRoot: c.issuesRoot,
				IssueId:    issueID,
				RunId:      runID,
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

	resumeResp := resp.GetResumeRun()
	if resumeResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &ResumeRunResponse{
		SessionName: resumeResp.SessionName,
	}, nil
}

type ProviderInfo struct {
	ID     string
	Name   string
	Models []ModelInfo
}

type ModelInfo struct {
	ID       string
	Name     string
	Variants []string
}

type QueryOpenCodeServerResponse struct {
	ServerRunning bool
	Providers     []ProviderInfo
}

func (c *ProtoClient) QueryOpenCodeServer(port int) (*QueryOpenCodeServerResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_QueryOpencodeServer{
			QueryOpencodeServer: &orchpb.QueryOpenCodeServerRequest{
				Port: int32(port),
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

	queryResp := resp.GetQueryOpencodeServer()
	if queryResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	providers := make([]ProviderInfo, 0, len(queryResp.Providers))
	for _, p := range queryResp.Providers {
		models := make([]ModelInfo, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, ModelInfo{
				ID:       m.Id,
				Name:     m.Name,
				Variants: m.Variants,
			})
		}
		providers = append(providers, ProviderInfo{
			ID:     p.Id,
			Name:   p.Name,
			Models: models,
		})
	}

	return &QueryOpenCodeServerResponse{
		ServerRunning: queryResp.ServerRunning,
		Providers:     providers,
	}, nil
}
