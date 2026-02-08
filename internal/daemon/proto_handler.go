package daemon

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/pr"
	"github.com/s22625/orch/internal/store"
	"google.golang.org/protobuf/proto"
)

func generateMonitorID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

const (
	maxProtoMessageSize = 10 * 1024 * 1024
)

func (s *SocketServer) handleProtoConnection(conn net.Conn, data []byte) {
	defer conn.Close()

	msgLen := binary.BigEndian.Uint32(data[:4])
	if msgLen > maxProtoMessageSize {
		s.sendProtoError(conn, "message too large")
		return
	}

	msgData := make([]byte, msgLen)
	if len(data) > 4 {
		copy(msgData, data[4:])
	}

	remaining := int(msgLen) - (len(data) - 4)
	if remaining > 0 {
		_, err := io.ReadFull(conn, msgData[len(data)-4:])
		if err != nil {
			s.logger.Printf("failed to read proto message: %v", err)
			return
		}
	}

	var req orchpb.Request
	if err := proto.Unmarshal(msgData, &req); err != nil {
		s.logger.Printf("failed to unmarshal proto request: %v", err)
		s.sendProtoError(conn, "invalid request")
		return
	}

	resp := s.handleProtoRequest(&req)
	s.sendProtoResponse(conn, resp)
}

func (s *SocketServer) handleProtoRequest(req *orchpb.Request) *orchpb.Response {
	switch r := req.Request.(type) {
	case *orchpb.Request_Ping:
		return s.handleProtoPing(r.Ping)
	case *orchpb.Request_ListRuns:
		return s.handleProtoListRuns(r.ListRuns)
	case *orchpb.Request_GetRun:
		return s.handleProtoGetRun(r.GetRun)
	case *orchpb.Request_StartRun:
		return s.handleProtoStartRun(r.StartRun)
	case *orchpb.Request_StopRun:
		return s.handleProtoStopRun(r.StopRun)
	case *orchpb.Request_ResolveRun:
		return s.handleProtoResolveRun(r.ResolveRun)
	case *orchpb.Request_ListIssues:
		return s.handleProtoListIssues(r.ListIssues)
	case *orchpb.Request_GetIssue:
		return s.handleProtoGetIssue(r.GetIssue)
	case *orchpb.Request_CreateIssue:
		return s.handleProtoCreateIssue(r.CreateIssue)
	case *orchpb.Request_CloseIssue:
		return s.handleProtoCloseIssue(r.CloseIssue)
	case *orchpb.Request_GetControlAgentLaunch:
		return s.handleProtoGetControlAgentLaunch(r.GetControlAgentLaunch)
	case *orchpb.Request_GetAttachInfo:
		return s.handleProtoGetAttachInfo(r.GetAttachInfo)
	case *orchpb.Request_CaptureSession:
		return s.handleProtoCaptureSession(r.CaptureSession)
	case *orchpb.Request_SendMessage:
		return s.handleProtoSendMessage(r.SendMessage)
	case *orchpb.Request_GetDiffStats:
		return s.handleProtoGetDiffStats(r.GetDiffStats)
	case *orchpb.Request_GetBranchState:
		return s.handleProtoGetBranchState(r.GetBranchState)
	case *orchpb.Request_GetDiff:
		return s.handleProtoGetDiff(r.GetDiff)
	case *orchpb.Request_RegisterMonitor:
		return s.handleProtoRegisterMonitor(r.RegisterMonitor)
	case *orchpb.Request_UnregisterMonitor:
		return s.handleProtoUnregisterMonitor(r.UnregisterMonitor)
	case *orchpb.Request_Heartbeat:
		return s.handleProtoHeartbeat(r.Heartbeat)
	case *orchpb.Request_ListMonitors:
		return s.handleProtoListMonitors(r.ListMonitors)
	case *orchpb.Request_KillMonitor:
		return s.handleProtoKillMonitor(r.KillMonitor)
	case *orchpb.Request_GetRunByShortId:
		return s.handleProtoGetRunByShortID(r.GetRunByShortId)
	case *orchpb.Request_ResolveIssue:
		return s.handleProtoResolveIssue(r.ResolveIssue)
	case *orchpb.Request_AppendEvent:
		return s.handleProtoAppendEvent(r.AppendEvent)
	case *orchpb.Request_EnsureOpencodeServer:
		return s.handleProtoEnsureOpenCodeServer(r.EnsureOpencodeServer)
	case *orchpb.Request_RegisterRepo:
		return s.handleProtoRegisterRepo(r.RegisterRepo)
	case *orchpb.Request_ListRepos:
		return s.handleProtoListRepos(r.ListRepos)
	case *orchpb.Request_DeleteRun:
		return s.handleProtoDeleteRun(r.DeleteRun)
	case *orchpb.Request_UpdateIssue:
		return s.handleProtoUpdateIssue(r.UpdateIssue)
	case *orchpb.Request_ValidateIssueFiles:
		return s.handleProtoValidateIssueFiles(r.ValidateIssueFiles)
	case *orchpb.Request_WriteAgentPrompt:
		return s.handleProtoWriteAgentPrompt(r.WriteAgentPrompt)
	case *orchpb.Request_ReadAgentPrompt:
		return s.handleProtoReadAgentPrompt(r.ReadAgentPrompt)
	case *orchpb.Request_RepairState:
		return s.handleProtoRepairState(r.RepairState)
	case *orchpb.Request_GetDaemonLog:
		return s.handleProtoGetDaemonLog(r.GetDaemonLog)
	case *orchpb.Request_ReadFile:
		return s.handleProtoReadFile(r.ReadFile)
	case *orchpb.Request_WriteFile:
		return s.handleProtoWriteFile(r.WriteFile)
	case *orchpb.Request_CreateRun:
		return s.handleProtoCreateRun(r.CreateRun)
	case *orchpb.Request_KillSession:
		return s.handleProtoKillSession(r.KillSession)
	case *orchpb.Request_ListSessions:
		return s.handleProtoListSessions(r.ListSessions)
	case *orchpb.Request_ResumeRun:
		return s.handleProtoResumeRun(r.ResumeRun)
	case *orchpb.Request_QueryOpencodeServer:
		return s.handleProtoQueryOpenCodeServer(r.QueryOpencodeServer)
	case *orchpb.Request_InjectInitialPrompt:
		return s.handleProtoInjectInitialPrompt(r.InjectInitialPrompt)
	default:
		return errorResponse("unknown request type")
	}
}

func (s *SocketServer) sendProtoResponse(conn net.Conn, resp *orchpb.Response) {
	data, err := proto.Marshal(resp)
	if err != nil {
		s.logger.Printf("failed to marshal proto response: %v", err)
		return
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	conn.Write(lenBuf)
	conn.Write(data)
}

func (s *SocketServer) sendProtoError(conn net.Conn, errMsg string) {
	s.sendProtoResponse(conn, errorResponse(errMsg))
}

func errorResponse(errMsg string) *orchpb.Response {
	return &orchpb.Response{Ok: false, Error: errMsg}
}

func (s *SocketServer) resolveStoreFromProto(issuesRoot string) store.Store {
	if issuesRoot != "" {
		return s.getOrCreateStore(issuesRoot, "")
	}
	s.reposMu.RLock()
	defer s.reposMu.RUnlock()
	for _, ctx := range s.repos {
		if ctx.Store != nil {
			return ctx.Store
		}
	}
	return nil
}

func (s *SocketServer) handleProtoPing(_ *orchpb.PingRequest) *orchpb.Response {
	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_Ping{
			Ping: &orchpb.PingResponse{Ok: true, Version: "1.0.0"},
		},
	}
}

func (s *SocketServer) handleProtoListRuns(req *orchpb.ListRunsRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	filter := &store.ListRunsFilter{
		IssueID:    req.IssueId,
		Status:     protoRunStatusSliceToModel(req.Status),
		Agent:      req.Agent,
		TextSearch: req.TextSearch,
		TimeRange:  req.TimeRange,
		Limit:      int(req.Limit),
		OlderThan:  req.OlderThan,
	}

	runs, err := st.ListRuns(filter)
	if err != nil {
		return errorResponse("store_error")
	}

	protoRuns := make([]*orchpb.Run, len(runs))
	protoRuns = enrichRunsParallel(runs, protoRuns)

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ListRuns{
			ListRuns: &orchpb.ListRunsResponse{
				Runs:  protoRuns,
				Total: int32(len(runs)),
			},
		},
	}
}

func enrichRunProto(pr *orchpb.Run, run *model.Run) {
	if run.WorktreePath != "" && run.Branch != "" {
		pr.BranchState = computeBranchState(run.WorktreePath, run.Branch, "main")
		stats := git.GetDiffStats(run.WorktreePath, run.Branch, "main")
		if stats.Additions > 0 || stats.Deletions > 0 || stats.FilesChanged > 0 {
			pr.DiffStats = &orchpb.DiffStats{
				Additions:    int32(stats.Additions),
				Deletions:    int32(stats.Deletions),
				FilesChanged: int32(stats.FilesChanged),
				Files:        stats.Files,
			}
		}
	}
	pr.ElapsedDisplay = formatElapsedTime(run.StartedAt, run.UpdatedAt, run.Status)
}

func enrichRunsParallel(runs []*model.Run, protoRuns []*orchpb.Run) []*orchpb.Run {
	if len(runs) == 0 {
		return protoRuns
	}

	worktrees := make([]struct {
		Path       string
		Branch     string
		BaseBranch string
	}, 0, len(runs))

	for _, run := range runs {
		worktrees = append(worktrees, struct {
			Path       string
			Branch     string
			BaseBranch string
		}{
			Path:       run.WorktreePath,
			Branch:     run.Branch,
			BaseBranch: "main",
		})
	}

	statusMap := git.GetCachedWorktreeStatusBatch(worktrees)

	prInfoMap := pr.PopulateRunInfo(runs)

	for i, run := range runs {
		proto := modelRunToProto(run)
		proto.ElapsedDisplay = formatElapsedTime(run.StartedAt, run.UpdatedAt, run.Status)

		if status, ok := statusMap[run.WorktreePath]; ok {
			switch status.State {
			case git.BranchStateDirty:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_DIRTY
			case git.BranchStateMerged:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_MERGED
			case git.BranchStateClean:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_CLEAN
			case git.BranchStateAhead:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_AHEAD
			case git.BranchStateBehind:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_BEHIND
			case git.BranchStateDiverged:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_DIVERGED
			case git.BranchStateConflict:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_CONFLICT
			case git.BranchStateSynced:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_SYNCED
			default:
				proto.BranchState = orchpb.BranchState_BRANCH_STATE_UNSPECIFIED
			}

			if status.DiffStats.Additions > 0 || status.DiffStats.Deletions > 0 || status.DiffStats.FilesChanged > 0 {
				proto.DiffStats = &orchpb.DiffStats{
					Additions:    int32(status.DiffStats.Additions),
					Deletions:    int32(status.DiffStats.Deletions),
					FilesChanged: int32(status.DiffStats.FilesChanged),
					Files:        status.DiffStats.Files,
				}
			}
		}

		if prInfo, ok := prInfoMap[run.Branch]; ok && prInfo != nil {
			proto.PrNumber = int32(prInfo.Number)
			proto.PrState = strings.ToLower(prInfo.State)
		}

		protoRuns[i] = proto
	}

	return protoRuns
}

func lookupPRInfoForRun(run *model.Run) (prNumber int, prState string) {
	if run.PRUrl != "" {
		prInfo, err := pr.LookupInfoByURL(run.PRUrl)
		if err == nil && prInfo != nil {
			return prInfo.Number, strings.ToLower(prInfo.State)
		}
	}

	if run.Branch == "" {
		return 0, ""
	}

	var repoRoot string
	var err error
	if run.WorktreePath != "" {
		repoRoot, err = git.FindMainRepoRoot(run.WorktreePath)
	}
	if repoRoot == "" || err != nil {
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return 0, ""
		}
	}

	prInfo, err := pr.LookupInfo(repoRoot, run.Branch)
	if err != nil || prInfo == nil {
		return 0, ""
	}

	return prInfo.Number, strings.ToLower(prInfo.State)
}

func formatElapsedTime(startedAt, updatedAt time.Time, status model.Status) string {
	if startedAt.IsZero() {
		return "-"
	}

	var elapsed time.Duration
	if status == model.StatusRunning || status == model.StatusBlocked || status == model.StatusBlockedAPI {
		elapsed = time.Since(startedAt)
	} else if !updatedAt.IsZero() {
		elapsed = updatedAt.Sub(startedAt)
	} else {
		elapsed = time.Since(startedAt)
	}

	hours := int(elapsed.Hours())
	minutes := int(elapsed.Minutes()) % 60
	seconds := int(elapsed.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (s *SocketServer) handleProtoGetRun(req *orchpb.GetRunRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	run, err := st.GetRun(ref)
	if err != nil {
		return errorResponse("not_found")
	}

	protoEvents := make([]*orchpb.Event, len(run.Events))
	for i, e := range run.Events {
		protoEvents[i] = modelEventToProto(e)
	}

	pr := modelRunToProto(run)
	enrichRunProto(pr, run)

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetRun{
			GetRun: &orchpb.GetRunResponse{
				Run:    pr,
				Events: protoEvents,
			},
		},
	}
}

func (s *SocketServer) handleProtoStartRun(req *orchpb.StartRunRequest) *orchpb.Response {
	jsonReq := SendRequest{
		Type:           "start_run",
		IssueID:        req.IssueId,
		RunID:          req.RunId,
		AgentType:      req.Agent,
		Message:        req.Model,
		ModelVariant:   req.ModelVariant,
		BaseBranch:     req.BaseBranch,
		Branch:         req.Branch,
		WorktreeDir:    req.WorktreeDir,
		NoPR:           req.NoPr,
		PromptTemplate: req.PromptTemplate,
		PRTargetBranch: req.PrTargetBranch,
		DryRun:         req.DryRun,
		Reuse:          req.Reuse,
		AgentCmd:       req.AgentCmd,
		AgentProfile:   req.AgentProfile,
		Multiplexer:    req.Multiplexer,
		IssuesRoot:     req.IssuesRoot,
		ProjectRoot:    req.ProjectRoot,
	}

	var buf jsonCapture
	encoder := json.NewEncoder(&buf)
	s.handleStartRun(jsonReq, encoder)

	var result StartRunResponse
	if err := json.Unmarshal(buf.data, &result); err != nil {
		return errorResponse("internal error")
	}

	if !result.OK {
		return errorResponse(result.Error)
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_StartRun{
			StartRun: &orchpb.StartRunResponse{
				RunId:        result.RunID,
				Branch:       result.Branch,
				WorktreePath: result.WorktreePath,
				TmuxSession:  result.TmuxSession,
				Status:       result.Status,
			},
		},
	}
}

type jsonCapture struct {
	data []byte
}

func (j *jsonCapture) Write(p []byte) (n int, err error) {
	j.data = append(j.data, p...)
	return len(p), nil
}

func (s *SocketServer) handleProtoStopRun(req *orchpb.StopRunRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	run, err := st.GetRun(ref)
	if err != nil {
		return errorResponse("not_found")
	}

	if err := s.stopSingleRun(run, st); err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok:       true,
		Response: &orchpb.Response_StopRun{StopRun: &orchpb.StopRunResponse{}},
	}
}

func (s *SocketServer) handleProtoResolveRun(req *orchpb.ResolveRunRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	if err := st.SetIssueStatus(req.IssueId, model.IssueStatusResolved); err != nil {
		return errorResponse("store_error")
	}

	return &orchpb.Response{
		Ok:       true,
		Response: &orchpb.Response_ResolveRun{ResolveRun: &orchpb.ResolveRunResponse{}},
	}
}

func (s *SocketServer) handleProtoListIssues(req *orchpb.ListIssuesRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	issues, err := st.ListIssues()
	if err != nil {
		return errorResponse("store_error")
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ModifiedAt.After(issues[j].ModifiedAt)
	})

	if len(req.Status) > 0 {
		statusSet := make(map[model.IssueStatus]bool)
		for _, st := range req.Status {
			statusSet[protoIssueStatusToModel(st)] = true
		}
		var filtered []*model.Issue
		for _, issue := range issues {
			if statusSet[issue.Status] {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	if len(req.Tags) > 0 {
		tagSet := make(map[string]bool)
		for _, t := range req.Tags {
			tagSet[strings.ToLower(t)] = true
		}
		var filtered []*model.Issue
		for _, issue := range issues {
			if matchesTags(issue.Tags, tagSet, req.TagsMode) {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	if req.TextSearch != "" {
		search := strings.ToLower(req.TextSearch)
		var filtered []*model.Issue
		for _, issue := range issues {
			if strings.Contains(strings.ToLower(issue.ID), search) ||
				strings.Contains(strings.ToLower(issue.Title), search) ||
				strings.Contains(strings.ToLower(issue.Summary), search) {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	total := len(issues)

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	offset, _ := DecodeCursor(req.Cursor)
	if offset > len(issues) {
		offset = len(issues)
	}
	end := offset + limit
	if end > len(issues) {
		end = len(issues)
	}
	paginatedIssues := issues[offset:end]

	protoIssues := make([]*orchpb.Issue, len(paginatedIssues))
	for i, issue := range paginatedIssues {
		protoIssues[i] = modelIssueToProto(issue)
	}

	var nextCursor string
	if end < total {
		nextCursor = EncodeCursor(end)
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ListIssues{
			ListIssues: &orchpb.ListIssuesResponse{
				Issues:     protoIssues,
				Total:      int32(total),
				NextCursor: nextCursor,
			},
		},
	}
}

func (s *SocketServer) handleProtoGetIssue(req *orchpb.GetIssueRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	issue, err := st.ResolveIssue(req.IssueId)
	if err != nil {
		return errorResponse("not_found")
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetIssue{
			GetIssue: &orchpb.GetIssueResponse{
				Issue: modelIssueToProto(issue),
			},
		},
	}
}

func (s *SocketServer) handleProtoCreateIssue(req *orchpb.CreateIssueRequest) *orchpb.Response {
	jsonReq := SendRequest{
		Type:       "create_issue",
		IssueID:    req.IssueId,
		Title:      req.Title,
		Body:       req.Body,
		IssuesRoot: req.IssuesRoot,
	}

	var buf jsonCapture
	encoder := json.NewEncoder(&buf)
	s.handleCreateIssue(jsonReq, encoder)

	var result CreateIssueResponse
	if err := json.Unmarshal(buf.data, &result); err != nil {
		return errorResponse("internal error")
	}

	if !result.OK {
		return errorResponse(result.Error)
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_CreateIssue{
			CreateIssue: &orchpb.CreateIssueResponse{
				Path: result.Path,
			},
		},
	}
}

func (s *SocketServer) handleProtoCloseIssue(req *orchpb.CloseIssueRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	if err := st.SetIssueStatus(req.IssueId, model.IssueStatusClosed); err != nil {
		return errorResponse("not_found")
	}

	return &orchpb.Response{
		Ok:       true,
		Response: &orchpb.Response_CloseIssue{CloseIssue: &orchpb.CloseIssueResponse{}},
	}
}

func (s *SocketServer) handleProtoGetControlAgentLaunch(req *orchpb.GetControlAgentLaunchRequest) *orchpb.Response {
	jsonReq := SendRequest{
		Type:        "get_control_agent_launch",
		ProjectRoot: req.ProjectRoot,
		AgentType:   req.Agent,
		Force:       req.NewSession,
	}

	var buf jsonCapture
	encoder := json.NewEncoder(&buf)
	s.handleGetControlAgentLaunch(jsonReq, encoder)

	var result map[string]interface{}
	if err := json.Unmarshal(buf.data, &result); err != nil {
		return errorResponse("internal error")
	}

	if ok, _ := result["ok"].(bool); !ok {
		errMsg, _ := result["error"].(string)
		return errorResponse(errMsg)
	}

	command, _ := result["command"].(string)
	promptFile, _ := result["prompt_file"].(string)
	port, _ := result["port"].(float64)
	sessionID, _ := result["session_id"].(string)

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetControlAgentLaunch{
			GetControlAgentLaunch: &orchpb.GetControlAgentLaunchResponse{
				Command:    command,
				PromptFile: promptFile,
				Port:       int32(port),
				SessionId:  sessionID,
			},
		},
	}
}

func (s *SocketServer) handleProtoGetAttachInfo(req *orchpb.GetAttachInfoRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	var run *model.Run
	var err error

	if req.ShortId != "" {
		run, err = st.GetRunByShortID(req.ShortId)
	} else {
		ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
		run, err = st.GetRun(ref)
	}
	if err != nil {
		return errorResponse("not_found")
	}

	attachInfo := &orchpb.GetAttachInfoResponse{
		Command:           []string{"orch", "attach", fmt.Sprintf("%s#%s", run.IssueID, run.RunID)},
		Multiplexer:       multiplexerToProto(run.Multiplexer),
		SessionName:       run.TmuxSession,
		WorktreePath:      run.WorktreePath,
		Agent:             run.Agent,
		ServerPort:        int32(run.ServerPort),
		OpencodeSessionId: run.OpenCodeSessionID,
		IssueId:           run.IssueID,
		RunId:             run.RunID,
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
		attachInfo.SessionName = sessionName
	}

	isOpenCode := run.Agent == "opencode"
	if isOpenCode {
		if run.ServerPort == 0 {
			return &orchpb.Response{
				Ok:    false,
				Error: "opencode_server_not_found",
				Response: &orchpb.Response_GetAttachInfo{
					GetAttachInfo: attachInfo,
				},
			}
		}
	} else {
		muxType, _ := multiplexer.ParseType(run.Multiplexer)
		mux, _ := multiplexer.GetMultiplexer(muxType)
		if mux != nil && !mux.HasSession(sessionName) {
			return &orchpb.Response{
				Ok:    false,
				Error: "session_not_found",
				Response: &orchpb.Response_GetAttachInfo{
					GetAttachInfo: attachInfo,
				},
			}
		}
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetAttachInfo{
			GetAttachInfo: attachInfo,
		},
	}
}

func (s *SocketServer) handleProtoCaptureSession(req *orchpb.CaptureSessionRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	run, err := st.GetRun(ref)
	if err != nil {
		return errorResponse("not_found")
	}

	var content string
	var source string

	if run.Agent == string(agent.AgentOpenCode) {
		content, source, err = s.captureOpenCodeSession(run)
		if err != nil {
			return errorResponse(err.Error())
		}
	} else {
		muxType, _ := multiplexer.ParseType(run.Multiplexer)
		mux, _ := multiplexer.GetMultiplexer(muxType)
		if mux != nil && run.TmuxSession != "" {
			content, _ = mux.CapturePane(run.TmuxSession, 100)
		}
		source = run.Multiplexer
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_CaptureSession{
			CaptureSession: &orchpb.CaptureSessionResponse{
				Content:       content,
				TimestampUnix: time.Now().Unix(),
				Source:        source,
			},
		},
	}
}

func (s *SocketServer) captureOpenCodeSession(run *model.Run) (string, string, error) {
	if run.ServerPort == 0 {
		return "", "", fmt.Errorf("run has no server port (may have ended)")
	}
	if run.OpenCodeSessionID == "" {
		return "", "", fmt.Errorf("run has no session ID")
	}

	client := agent.NewOpenCodeClient(run.ServerPort)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !client.IsServerRunning(ctx) {
		return "", "", fmt.Errorf("server not running (run may have ended)")
	}

	messages, err := client.GetMessages(ctx, run.OpenCodeSessionID, run.WorktreePath)
	if err != nil {
		return "", "", fmt.Errorf("failed to get messages: %w", err)
	}

	content := agent.FormatOpenCodeMessages(messages, 100)
	return content, "opencode", nil
}

func (s *SocketServer) handleProtoSendMessage(req *orchpb.SendMessageRequest) *orchpb.Response {
	jsonReq := SendRequest{
		Type:       "send",
		IssueID:    req.IssueId,
		RunID:      req.RunId,
		Message:    req.Message,
		IssuesRoot: req.IssuesRoot,
	}

	if err := s.processSend(jsonReq); err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok:       true,
		Response: &orchpb.Response_SendMessage{SendMessage: &orchpb.SendMessageResponse{}},
	}
}

func (s *SocketServer) handleProtoGetDiffStats(req *orchpb.GetDiffStatsRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	run, err := st.GetRun(ref)
	if err != nil {
		return errorResponse("not_found")
	}

	stats := git.GetDiffStats(run.WorktreePath, run.Branch, "main")

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetDiffStats{
			GetDiffStats: &orchpb.GetDiffStatsResponse{
				DiffStats: &orchpb.DiffStats{
					Additions:    int32(stats.Additions),
					Deletions:    int32(stats.Deletions),
					FilesChanged: int32(stats.FilesChanged),
					Files:        stats.Files,
				},
			},
		},
	}
}

func (s *SocketServer) handleProtoGetBranchState(req *orchpb.GetBranchStateRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	run, err := st.GetRun(ref)
	if err != nil {
		return errorResponse("not_found")
	}

	state := computeBranchState(run.WorktreePath, run.Branch, "main")

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetBranchState{
			GetBranchState: &orchpb.GetBranchStateResponse{
				State: state,
			},
		},
	}
}

func computeBranchState(worktreePath, branch, baseBranch string) orchpb.BranchState {
	if worktreePath == "" {
		return orchpb.BranchState_BRANCH_STATE_UNSPECIFIED
	}

	cmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return orchpb.BranchState_BRANCH_STATE_UNSPECIFIED
	}
	if len(output) > 0 {
		return orchpb.BranchState_BRANCH_STATE_DIRTY
	}

	cmd = exec.Command("git", "-C", worktreePath, "branch", "--merged", baseBranch, "--format=%(refname:short)")
	output, err = cmd.Output()
	if err == nil {
		lines := string(output)
		for _, line := range splitLines(lines) {
			if line == branch {
				return orchpb.BranchState_BRANCH_STATE_MERGED
			}
		}
	}

	return orchpb.BranchState_BRANCH_STATE_CLEAN
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				lines = append(lines, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func (s *SocketServer) handleProtoGetDiff(req *orchpb.GetDiffRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	run, err := st.GetRun(ref)
	if err != nil {
		return errorResponse("not_found")
	}

	var diff string
	if run.WorktreePath != "" && run.Branch != "" {
		cmd := exec.Command("git", "-C", run.WorktreePath, "diff", "main..."+run.Branch)
		output, err := cmd.Output()
		if err == nil {
			diff = string(output)
		}
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetDiff{
			GetDiff: &orchpb.GetDiffResponse{
				Diff: diff,
			},
		},
	}
}

func (s *SocketServer) handleProtoRegisterMonitor(req *orchpb.RegisterMonitorRequest) *orchpb.Response {
	conn := &MonitorConnection{
		ID:          generateMonitorID(),
		PID:         int(req.Pid),
		Type:        req.MonitorType,
		View:        req.View,
		StartedAt:   time.Now(),
		LastSeen:    time.Now(),
		Project:     req.Project,
		TmuxSession: req.SessionName,
	}

	s.monitorsMu.Lock()
	s.monitors[conn.ID] = conn
	s.monitorsMu.Unlock()

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_RegisterMonitor{
			RegisterMonitor: &orchpb.RegisterMonitorResponse{
				MonitorId: conn.ID,
			},
		},
	}
}

func (s *SocketServer) handleProtoUnregisterMonitor(req *orchpb.UnregisterMonitorRequest) *orchpb.Response {
	s.monitorsMu.Lock()
	delete(s.monitors, req.MonitorId)
	s.monitorsMu.Unlock()

	return &orchpb.Response{
		Ok:       true,
		Response: &orchpb.Response_UnregisterMonitor{UnregisterMonitor: &orchpb.UnregisterMonitorResponse{}},
	}
}

func (s *SocketServer) handleProtoHeartbeat(req *orchpb.HeartbeatRequest) *orchpb.Response {
	s.monitorsMu.Lock()
	if conn, ok := s.monitors[req.MonitorId]; ok {
		conn.LastSeen = time.Now()
	}
	s.monitorsMu.Unlock()

	return &orchpb.Response{
		Ok:       true,
		Response: &orchpb.Response_Heartbeat{Heartbeat: &orchpb.HeartbeatResponse{}},
	}
}

func (s *SocketServer) handleProtoListMonitors(req *orchpb.ListMonitorsRequest) *orchpb.Response {
	s.monitorsMu.RLock()
	defer s.monitorsMu.RUnlock()

	var monitors []*orchpb.MonitorInfo
	for _, conn := range s.monitors {
		if !req.All && req.Project != "" && conn.Project != req.Project {
			continue
		}
		monitors = append(monitors, &orchpb.MonitorInfo{
			Id:                conn.ID,
			Pid:               int32(conn.PID),
			Type:              conn.Type,
			View:              conn.View,
			Project:           conn.Project,
			SessionName:       conn.TmuxSession,
			StartedAtUnix:     conn.StartedAt.Unix(),
			LastHeartbeatUnix: conn.LastSeen.Unix(),
		})
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ListMonitors{
			ListMonitors: &orchpb.ListMonitorsResponse{
				Monitors: monitors,
			},
		},
	}
}

func (s *SocketServer) handleProtoKillMonitor(req *orchpb.KillMonitorRequest) *orchpb.Response {
	s.monitorsMu.Lock()
	defer s.monitorsMu.Unlock()

	var killedCount int32

	if req.MonitorId != "" {
		if _, ok := s.monitors[req.MonitorId]; ok {
			delete(s.monitors, req.MonitorId)
			killedCount = 1
		}
	} else if req.All {
		for id, conn := range s.monitors {
			if req.Global || (req.Project != "" && conn.Project == req.Project) || req.Project == "" {
				delete(s.monitors, id)
				killedCount++
			}
		}
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_KillMonitor{
			KillMonitor: &orchpb.KillMonitorResponse{
				KilledCount: killedCount,
			},
		},
	}
}

func (s *SocketServer) handleProtoGetRunByShortID(req *orchpb.GetRunByShortIDRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	run, err := st.GetRunByShortID(req.ShortId)
	if err != nil {
		return errorResponse("not_found")
	}

	pr := modelRunToProto(run)
	enrichRunProto(pr, run)

	protoEvents := make([]*orchpb.Event, len(run.Events))
	for i, e := range run.Events {
		protoEvents[i] = modelEventToProto(e)
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetRunByShortId{
			GetRunByShortId: &orchpb.GetRunByShortIDResponse{
				Run:    pr,
				Events: protoEvents,
			},
		},
	}
}

func (s *SocketServer) handleProtoResolveIssue(req *orchpb.ResolveIssueRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	if err := st.SetIssueStatus(req.IssueId, model.IssueStatusResolved); err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ResolveIssue{
			ResolveIssue: &orchpb.ResolveIssueResponse{
				IssueId: req.IssueId,
			},
		},
	}
}

func (s *SocketServer) handleProtoAppendEvent(req *orchpb.AppendEventRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	event := model.NewEvent(model.EventType(req.EventType), req.EventName, req.EventAttrs)

	if err := st.AppendEvent(ref, event); err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_AppendEvent{
			AppendEvent: &orchpb.AppendEventResponse{},
		},
	}
}

func (s *SocketServer) handleProtoEnsureOpenCodeServer(req *orchpb.EnsureOpenCodeServerRequest) *orchpb.Response {
	port, err := s.ensureOpenCodeServerRunning(req.ProjectRoot)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to ensure opencode server: %v", err))
	}

	s.openCodeServersMu.RLock()
	srv, exists := s.openCodeServers[req.ProjectRoot]
	alreadyRunning := exists && srv != nil
	s.openCodeServersMu.RUnlock()

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_EnsureOpencodeServer{
			EnsureOpencodeServer: &orchpb.EnsureOpenCodeServerResponse{
				Port:           int32(port),
				AlreadyRunning: alreadyRunning,
			},
		},
	}
}

func (s *SocketServer) handleProtoRegisterRepo(req *orchpb.RegisterRepoRequest) *orchpb.Response {
	if req.ProjectRoot == "" {
		return errorResponse("project_root required")
	}

	repoID := deriveRepoID(req.ProjectRoot)

	s.reposMu.Lock()
	if _, exists := s.repos[repoID]; !exists {
		s.repos[repoID] = &RepoContext{
			ProjectRoot: req.ProjectRoot,
			RepoID:      repoID,
		}
	}
	s.reposMu.Unlock()

	s.logger.Printf("registered repo: %s (%s)", repoID, req.ProjectRoot)

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_RegisterRepo{
			RegisterRepo: &orchpb.RegisterRepoResponse{
				RepoId: repoID,
			},
		},
	}
}

func (s *SocketServer) handleProtoListRepos(_ *orchpb.ListReposRequest) *orchpb.Response {
	s.reposMu.RLock()
	defer s.reposMu.RUnlock()

	repos := make([]*orchpb.RepoInfo, 0, len(s.repos))
	for id, info := range s.repos {
		issuesRoot := ""
		if info.Store != nil {
			issuesRoot = info.Store.RootPath()
		}
		repos = append(repos, &orchpb.RepoInfo{
			Id:          id,
			ProjectRoot: info.ProjectRoot,
			IssuesRoot:  issuesRoot,
		})
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ListRepos{
			ListRepos: &orchpb.ListReposResponse{
				Repos: repos,
			},
		},
	}
}

func (s *SocketServer) handleProtoDeleteRun(req *orchpb.DeleteRunRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	var run *model.Run
	var err error

	if req.ShortId != "" {
		run, err = st.GetRunByShortID(req.ShortId)
	} else {
		ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
		run, err = st.GetRun(ref)
	}
	if err != nil {
		return errorResponse(fmt.Sprintf("run not found: %v", err))
	}

	result := &orchpb.DeleteRunResponse{
		IssueId: run.IssueID,
		RunId:   run.RunID,
		ShortId: run.ShortID(),
	}

	if req.WithWorktree && run.WorktreePath != "" {
		repoRoot, err := git.FindRepoRoot("")
		if err == nil {
			if git.RemoveWorktree(repoRoot, run.WorktreePath) == nil {
				result.WorktreeRemoved = true
			}
		}
	}

	if req.WithBranch && run.Branch != "" {
		repoRoot, err := git.FindRepoRoot("")
		if err == nil {
			cmd := exec.Command("git", "-C", repoRoot, "branch", "-D", run.Branch)
			if cmd.Run() == nil {
				result.BranchRemoved = true
			}
		}
	}

	if err := st.DeleteRun(&model.RunRef{IssueID: run.IssueID, RunID: run.RunID}); err != nil {
		return errorResponse(fmt.Sprintf("failed to delete run: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_DeleteRun{
			DeleteRun: result,
		},
	}
}

func (s *SocketServer) handleProtoUpdateIssue(req *orchpb.UpdateIssueRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	issue, err := st.ResolveIssue(req.IssueId)
	if err != nil {
		return errorResponse(fmt.Sprintf("issue not found: %v", err))
	}

	if req.Title != "" {
		issue.Title = req.Title
	}
	if req.Summary != "" {
		issue.Summary = req.Summary
	}
	if req.Body != "" {
		issue.Body = req.Body
	}
	if req.Status != "" {
		issue.Status = model.IssueStatus(req.Status)
	}

	if err := st.UpdateIssue(issue); err != nil {
		return errorResponse(fmt.Sprintf("failed to update issue: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_UpdateIssue{
			UpdateIssue: &orchpb.UpdateIssueResponse{
				Issue: modelIssueToProto(issue),
			},
		},
	}
}

func (s *SocketServer) handleProtoValidateIssueFiles(req *orchpb.ValidateIssueFilesRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	result, err := st.ValidateIssueFiles(req.IssueId)
	if err != nil {
		return errorResponse(fmt.Sprintf("validation failed: %v", err))
	}

	protoResult := &orchpb.ValidateIssueFilesResponse{
		Total: int32(result.Total),
		Valid: int32(result.Valid),
	}

	for _, e := range result.Errors {
		item := &orchpb.ValidationResultItem{
			File:    e.File,
			IssueId: e.IssueID,
		}
		for _, issue := range e.Errors {
			item.Errors = append(item.Errors, &orchpb.ValidationIssue{
				Code:    issue.Code,
				Message: issue.Message,
				Line:    int32(issue.Line),
				Level:   string(issue.Level),
			})
		}
		protoResult.Errors = append(protoResult.Errors, item)
	}

	for _, d := range result.Duplicates {
		protoResult.Duplicates = append(protoResult.Duplicates, &orchpb.DuplicateIDItem{
			Id:    d.ID,
			Files: d.Files,
		})
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ValidateIssueFiles{
			ValidateIssueFiles: protoResult,
		},
	}
}

func (s *SocketServer) handleProtoWriteAgentPrompt(req *orchpb.WriteAgentPromptRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	var run *model.Run
	var err error

	if req.ShortId != "" {
		run, err = st.GetRunByShortID(req.ShortId)
	} else {
		ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
		run, err = st.GetRun(ref)
	}
	if err != nil {
		return errorResponse(fmt.Sprintf("run not found: %v", err))
	}

	if err := st.WriteAgentPrompt(&model.RunRef{IssueID: run.IssueID, RunID: run.RunID}, req.Content); err != nil {
		return errorResponse(fmt.Sprintf("failed to write agent prompt: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_WriteAgentPrompt{
			WriteAgentPrompt: &orchpb.WriteAgentPromptResponse{},
		},
	}
}

func (s *SocketServer) handleProtoReadAgentPrompt(req *orchpb.ReadAgentPromptRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	var run *model.Run
	var err error

	if req.ShortId != "" {
		run, err = st.GetRunByShortID(req.ShortId)
	} else {
		ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
		run, err = st.GetRun(ref)
	}
	if err != nil {
		return errorResponse(fmt.Sprintf("run not found: %v", err))
	}

	content, err := st.ReadAgentPrompt(&model.RunRef{IssueID: run.IssueID, RunID: run.RunID})
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to read agent prompt: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ReadAgentPrompt{
			ReadAgentPrompt: &orchpb.ReadAgentPromptResponse{
				Content: content,
			},
		},
	}
}

func (s *SocketServer) handleProtoRepairState(req *orchpb.RepairStateRequest) *orchpb.Response {
	result := &orchpb.RepairStateResponse{}

	if err := CleanupStaleRegistrations(); err != nil {
		result.Details = append(result.Details, fmt.Sprintf("registry cleanup error: %v", err))
	}

	infos, _ := ListAllDaemons()
	for _, info := range infos {
		if !info.IsHealthy {
			result.ProblemsFound++
			result.Details = append(result.Details, fmt.Sprintf("unhealthy daemon: pid=%d project=%s", info.PID, info.ProjectRoot))
			if !req.DryRun && req.Force {
				if err := KillDaemon(info.ProjectRoot); err == nil {
					result.ProblemsFixed++
					result.Details = append(result.Details, fmt.Sprintf("killed unhealthy daemon: %s", info.ProjectRoot))
				}
			}
		}
	}

	// Repair orphaned sessions
	orphaned := s.findOrphanedSessions()
	if len(orphaned) > 0 {
		result.ProblemsFound += int32(len(orphaned))
		for _, sessionName := range orphaned {
			result.Details = append(result.Details, fmt.Sprintf("orphaned session: %s", sessionName))
			if !req.DryRun && req.Force {
				mux := multiplexer.GetDefault()
				if err := mux.KillSession(sessionName); err == nil {
					result.ProblemsFixed++
					result.Details = append(result.Details, fmt.Sprintf("killed orphaned session: %s", sessionName))
				}
			}
		}
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_RepairState{
			RepairState: result,
		},
	}
}

func (s *SocketServer) findOrphanedSessions() []string {
	mux := multiplexer.GetDefault()
	sessions, err := mux.ListSessions()
	if err != nil || len(sessions) == 0 {
		return nil
	}

	st := s.resolveStoreFromProto("")
	if st == nil {
		return nil
	}

	runs, err := st.ListRuns(&store.ListRunsFilter{})
	if err != nil {
		return nil
	}

	expectedSessions := make(map[string]bool)
	for _, run := range runs {
		sessionName := run.TmuxSession
		if sessionName == "" {
			sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
		}
		expectedSessions[sessionName] = true
	}

	var orphaned []string
	for _, sess := range sessions {
		if len(sess) > 4 && sess[:4] == "run-" {
			if !expectedSessions[sess] {
				orphaned = append(orphaned, sess)
			}
		}
	}

	return orphaned
}

func (s *SocketServer) handleProtoGetDaemonLog(req *orchpb.GetDaemonLogRequest) *orchpb.Response {
	projectRoot := s.getFirstProjectRoot()
	logPath := LogFilePath(projectRoot)
	content, err := readLastNLines(logPath, int(req.Lines))
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to read daemon log: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetDaemonLog{
			GetDaemonLog: &orchpb.GetDaemonLogResponse{
				Content: content,
			},
		},
	}
}

func (s *SocketServer) getFirstProjectRoot() string {
	s.reposMu.RLock()
	defer s.reposMu.RUnlock()
	for _, ctx := range s.repos {
		if ctx.ProjectRoot != "" {
			return ctx.ProjectRoot
		}
	}
	return ""
}

func (s *SocketServer) handleProtoReadFile(req *orchpb.ReadFileRequest) *orchpb.Response {
	content, err := readFileContent(req.Path)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to read file: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ReadFile{
			ReadFile: &orchpb.ReadFileResponse{
				Content: content,
			},
		},
	}
}

func (s *SocketServer) handleProtoWriteFile(req *orchpb.WriteFileRequest) *orchpb.Response {
	if err := writeFileContent(req.Path, req.Content, req.Perm); err != nil {
		return errorResponse(fmt.Sprintf("failed to write file: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_WriteFile{
			WriteFile: &orchpb.WriteFileResponse{},
		},
	}
}

func (s *SocketServer) handleProtoCreateRun(req *orchpb.CreateRunRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	run, err := st.CreateRun(req.IssueId, req.RunId, req.Metadata)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to create run: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_CreateRun{
			CreateRun: &orchpb.CreateRunResponse{
				IssueId: run.IssueID,
				RunId:   run.RunID,
				Path:    run.Path,
			},
		},
	}
}

func (s *SocketServer) handleProtoInjectInitialPrompt(req *orchpb.InjectInitialPromptRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	run, err := st.GetRun(ref)
	if err != nil {
		return errorResponse(fmt.Sprintf("run not found: %v", err))
	}

	if req.Prompt == "" {
		return &orchpb.Response{
			Ok: true,
			Response: &orchpb.Response_InjectInitialPrompt{
				InjectInitialPrompt: &orchpb.InjectInitialPromptResponse{
					SessionId: run.TmuxSession,
					Port:      int32(run.ServerPort),
				},
			},
		}
	}

	if run.Agent == string(agent.AgentOpenCode) {
		if run.ServerPort <= 0 {
			return errorResponse(fmt.Sprintf("run %s missing server port (not running or server not started)", ref.String()))
		}
		if run.OpenCodeSessionID == "" {
			return errorResponse(fmt.Sprintf("run %s missing session ID (agent may still be booting)", ref.String()))
		}

		client := agent.NewOpenCodeClient(run.ServerPort)
		healthCtx, healthCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer healthCancel()

		if !client.IsServerRunning(healthCtx) {
			return errorResponse(fmt.Sprintf("opencode server not running for %s (port %d)", ref.String(), run.ServerPort))
		}

		sendCtx, sendCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer sendCancel()

		_, err := client.SendMessage(sendCtx, run.OpenCodeSessionID, req.Prompt)
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to send prompt to opencode: %v", err))
		}

		return &orchpb.Response{
			Ok: true,
			Response: &orchpb.Response_InjectInitialPrompt{
				InjectInitialPrompt: &orchpb.InjectInitialPromptResponse{
					SessionId: run.OpenCodeSessionID,
					Port:      int32(run.ServerPort),
				},
			},
		}
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	mux := multiplexer.GetDefault()
	if mux == nil {
		return errorResponse("no multiplexer available")
	}

	if !mux.HasSession(sessionName) {
		return errorResponse(fmt.Sprintf("session not found: %s", sessionName))
	}

	if err := mux.SendKeys(sessionName, req.Prompt); err != nil {
		return errorResponse(fmt.Sprintf("failed to send prompt to session: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_InjectInitialPrompt{
			InjectInitialPrompt: &orchpb.InjectInitialPromptResponse{
				SessionId: sessionName,
				Port:      int32(run.ServerPort),
			},
		},
	}
}

func readLastNLines(path string, n int) (string, error) {
	if n <= 0 {
		n = 100
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

func readFileContent(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func writeFileContent(path string, content []byte, perm uint32) error {
	if perm == 0 {
		perm = 0644
	}
	return os.WriteFile(path, content, os.FileMode(perm))
}

func (s *SocketServer) handleProtoKillSession(req *orchpb.KillSessionRequest) *orchpb.Response {
	mux := s.getMultiplexer(req.Multiplexer)
	err := mux.KillSession(req.SessionName)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to kill session: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_KillSession{
			KillSession: &orchpb.KillSessionResponse{
				Killed: true,
			},
		},
	}
}

func (s *SocketServer) handleProtoListSessions(req *orchpb.ListSessionsRequest) *orchpb.Response {
	mux := s.getMultiplexer(req.Multiplexer)
	sessions, err := mux.ListSessions()
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to list sessions: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ListSessions{
			ListSessions: &orchpb.ListSessionsResponse{
				Sessions: sessions,
			},
		},
	}
}

func (s *SocketServer) handleProtoResumeRun(req *orchpb.ResumeRunRequest) *orchpb.Response {
	st := s.resolveStoreFromProto(req.IssuesRoot)
	if st == nil {
		return errorResponse("no store available")
	}

	var run *model.Run
	var err error

	if req.ShortId != "" {
		run, err = st.GetRunByShortID(req.ShortId)
	} else {
		ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
		run, err = st.GetRun(ref)
	}
	if err != nil {
		return errorResponse(fmt.Sprintf("run not found: %v", err))
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	mux := multiplexer.GetDefault()
	if err := mux.SendKeys(sessionName, ""); err != nil {
		return errorResponse(fmt.Sprintf("failed to resume run: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ResumeRun{
			ResumeRun: &orchpb.ResumeRunResponse{
				SessionName: sessionName,
			},
		},
	}
}

func (s *SocketServer) handleProtoQueryOpenCodeServer(req *orchpb.QueryOpenCodeServerRequest) *orchpb.Response {
	port := int(req.Port)
	if port == 0 {
		return errorResponse("port required")
	}

	client := agent.NewOpenCodeClient(port)
	providersResp, err := client.GetProviders(context.Background())
	if err != nil {
		return &orchpb.Response{
			Ok: true,
			Response: &orchpb.Response_QueryOpencodeServer{
				QueryOpencodeServer: &orchpb.QueryOpenCodeServerResponse{
					ServerRunning: false,
				},
			},
		}
	}

	protoProviders := make([]*orchpb.OpenCodeProviderInfo, 0, len(providersResp.All))
	for _, p := range providersResp.All {
		protoModels := make([]*orchpb.OpenCodeModelInfo, 0, len(p.Models))
		for _, m := range p.Models {
			protoModels = append(protoModels, &orchpb.OpenCodeModelInfo{
				Id:       m.ID,
				Name:     m.Name,
				Variants: m.Variants,
			})
		}
		protoProviders = append(protoProviders, &orchpb.OpenCodeProviderInfo{
			Id:     p.ID,
			Name:   p.Name,
			Models: protoModels,
		})
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_QueryOpencodeServer{
			QueryOpencodeServer: &orchpb.QueryOpenCodeServerResponse{
				ServerRunning: true,
				Providers:     protoProviders,
			},
		},
	}
}

func (s *SocketServer) getMultiplexer(muxType orchpb.Multiplexer) multiplexer.Multiplexer {
	switch muxType {
	case orchpb.Multiplexer_MULTIPLEXER_TMUX:
		return multiplexer.NewTmuxMultiplexer()
	case orchpb.Multiplexer_MULTIPLEXER_ZELLIJ:
		return multiplexer.NewZellijMultiplexer()
	default:
		return multiplexer.GetDefault()
	}
}
