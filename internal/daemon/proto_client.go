package daemon

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/xdg"
	"google.golang.org/protobuf/proto"
)

type ProtoClient struct {
	projectRoot     string
	daemonAddr      string
	timeout         time.Duration
	workerAuthToken string

	mu   sync.Mutex
	conn net.Conn
}

const protoSendMessageTimeoutBuffer = 5 * time.Second
const protoWaitForRunsTimeoutBuffer = 3 * time.Second

var requestCounter uint64

func NewProtoClient(projectRoot string) *ProtoClient {
	return &ProtoClient{
		projectRoot: projectRoot,
		timeout:     30 * time.Second,
	}
}

func NewProtoClientLocal(projectRoot string) *ProtoClient {
	return NewProtoClientWithAddress(projectRoot, "")
}

func NewProtoClientWithAddress(projectRoot, daemonAddr string) *ProtoClient {
	remoteAddr := strings.TrimSpace(daemonAddr)
	trimmedProjectRoot := strings.TrimSpace(projectRoot)
	if remoteAddr != "" && trimmedProjectRoot != "" {
		projectID := repoIDFromProjectSelector(trimmedProjectRoot)
		trimmedProjectRoot = strings.TrimSpace(projectID)
	}

	return &ProtoClient{
		projectRoot:     trimmedProjectRoot,
		daemonAddr:      remoteAddr,
		timeout:         30 * time.Second,
		workerAuthToken: strings.TrimSpace(os.Getenv("ORCH_WORKER_AUTH_TOKEN")),
	}
}

func (c *ProtoClient) SetWorkerAuthToken(token string) {
	c.workerAuthToken = strings.TrimSpace(token)
}

func (c *ProtoClient) projectRootForRequest(projectRoot string) string {
	target := strings.TrimSpace(projectRoot)
	if target == "" {
		target = strings.TrimSpace(c.projectRoot)
	}
	if strings.TrimSpace(c.daemonAddr) != "" {
		return ""
	}
	return target
}

func (c *ProtoClient) projectIDForRequest(projectRoot string) string {
	target := strings.TrimSpace(projectRoot)
	if target == "" {
		target = strings.TrimSpace(c.projectRoot)
	}
	if target == "" {
		return ""
	}

	return repoIDFromProjectSelector(target)
}

func (c *ProtoClient) newRequestID() string {
	seq := atomic.AddUint64(&requestCounter, 1)
	return fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), seq)
}

func (c *ProtoClient) requestContext(projectRoot string) *orchpb.RequestContext {
	projectID := c.projectIDForRequest(projectRoot)
	if projectID == "" {
		return nil
	}

	return &orchpb.RequestContext{
		ProjectId: projectID,
		RequestId: c.newRequestID(),
		ClientId:  "orch-cli",
	}
}

func (c *ProtoClient) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

func (c *ProtoClient) sendMessageTimeout() time.Duration {
	timeout := c.timeout
	minTimeout := openCodeSendAckTimeout + protoSendMessageTimeoutBuffer
	if timeout < minTimeout {
		return minTimeout
	}
	return timeout
}

func (c *ProtoClient) IsAvailable() bool {
	if c.daemonAddr != "" {
		req := &orchpb.Request{Request: &orchpb.Request_Ping{Ping: &orchpb.PingRequest{}}}
		resp, err := c.sendRequestWithTimeout(req, 500*time.Millisecond)
		return err == nil && resp != nil && resp.Ok && resp.GetPing() != nil && resp.GetPing().GetOk()
	}
	return IsDaemonSocketAvailable("") && IsRunning("")
}

func (c *ProtoClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	return err
}

// ARCHITECTURE NOTE (orch-447): ProtoClient reuses a single persistent Unix
// socket connection. Do not introduce connection-per-request dials in client
// request paths. Excessive socket churn can cause memory blowups in host
// security services that audit socket lifecycle events.
func (c *ProtoClient) sendRequestWithTimeout(req *orchpb.Request, timeout time.Duration) (*orchpb.Response, error) {
	return c.sendRequestWithOptions(req, timeout, false)
}

func (c *ProtoClient) sendRequestWithOptions(req *orchpb.Request, timeout time.Duration, noDeadline bool) (*orchpb.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if timeout <= 0 {
		timeout = c.timeout
	}

	conn, err := c.getOrConnectLocked(timeout)
	if err != nil {
		return nil, err
	}

	if noDeadline {
		_ = conn.SetDeadline(time.Time{})
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	resp, err := c.doSendRequest(conn, req)
	if err == nil {
		return resp, nil
	}

	// Connection might have gone stale (daemon restart, peer close, etc.).
	c.resetConnLocked()

	conn, reconnErr := c.getOrConnectLocked(timeout)
	if reconnErr != nil {
		return nil, fmt.Errorf("daemon request failed: %w (reconnect failed: %v)", err, reconnErr)
	}

	if noDeadline {
		_ = conn.SetDeadline(time.Time{})
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	resp, retryErr := c.doSendRequest(conn, req)
	if retryErr != nil {
		c.resetConnLocked()
		return nil, retryErr
	}

	return resp, nil
}

func (c *ProtoClient) getOrConnectLocked(timeout time.Duration) (net.Conn, error) {
	if c.conn != nil {
		return c.conn, nil
	}

	network, address, err := c.dialTarget()
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon (%s %s): %w", network, address, err)
	}
	c.conn = conn
	return c.conn, nil
}

func (c *ProtoClient) dialTarget() (string, string, error) {
	if c.daemonAddr == "" {
		return "unix", xdg.SocketPath(), nil
	}

	addr := strings.TrimSpace(c.daemonAddr)
	if addr == "" {
		return "unix", xdg.SocketPath(), nil
	}

	if strings.HasPrefix(addr, "tcp://") {
		hostPort := strings.TrimPrefix(addr, "tcp://")
		if hostPort == "" {
			return "", "", fmt.Errorf("invalid daemon address: %q", c.daemonAddr)
		}
		return "tcp", hostPort, nil
	}

	if strings.HasPrefix(addr, "unix://") {
		socketPath := strings.TrimPrefix(addr, "unix://")
		if socketPath == "" {
			return "", "", fmt.Errorf("invalid daemon address: %q", c.daemonAddr)
		}
		return "unix", socketPath, nil
	}

	if strings.Contains(addr, "://") {
		return "", "", fmt.Errorf("unsupported daemon address scheme: %q", c.daemonAddr)
	}

	return "tcp", addr, nil
}

func (c *ProtoClient) resetConnLocked() {
	if c.conn == nil {
		return
	}
	_ = c.conn.Close()
	c.conn = nil
}

func (c *ProtoClient) sendRequest(req *orchpb.Request) (*orchpb.Response, error) {
	return c.sendRequestWithTimeout(req, c.timeout)
}

func (c *ProtoClient) doSendRequest(conn net.Conn, req *orchpb.Request) (*orchpb.Response, error) {

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
				IssueId:   issueID,
				Status:    protoStatuses,
				Limit:     int32(limit),
				Cursor:    cursor,
				OlderThan: olderThan,
				Context:   c.requestContext(c.projectRoot),
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
	cfg, _ := config.Load()

	runs := make([]*RunSummary, len(listResp.Runs))
	for i, r := range listResp.Runs {
		runs[i] = protoRunToSummary(r, cfg)
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
				IssueId: issueID,
				RunId:   runID,
				Context: c.requestContext(c.projectRoot),
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
		Run: protoRunToFull(getResp.Run, getResp.Events, loadConfigOrNil()),
	}, nil
}

func (c *ProtoClient) GetRunByShortID(shortID string) (*GetRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetRunByShortId{
			GetRunByShortId: &orchpb.GetRunByShortIDRequest{
				ShortId: shortID,
				Context: c.requestContext(c.projectRoot),
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
		Run: protoRunToFull(getResp.Run, getResp.Events, loadConfigOrNil()),
	}, nil
}

func (c *ProtoClient) WaitForRuns(runRefs []string, timeoutSeconds int) (*WaitForRunsResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_WaitForRuns{
			WaitForRuns: &orchpb.WaitForRunsRequest{
				RunRefs:        runRefs,
				TimeoutSeconds: int32(timeoutSeconds),
				Context:        c.requestContext(c.projectRoot),
			},
		},
	}

	requestTimeout := c.timeout
	noDeadline := timeoutSeconds == 0
	if timeoutSeconds > 0 {
		requestTimeout = time.Duration(timeoutSeconds)*time.Second + protoWaitForRunsTimeoutBuffer
	}

	resp, err := c.sendRequestWithOptions(req, requestTimeout, noDeadline)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	waitResp := resp.GetWaitForRuns()
	if waitResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &WaitForRunsResponse{
		OK:     true,
		RunID:  waitResp.RunId,
		Status: waitResp.Status,
		Issue:  waitResp.Issue,
		PRURL:  waitResp.PrUrl,
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
				Status:  protoStatuses,
				Limit:   int32(limit),
				Cursor:  cursor,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				Context: c.requestContext(c.projectRoot),
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

func (c *ProtoClient) CreateIssue(issueID, title, summary, body string, tags []string, baseBranch string) (*CreateIssueResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_CreateIssue{
			CreateIssue: &orchpb.CreateIssueRequest{
				IssueId:    issueID,
				Title:      title,
				Body:       body,
				Tags:       tags,
				BaseBranch: baseBranch,
				Context:    c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				Context: c.requestContext(c.projectRoot),
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
				IssueId:        opts.IssueID.String(),
				RunId:          opts.RunID.String(),
				Agent:          opts.Agent,
				AgentCmd:       opts.AgentCmd,
				AgentProfile:   opts.AgentProfile,
				Model:          opts.Model,
				ModelVariant:   opts.ModelVariant,
				Preset:         opts.Preset,
				BaseBranch:     opts.BaseBranch,
				Branch:         opts.Branch,
				WorktreeDir:    opts.WorktreeDir,
				NoPr:           opts.NoPR,
				PromptTemplate: opts.PromptTemplate,
				PrTargetBranch: opts.PRTargetBranch,
				DryRun:         opts.DryRun,
				Reuse:          opts.Reuse,
				Multiplexer:    opts.Multiplexer,
				Target:         opts.Target,
				Context:        c.requestContext(c.projectRoot),
			},
		},
	}

	resp, err := c.sendRequestWithTimeout(req, 120*time.Second)
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
		SessionName:  startResp.SessionName,
		Status:       startResp.Status,
	}, nil
}

func (c *ProtoClient) ContinueRun(opts *ContinueRunOptions) (*ContinueRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ContinueRun{
			ContinueRun: &orchpb.ContinueRunRequest{
				IssueId:        opts.IssueID.String(),
				RunId:          opts.RunID.String(),
				ShortId:        opts.ShortID.String(),
				Branch:         opts.Branch,
				Agent:          opts.Agent,
				AgentCmd:       opts.AgentCmd,
				AgentProfile:   opts.AgentProfile,
				WorktreeDir:    opts.WorktreeDir,
				NoPr:           opts.NoPR,
				PromptTemplate: opts.PromptTemplate,
				PrTargetBranch: opts.PRTargetBranch,
				Multiplexer:    opts.Multiplexer,
				SessionName:    opts.SessionName,
				Context:        c.requestContext(c.projectRoot),
			},
		},
	}

	resp, err := c.sendRequestWithTimeout(req, 120*time.Second)
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
		SessionName:   continueResp.SessionName,
		Status:        continueResp.Status,
		ContinuedFrom: continueResp.ContinuedFrom,
		IssueID:       continueResp.IssueId,
	}, nil
}

func (c *ProtoClient) StopRun(issueID, runID string, force bool) (*StopRunResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_StopRun{
			StopRun: &orchpb.StopRunRequest{
				IssueId: issueID,
				RunId:   runID,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				Force:   force,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				RunId:   runID,
				ShortId: shortID,
				Context: c.requestContext(c.projectRoot),
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
		SessionName:       attachResp.SessionName,
		Multiplexer:       protoMultiplexerToString(attachResp.Multiplexer),
		WorktreePath:      attachResp.WorktreePath,
		ServerPort:        int(attachResp.ServerPort),
		OpenCodeSessionID: attachResp.OpencodeSessionId,
		TargetHost:        attachResp.TargetHost,
	}, nil
}

func (c *ProtoClient) RegisterMonitor(pid int, monitorType, view, project, sessionName string) (*RegisterMonitorResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_RegisterMonitor{
			RegisterMonitor: &orchpb.RegisterMonitorRequest{
				Pid:         int32(pid),
				MonitorType: monitorType,
				View:        view,
				Project:     project,
				SessionName: sessionName,
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
			SessionName: m.SessionName,
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

func (c *ProtoClient) RegisterWorker(workerID, workerType, host, mode string) (*RegisterWorkerResponse, error) {
	return c.RegisterWorkerWithCapabilities(workerID, workerType, host, mode, nil)
}

func (c *ProtoClient) RegisterWorkerWithCapabilities(workerID, workerType, host, mode string, capabilities []string) (*RegisterWorkerResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_RegisterWorker{
			RegisterWorker: &orchpb.RegisterWorkerRequest{
				WorkerId:     workerID,
				WorkerType:   workerType,
				Host:         host,
				Mode:         mode,
				AuthToken:    c.workerAuthToken,
				Capabilities: append([]string(nil), capabilities...),
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

	regResp := resp.GetRegisterWorker()
	if regResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &RegisterWorkerResponse{
		OK:                  true,
		WorkerID:            regResp.WorkerId,
		HeartbeatTTLSeconds: regResp.HeartbeatTtlSeconds,
	}, nil
}

func (c *ProtoClient) UnregisterWorker(workerID string) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_UnregisterWorker{
			UnregisterWorker: &orchpb.UnregisterWorkerRequest{WorkerId: workerID},
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

func (c *ProtoClient) WorkerHeartbeat(workerID string) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_WorkerHeartbeat{
			WorkerHeartbeat: &orchpb.WorkerHeartbeatRequest{WorkerId: workerID, AuthToken: c.workerAuthToken},
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

func (c *ProtoClient) ListWorkers() (*ListWorkersResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_ListWorkers{ListWorkers: &orchpb.ListWorkersRequest{}},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}
	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	listResp := resp.GetListWorkers()
	if listResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	workers := make([]*WorkerRegistration, 0, len(listResp.Workers))
	for _, w := range listResp.Workers {
		workers = append(workers, &WorkerRegistration{
			ID:            w.Id,
			WorkerType:    w.WorkerType,
			Host:          w.Host,
			Mode:          w.Mode,
			Capabilities:  append([]string(nil), w.Capabilities...),
			RegisteredAt:  time.Unix(w.RegisteredAtUnix, 0),
			LastHeartbeat: time.Unix(w.LastHeartbeatUnix, 0),
			Active:        w.Active,
		})
	}

	return &ListWorkersResponse{OK: true, Workers: workers}, nil
}

func (c *ProtoClient) LeaseWork(workerID string) (*LeaseWorkResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_LeaseWork{
			LeaseWork: &orchpb.LeaseWorkRequest{WorkerId: workerID, AuthToken: c.workerAuthToken},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}
	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	leaseResp := resp.GetLeaseWork()
	if leaseResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	var payload *WorkerEffectPayload
	if strings.TrimSpace(leaseResp.PayloadJson) != "" {
		payload = &WorkerEffectPayload{}
		if err := json.Unmarshal([]byte(leaseResp.PayloadJson), payload); err != nil {
			return nil, fmt.Errorf("invalid lease payload_json: %w", err)
		}
	}

	return &LeaseWorkResponse{
		OK: true,
		Lease: &WorkerLease{
			LeaseID:     leaseResp.LeaseId,
			WorkerID:    leaseResp.WorkerId,
			ProjectID:   leaseResp.ProjectId,
			Effect:      leaseResp.Effect,
			IssueID:     leaseResp.IssueId,
			RunID:       leaseResp.RunId,
			LeasedAt:    time.Unix(leaseResp.LeasedAtUnix, 0),
			ExpiresAt:   time.Unix(leaseResp.ExpiresAtUnix, 0),
			Payload:     payload,
			PayloadJSON: leaseResp.PayloadJson,
		},
	}, nil
}

func (c *ProtoClient) AcknowledgeEffect(workerID, leaseID string, success bool, effectErr, resultJSON string) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_AcknowledgeEffect{
			AcknowledgeEffect: &orchpb.AcknowledgeEffectRequest{
				WorkerId:   workerID,
				LeaseId:    leaseID,
				Success:    success,
				Error:      effectErr,
				ResultJson: resultJSON,
				AuthToken:  c.workerAuthToken,
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

func (c *ProtoClient) GetControlAgentLaunch(agentType string, newSession bool) (*GetControlAgentLaunchResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetControlAgentLaunch{
			GetControlAgentLaunch: &orchpb.GetControlAgentLaunchRequest{
				Agent:      agentType,
				NewSession: newSession,
				Context:    c.requestContext(c.projectRoot),
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

func (c *ProtoClient) GetControlAgentConfig() (*GetControlAgentConfigResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetControlAgentConfig{
			GetControlAgentConfig: &orchpb.GetControlAgentConfigRequest{
				Context: c.requestContext(c.projectRoot),
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

	configResp := resp.GetGetControlAgentConfig()
	if configResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &GetControlAgentConfigResponse{
		OK:            true,
		PromptContent: configResp.PromptContent,
		Agent:         configResp.Agent,
		Model:         configResp.Model,
		ModelVariant:  configResp.ModelVariant,
		ExtraArgs:     configResp.ExtraArgs,
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
	case "blocked", "waiting":
		return orchpb.RunStatus_RUN_STATUS_WAITING
	case "blocked_api", "rate_limited":
		return orchpb.RunStatus_RUN_STATUS_RATE_LIMITED
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
	case orchpb.RunStatus_RUN_STATUS_WAITING:
		return "waiting"
	case orchpb.RunStatus_RUN_STATUS_RATE_LIMITED:
		return "rate_limited"
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

func protoRunToSummary(r *orchpb.Run, _ *config.Config) *RunSummary {
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
		Target:            r.Target,
		TargetHost:        strings.TrimSpace(r.TargetHost),
		SessionName:       r.SessionName,
		Multiplexer:       protoMultiplexerToString(r.Multiplexer),
		PRUrl:             r.PrUrl,
		PRNumber:          int(r.PrNumber),
		PRState:           r.PrState,
		ServerPort:        int(r.ServerPort),
		OpenCodeSessionID: r.OpencodeSessionId,
		IssueStatus:       r.IssueStatus,
		IssueTopic:        r.IssueTopic,
		DiffStats:         diffStats,
		BranchState:       protoBranchStateToString(r.BranchState),
		ElapsedSeconds:    int(r.ElapsedSeconds),
		ElapsedDisplay:    r.ElapsedDisplay,
		Alive:             r.Alive,
		AliveKnown:        r.AliveKnown,
		WorktreeExists:    r.WorktreeExists,
		StartedAt:         formatUnixTime(r.StartedAtUnix),
		UpdatedAt:         formatUnixTime(r.UpdatedAtUnix),
		URI:               fmt.Sprintf("orch://run/%s/%s", r.IssueId, r.RunId),
	}
}

func protoRunToFull(r *orchpb.Run, events []*orchpb.Event, _ *config.Config) *RunFull {
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
	phase := ""
	for i, e := range events {
		if e.Type == "phase" {
			if e.Name != "" {
				phase = e.Name
			} else if e.Attrs != nil {
				if p := e.Attrs["phase"]; p != "" {
					phase = p
				} else if p := e.Attrs["name"]; p != "" {
					phase = p
				}
			}
		}
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
		Phase:             phase,
		Agent:             r.Agent,
		Model:             r.Model,
		Branch:            r.Branch,
		WorktreePath:      r.WorktreePath,
		Target:            r.Target,
		TargetHost:        strings.TrimSpace(r.TargetHost),
		SessionName:       r.SessionName,
		Multiplexer:       protoMultiplexerToString(r.Multiplexer),
		PRUrl:             r.PrUrl,
		PRNumber:          int(r.PrNumber),
		PRState:           r.PrState,
		ServerPort:        int(r.ServerPort),
		OpenCodeSessionID: r.OpencodeSessionId,
		IssueStatus:       r.IssueStatus,
		IssueTopic:        r.IssueTopic,
		ContinuedFrom:     r.ContinuedFrom,
		DiffStats:         diffStats,
		BranchState:       protoBranchStateToString(r.BranchState),
		ElapsedSeconds:    int(r.ElapsedSeconds),
		ElapsedDisplay:    r.ElapsedDisplay,
		Alive:             r.Alive,
		AliveKnown:        r.AliveKnown,
		WorktreeExists:    r.WorktreeExists,
		StartedAt:         formatUnixTime(r.StartedAtUnix),
		UpdatedAt:         formatUnixTime(r.UpdatedAtUnix),
		URI:               fmt.Sprintf("orch://run/%s/%s", r.IssueId, r.RunId),
		Events:            eventJSON,
	}
}

func loadConfigOrNil() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	return cfg
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
		ID:         i.Id,
		Title:      i.Title,
		Summary:    i.Summary,
		Status:     protoIssueStatusToString(i.Status),
		Body:       i.Body,
		Tags:       i.Tags,
		BaseBranch: i.BaseBranch,
		URI:        fmt.Sprintf("orch://issue/%s", i.Id),
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
				IssueId:     issueID,
				RunId:       runID,
				EventType:   eventType,
				EventName:   eventName,
				EventAttrs:  attrs,
				EventSource: source,
				Context:     c.requestContext(c.projectRoot),
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

func (c *ProtoClient) GetOpenCodeServer() (*GetOpenCodeServerResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_EnsureOpencodeServer{
			EnsureOpencodeServer: &orchpb.EnsureOpenCodeServerRequest{
				Context: c.requestContext(c.projectRoot),
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
				ProjectRoot: strings.TrimSpace(projectRoot),
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
				IssueId:      issueID,
				RunId:        runID,
				ShortId:      shortID,
				WithWorktree: withWorktree,
				WithBranch:   withBranch,
				Force:        force,
				Context:      c.requestContext(c.projectRoot),
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

type CleanRunWorktreeResponse struct {
	IssueID         string
	RunID           string
	ShortID         string
	WorktreePath    string
	WorktreeRemoved bool
	Skipped         bool
	Reason          string
}

func (c *ProtoClient) CleanRunWorktree(issueID, runID, shortID string) (*CleanRunWorktreeResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_CleanRunWorktree{
			CleanRunWorktree: &orchpb.CleanRunWorktreeRequest{
				IssueId: issueID,
				RunId:   runID,
				ShortId: shortID,
				Context: c.requestContext(c.projectRoot),
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

	cleanResp := resp.GetCleanRunWorktree()
	if cleanResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &CleanRunWorktreeResponse{
		IssueID:         cleanResp.IssueId,
		RunID:           cleanResp.RunId,
		ShortID:         cleanResp.ShortId,
		WorktreePath:    cleanResp.WorktreePath,
		WorktreeRemoved: cleanResp.WorktreeRemoved,
		Skipped:         cleanResp.Skipped,
		Reason:          cleanResp.Reason,
	}, nil
}

func (c *ProtoClient) UpdateIssue(issueID, title, summary, body, status string) (*IssueFull, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_UpdateIssue{
			UpdateIssue: &orchpb.UpdateIssueRequest{
				IssueId: issueID,
				Title:   title,
				Summary: summary,
				Body:    body,
				Status:  status,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				RunId:   runID,
				ShortId: shortID,
				Content: content,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				RunId:   runID,
				ShortId: shortID,
				Context: c.requestContext(c.projectRoot),
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
				IssueId:  issueID,
				RunId:    runID,
				Metadata: metadata,
				Context:  c.requestContext(c.projectRoot),
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

func (c *ProtoClient) CaptureSession(issueID, runID string, lines int) (*CaptureSessionResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_CaptureSession{
			CaptureSession: &orchpb.CaptureSessionRequest{
				IssueId: issueID,
				RunId:   runID,
				Context: c.requestContext(c.projectRoot),
				Lines:   int32(lines),
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

func (c *ProtoClient) SendMessage(issueID, runID, message string, noEnter bool) error {
	req := &orchpb.Request{
		Request: &orchpb.Request_SendMessage{
			SendMessage: &orchpb.SendMessageRequest{
				IssueId: issueID,
				RunId:   runID,
				Message: message,
				NoEnter: noEnter,
				Context: c.requestContext(c.projectRoot),
			},
		},
	}

	resp, err := c.sendRequestWithTimeout(req, c.sendMessageTimeout())
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
				IssueId: issueID,
				RunId:   runID,
				Prompt:  prompt,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				RunId:   runID,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				RunId:   runID,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				RunId:   runID,
				Context: c.requestContext(c.projectRoot),
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
				IssueId: issueID,
				RunId:   runID,
				ShortId: shortID,
				Context: c.requestContext(c.projectRoot),
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

func (c *ProtoClient) GetConfig() (*ConfigResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetConfig{
			GetConfig: &orchpb.GetConfigRequest{
				Context: c.requestContext(c.projectRoot),
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

	configResp := resp.GetGetConfig()
	if configResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	cfg := &ConfigResponse{
		Agent:               configResp.Agent,
		Model:               configResp.Model,
		ModelVariant:        configResp.ModelVariant,
		WorktreeDir:         configResp.WorktreeDir,
		BaseBranch:          configResp.BaseBranch,
		PRTargetBranch:      configResp.PrTargetBranch,
		LogLevel:            configResp.LogLevel,
		PromptTemplate:      configResp.PromptTemplate,
		Multiplexer:         configResp.Multiplexer,
		MonitorMultiplexer:  configResp.MonitorMultiplexer,
		AgentMultiplexer:    configResp.AgentMultiplexer,
		NoPR:                configResp.NoPr,
		DefaultPreset:       configResp.DefaultPreset,
		ControlAgent:        configResp.ControlAgent,
		ControlModel:        configResp.ControlModel,
		ControlModelVariant: configResp.ControlModelVariant,
		DiffTool:            configResp.DiffTool,
	}

	if configResp.Monitor != nil {
		cfg.Monitor.PSColumns = configResp.Monitor.PsColumns
	}
	if configResp.Ps != nil {
		cfg.PS.DefaultStatuses = configResp.Ps.DefaultStatuses
	}

	for _, p := range configResp.Presets {
		cfg.Presets = append(cfg.Presets, PresetConfig{
			Name:    p.Name,
			Backend: p.Backend,
			Model:   p.Model,
			Variant: p.Variant,
			Profile: p.Profile,
		})
	}

	if configResp.Opencode != nil {
		cfg.OpenCode.DefaultModel = configResp.Opencode.DefaultModel
		cfg.OpenCode.DefaultVariant = configResp.Opencode.DefaultVariant
		cfg.OpenCode.PromptTemplate = configResp.Opencode.PromptTemplate
		cfg.OpenCode.ExtraArgs = configResp.Opencode.ExtraArgs
		cfg.OpenCode.ControlExtraArgs = configResp.Opencode.ControlExtraArgs
	}

	if configResp.Claude != nil {
		cfg.Claude.PromptTemplate = configResp.Claude.PromptTemplate
		cfg.Claude.ExtraArgs = configResp.Claude.ExtraArgs
		cfg.Claude.ControlExtraArgs = configResp.Claude.ControlExtraArgs
	}

	if configResp.Codex != nil {
		cfg.Codex.PromptTemplate = configResp.Codex.PromptTemplate
		cfg.Codex.ExtraArgs = configResp.Codex.ExtraArgs
		cfg.Codex.ControlExtraArgs = configResp.Codex.ControlExtraArgs
	}

	if configResp.Gemini != nil {
		cfg.Gemini.PromptTemplate = configResp.Gemini.PromptTemplate
		cfg.Gemini.ExtraArgs = configResp.Gemini.ExtraArgs
		cfg.Gemini.ControlExtraArgs = configResp.Gemini.ControlExtraArgs
	}

	if configResp.Slack != nil {
		cfg.Slack.Enabled = configResp.Slack.Enabled
		cfg.Slack.WebhookURL = configResp.Slack.WebhookUrl
		cfg.Slack.BotToken = configResp.Slack.BotToken
		cfg.Slack.Channel = configResp.Slack.Channel
		cfg.Slack.NotifyOn = configResp.Slack.NotifyOn
	}

	if configResp.Issues != nil {
		cfg.Issues.Backend = configResp.Issues.Backend
		cfg.Issues.Path = configResp.Issues.Path
	}

	if configResp.Github != nil {
		cfg.GitHub.Owner = configResp.Github.Owner
		cfg.GitHub.Repo = configResp.Github.Repo
		cfg.GitHub.LabelFilter = configResp.Github.LabelFilter
		cfg.GitHub.PollInterval = int(configResp.Github.PollInterval)
		cfg.GitHub.StatusLabels = configResp.Github.StatusLabels
	}

	return cfg, nil
}

func (c *ProtoClient) GetDaemonStatus() (*DaemonStatusResponse, error) {
	req := &orchpb.Request{
		Request: &orchpb.Request_GetDaemonStatus{
			GetDaemonStatus: &orchpb.GetDaemonStatusRequest{},
		},
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Ok {
		return nil, fmt.Errorf("daemon error: %s", resp.Error)
	}

	statusResp := resp.GetGetDaemonStatus()
	if statusResp == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	return &DaemonStatusResponse{
		Running: statusResp.Running,
		PID:     int(statusResp.Pid),
		LogPath: statusResp.LogPath,
		Version: statusResp.Version,
	}, nil
}
