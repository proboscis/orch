package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
)

const (
	socketFile = "daemon.sock"
)

func SocketFilePath(vaultPath string) string {
	return filepath.Join(OrchDir(vaultPath), socketFile)
}

type SendRequest struct {
	Type      string   `json:"type"`
	IssueID   string   `json:"issue_id"`
	RunID     string   `json:"run_id"`
	Message   string   `json:"message"`
	NoEnter   bool     `json:"no_enter,omitempty"`
	VaultPath string   `json:"vault_path"`
	Status    []string `json:"status,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Cursor    string   `json:"cursor,omitempty"`
	All       bool     `json:"all,omitempty"`
	Force     bool     `json:"force,omitempty"`
	Title     string   `json:"title,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Body      string   `json:"body,omitempty"`
}

type SendResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type SocketServer struct {
	vaultPath string
	store     store.Store
	listener  net.Listener
	logger    Logger
	stopCh    chan struct{}
}

type Logger interface {
	Printf(format string, v ...interface{})
}

func NewSocketServer(vaultPath string, st store.Store, logger Logger) *SocketServer {
	return &SocketServer{
		vaultPath: vaultPath,
		store:     st,
		logger:    logger,
		stopCh:    make(chan struct{}),
	}
}

func (s *SocketServer) Start() error {
	socketPath := SocketFilePath(s.vaultPath)

	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	if err := os.Chmod(socketPath, 0660); err != nil {
		s.logger.Printf("warning: failed to chmod socket: %v", err)
	}

	s.logger.Printf("socket server listening on %s", socketPath)

	go s.acceptLoop()

	return nil
}

func (s *SocketServer) Stop() {
	close(s.stopCh)
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(SocketFilePath(s.vaultPath))
}

func (s *SocketServer) acceptLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				s.logger.Printf("accept error: %v", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *SocketServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req SendRequest
	if err := decoder.Decode(&req); err != nil {
		s.logger.Printf("failed to decode request: %v", err)
		encoder.Encode(SendResponse{OK: false, Error: "invalid_request"})
		return
	}

	switch req.Type {
	case "send":
		s.handleSend(req, encoder)
	case "list_runs":
		s.handleListRuns(req, encoder)
	case "list_issues":
		s.handleListIssues(req, encoder)
	case "get_run":
		s.handleGetRun(req, encoder)
	case "get_issue":
		s.handleGetIssue(req, encoder)
	case "stop_run":
		s.handleStopRun(req, encoder)
	case "resolve_issue":
		s.handleResolveIssue(req, encoder)
	case "create_issue":
		s.handleCreateIssue(req, encoder)
	case "attach_info":
		s.handleAttachInfo(req, encoder)
	default:
		encoder.Encode(SendResponse{OK: false, Error: "unknown_type"})
	}
}

func (s *SocketServer) handleSend(req SendRequest, encoder *json.Encoder) {
	encoder.Encode(SendResponse{OK: true})
	go s.processSend(req)
}

func (s *SocketServer) processSend(req SendRequest) {
	s.logger.Printf("processing send for %s#%s", req.IssueID, req.RunID)

	ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
	run, err := s.store.GetRun(ref)
	if err != nil {
		s.logger.Printf("failed to get run %s#%s: %v", req.IssueID, req.RunID, err)
		return
	}

	if run.Agent != string(agent.AgentOpenCode) {
		s.logger.Printf("run %s#%s is not opencode agent, skipping", req.IssueID, req.RunID)
		return
	}

	if run.ServerPort <= 0 || run.OpenCodeSessionID == "" {
		s.logger.Printf("run %s#%s missing server config (port=%d, session=%s)",
			req.IssueID, req.RunID, run.ServerPort, run.OpenCodeSessionID)
		return
	}

	client := agent.NewOpenCodeClient(run.ServerPort)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	err = client.SendMessagePrompt(ctx, run.OpenCodeSessionID, req.Message, run.WorktreePath)
	if err != nil {
		s.logger.Printf("failed to send message to %s#%s: %v", req.IssueID, req.RunID, err)
		return
	}

	s.logger.Printf("message sent successfully to %s#%s", req.IssueID, req.RunID)
}

func (s *SocketServer) handleListRuns(req SendRequest, encoder *json.Encoder) {
	const defaultLimit = 50
	const maxLimit = 200

	limit := req.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	offset, err := DecodeCursor(req.Cursor)
	if err != nil {
		encoder.Encode(ListRunsResponse{OK: false, Error: err.Error()})
		return
	}

	filter := &store.ListRunsFilter{
		IssueID: req.IssueID,
		Limit:   0,
	}

	for _, status := range req.Status {
		filter.Status = append(filter.Status, model.Status(status))
	}

	runs, err := s.store.ListRuns(filter)
	if err != nil {
		s.logger.Printf("error listing runs: %v", err)
		encoder.Encode(ListRunsResponse{OK: false, Error: "store_error"})
		return
	}

	total := len(runs)

	if offset > len(runs) {
		offset = len(runs)
	}
	end := offset + limit
	if end > len(runs) {
		end = len(runs)
	}
	paginatedRuns := runs[offset:end]

	summaries := make([]*RunSummary, len(paginatedRuns))
	for i, run := range paginatedRuns {
		summaries[i] = RunToSummary(run)
	}

	var nextCursor *string
	if end < total {
		c := EncodeCursor(end)
		nextCursor = &c
	}

	encoder.Encode(ListRunsResponse{
		OK:         true,
		Runs:       summaries,
		NextCursor: nextCursor,
		Total:      total,
	})
}

func (s *SocketServer) handleListIssues(req SendRequest, encoder *json.Encoder) {
	const defaultLimit = 50
	const maxLimit = 200

	limit := req.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	offset, err := DecodeCursor(req.Cursor)
	if err != nil {
		encoder.Encode(ListIssuesResponse{OK: false, Error: err.Error()})
		return
	}

	issues, err := s.store.ListIssues()
	if err != nil {
		s.logger.Printf("error listing issues: %v", err)
		encoder.Encode(ListIssuesResponse{OK: false, Error: "store_error"})
		return
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ID < issues[j].ID
	})

	if len(req.Status) > 0 {
		statusSet := make(map[string]bool)
		for _, st := range req.Status {
			statusSet[st] = true
		}
		var filtered []*model.Issue
		for _, issue := range issues {
			if statusSet[string(issue.Status)] {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	total := len(issues)

	if offset > len(issues) {
		offset = len(issues)
	}
	end := offset + limit
	if end > len(issues) {
		end = len(issues)
	}
	paginatedIssues := issues[offset:end]

	summaries := make([]*IssueSummary, len(paginatedIssues))
	for i, issue := range paginatedIssues {
		summaries[i] = IssueToSummary(issue)
	}

	var nextCursor *string
	if end < total {
		c := EncodeCursor(end)
		nextCursor = &c
	}

	encoder.Encode(ListIssuesResponse{
		OK:         true,
		Issues:     summaries,
		NextCursor: nextCursor,
		Total:      total,
	})
}

func (s *SocketServer) handleGetRun(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(GetRunResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
	run, err := s.store.GetRun(ref)
	if err != nil {
		s.logger.Printf("error getting run %s#%s: %v", req.IssueID, req.RunID, err)
		encoder.Encode(GetRunResponse{OK: false, Error: "not_found"})
		return
	}

	encoder.Encode(GetRunResponse{
		OK:  true,
		Run: RunToFull(run),
	})
}

func (s *SocketServer) handleGetIssue(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(GetIssueResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	issue, err := s.store.ResolveIssue(req.IssueID)
	if err != nil {
		s.logger.Printf("error getting issue %s: %v", req.IssueID, err)
		encoder.Encode(GetIssueResponse{OK: false, Error: "not_found"})
		return
	}

	encoder.Encode(GetIssueResponse{
		OK:    true,
		Issue: IssueToFull(issue),
	})
}

func (s *SocketServer) handleStopRun(req SendRequest, encoder *json.Encoder) {
	if req.All {
		runs, err := s.store.ListRuns(&store.ListRunsFilter{
			Status: []model.Status{model.StatusRunning, model.StatusBooting},
		})
		if err != nil {
			encoder.Encode(StopRunResponse{OK: false, Error: "store_error"})
			return
		}

		stopped := 0
		for _, run := range runs {
			if s.stopSingleRun(run) {
				stopped++
			}
		}

		encoder.Encode(StopRunResponse{OK: true, Stopped: stopped})
		return
	}

	if req.IssueID == "" {
		encoder.Encode(StopRunResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	if req.RunID == "" {
		runs, err := s.store.ListRuns(&store.ListRunsFilter{
			IssueID: req.IssueID,
			Status:  []model.Status{model.StatusRunning, model.StatusBooting, model.StatusBlocked, model.StatusBlockedAPI, model.StatusQueued},
		})
		if err != nil {
			encoder.Encode(StopRunResponse{OK: false, Error: "store_error"})
			return
		}

		stopped := 0
		for _, run := range runs {
			if s.stopSingleRun(run) {
				stopped++
			}
		}

		encoder.Encode(StopRunResponse{OK: true, Stopped: stopped})
		return
	}

	ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
	run, err := s.store.GetRun(ref)
	if err != nil {
		encoder.Encode(StopRunResponse{OK: false, Error: "not_found"})
		return
	}

	if s.stopSingleRun(run) {
		encoder.Encode(StopRunResponse{OK: true, Stopped: 1})
	} else {
		encoder.Encode(StopRunResponse{OK: true, Stopped: 0})
	}
}

func (s *SocketServer) stopSingleRun(run *model.Run) bool {
	if run.Status == model.StatusDone || run.Status == model.StatusFailed || run.Status == model.StatusCanceled {
		return false
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	if hasSession(sessionName) {
		if err := killSession(sessionName); err != nil {
			s.logger.Printf("warning: failed to kill session %s: %v", sessionName, err)
		}
	}

	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	event := &model.Event{
		Timestamp: time.Now(),
		Type:      "status",
		Name:      string(model.StatusCanceled),
	}

	if err := s.store.AppendEvent(ref, event); err != nil {
		s.logger.Printf("failed to append canceled event: %v", err)
		return false
	}

	return true
}

func (s *SocketServer) handleResolveIssue(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(ResolveIssueResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	issue, err := s.store.ResolveIssue(req.IssueID)
	if err != nil {
		encoder.Encode(ResolveIssueResponse{OK: false, Error: "not_found"})
		return
	}

	if issue.Status == model.IssueStatusResolved {
		encoder.Encode(ResolveIssueResponse{OK: true})
		return
	}

	if !req.Force {
		filter := store.ListRunsFilter{IssueID: req.IssueID}
		runs, err := s.store.ListRuns(&filter)
		if err != nil {
			encoder.Encode(ResolveIssueResponse{OK: false, Error: "store_error"})
			return
		}

		hasCompletedRun := false
		for _, run := range runs {
			if run.Status == model.StatusDone || run.Status == model.StatusPROpen {
				hasCompletedRun = true
				break
			}
		}

		if !hasCompletedRun {
			encoder.Encode(ResolveIssueResponse{OK: false, Error: "no_completed_runs"})
			return
		}
	}

	if err := s.store.SetIssueStatus(req.IssueID, model.IssueStatusResolved); err != nil {
		encoder.Encode(ResolveIssueResponse{OK: false, Error: "store_error"})
		return
	}

	encoder.Encode(ResolveIssueResponse{OK: true})
}

func (s *SocketServer) handleCreateIssue(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	issuesDir := resolveIssuesDir(s.vaultPath)
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "failed_to_create_dir"})
		return
	}

	issuePath := filepath.Join(issuesDir, req.IssueID+".md")

	if _, err := os.Stat(issuePath); err == nil {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "issue_exists"})
		return
	}

	title := req.Title
	if title == "" {
		title = req.IssueID
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("type: issue\n")
	sb.WriteString(fmt.Sprintf("id: %s\n", req.IssueID))
	sb.WriteString(fmt.Sprintf("title: %s\n", title))
	if req.Summary != "" {
		sb.WriteString(fmt.Sprintf("summary: %s\n", req.Summary))
	}
	sb.WriteString("status: open\n")
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))

	if req.Body != "" {
		sb.WriteString(req.Body)
		sb.WriteString("\n")
	}

	if err := os.WriteFile(issuePath, []byte(sb.String()), 0644); err != nil {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "write_failed"})
		return
	}

	encoder.Encode(CreateIssueResponse{
		OK:      true,
		IssueID: req.IssueID,
		Path:    issuePath,
	})
}

func (s *SocketServer) handleAttachInfo(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(AttachInfoResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
	run, err := s.store.GetRun(ref)
	if err != nil {
		encoder.Encode(AttachInfoResponse{OK: false, Error: "not_found"})
		return
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	sessionExists := hasSession(sessionName)
	canAutoCreate := !sessionExists && run.WorktreePath != ""

	encoder.Encode(AttachInfoResponse{
		OK:            true,
		Agent:         run.Agent,
		TmuxSession:   sessionName,
		ServerPort:    run.ServerPort,
		SessionID:     run.OpenCodeSessionID,
		WorktreePath:  run.WorktreePath,
		SessionExists: sessionExists,
		CanAutoCreate: canAutoCreate,
	})
}

func resolveIssuesDir(vaultPath string) string {
	if strings.EqualFold(filepath.Base(vaultPath), "issues") {
		return vaultPath
	}

	issuesDir := filepath.Join(vaultPath, "issues")
	if dirExists(issuesDir) {
		return issuesDir
	}

	issuesDir = filepath.Join(vaultPath, "Issues")
	if dirExists(issuesDir) {
		return issuesDir
	}

	return filepath.Join(vaultPath, "issues")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func hasSession(name string) bool {
	return tmux.HasSession(name)
}

func killSession(name string) error {
	return tmux.KillSession(name)
}

func SendViaDaemon(vaultPath string, run *model.Run, message string, noEnter bool) error {
	socketPath := SocketFilePath(vaultPath)

	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	req := SendRequest{
		Type:      "send",
		IssueID:   run.IssueID,
		RunID:     run.RunID,
		Message:   message,
		NoEnter:   noEnter,
		VaultPath: vaultPath,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	decoder := json.NewDecoder(conn)
	var resp SendResponse
	if err := decoder.Decode(&resp); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	return nil
}

func IsDaemonSocketAvailable(vaultPath string) bool {
	socketPath := SocketFilePath(vaultPath)
	_, err := os.Stat(socketPath)
	return err == nil
}
