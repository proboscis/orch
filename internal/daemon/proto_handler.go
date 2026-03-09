package daemon

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/api/orchpb"
	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/pr"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/xdg"
	"google.golang.org/protobuf/proto"
)

func generateMonitorID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

const (
	maxProtoMessageSize = 10 * 1024 * 1024
	listRunsTimingEnv   = "ORCH_DAEMON_LISTRUNS_TIMING"
	protoConnIdleTTL    = 5 * time.Minute
)

const listRunsSlowThreshold = 250 * time.Millisecond

// ARCHITECTURE NOTE (orch-447): this handler must support multiple request/
// response exchanges on a single connection. If the daemon closes the
// connection after each request, clients are forced to reconnect constantly,
// which creates excessive socket lifecycle events and can exhaust memory in
// macOS security services.
func (s *SocketServer) handleProtoConnection(conn net.Conn) {
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("PANIC in handleProtoConnection: %v", r)
		}
	}()

	for {
		if err := conn.SetReadDeadline(time.Now().Add(protoConnIdleTTL)); err != nil {
			s.logger.Printf("failed to set read deadline: %v", err)
			return
		}

		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			if err == io.EOF {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return
			}
			s.logger.Printf("failed to read proto message length: %v", err)
			return
		}

		msgLen := binary.BigEndian.Uint32(lenBuf)
		if msgLen > maxProtoMessageSize {
			s.sendProtoError(conn, "message too large")
			return
		}

		msgData := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, msgData); err != nil {
			s.logger.Printf("failed to read proto message: %v", err)
			return
		}

		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			s.logger.Printf("failed to clear read deadline: %v", err)
			return
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
	case *orchpb.Request_GetControlAgentConfig:
		return s.handleProtoGetControlAgentConfig(r.GetControlAgentConfig)
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
	case *orchpb.Request_ContinueRun:
		return s.handleProtoContinueRun(r.ContinueRun)
	case *orchpb.Request_GetConfig:
		return s.handleProtoGetConfig(r.GetConfig)
	case *orchpb.Request_GetDaemonStatus:
		return s.handleProtoGetDaemonStatus(r.GetDaemonStatus)
	case *orchpb.Request_RegisterWorker:
		return s.handleProtoRegisterWorker(r.RegisterWorker)
	case *orchpb.Request_UnregisterWorker:
		return s.handleProtoUnregisterWorker(r.UnregisterWorker)
	case *orchpb.Request_WorkerHeartbeat:
		return s.handleProtoWorkerHeartbeat(r.WorkerHeartbeat)
	case *orchpb.Request_ListWorkers:
		return s.handleProtoListWorkers(r.ListWorkers)
	case *orchpb.Request_LeaseWork:
		return s.handleProtoLeaseWork(r.LeaseWork)
	case *orchpb.Request_AcknowledgeEffect:
		return s.handleProtoAcknowledgeEffect(r.AcknowledgeEffect)
	case *orchpb.Request_StartExternalWorker:
		return s.handleProtoStartExternalWorker(r.StartExternalWorker)
	case *orchpb.Request_StopExternalWorker:
		return s.handleProtoStopExternalWorker(r.StopExternalWorker)
	case *orchpb.Request_ListExternalWorkers:
		return s.handleProtoListExternalWorkers(r.ListExternalWorkers)
	default:
		return errorResponse("unknown request type")
	}
}

func (s *SocketServer) sendProtoResponse(conn net.Conn, resp *orchpb.Response) {
	data, err := proto.Marshal(resp)
	if err != nil {
		s.logger.Printf("failed to marshal proto response: %v", err)
		fallbackData, fallbackErr := proto.Marshal(errorResponse("response_encoding_error"))
		if fallbackErr != nil {
			s.logger.Printf("failed to marshal fallback proto response: %v", fallbackErr)
			return
		}
		data = fallbackData
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

func projectIDFromContext(ctx *orchpb.RequestContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.ProjectId)
}

func (s *SocketServer) resolveStoreFromContextOrProto(ctx *orchpb.RequestContext, _ string) store.Store {
	if projectID := projectIDFromContext(ctx); projectID != "" {
		if repo := s.ensureRepoStoreByID(projectID); repo != nil && repo.Store != nil {
			return repo.Store
		}
		return nil
	}

	return nil
}

func (s *SocketServer) resolveProjectRootFromContextOrProto(ctx *orchpb.RequestContext, _ string) string {
	if projectID := projectIDFromContext(ctx); projectID != "" {
		if repo := s.GetRepoContext(projectID); repo != nil && strings.TrimSpace(repo.ProjectRoot) != "" {
			return strings.TrimSpace(repo.ProjectRoot)
		}
		if repo := s.ensureRepoContextByID(projectID); repo != nil && strings.TrimSpace(repo.ProjectRoot) != "" {
			return strings.TrimSpace(repo.ProjectRoot)
		}
		return ""
	}

	return ""
}

func (s *SocketServer) resolveProtoProjectRoot(projectRoot string) string {
	if repoID, ok := decodeRepoIDToken(projectRoot); ok {
		if ctx := s.GetRepoContext(repoID); ctx != nil && strings.TrimSpace(ctx.ProjectRoot) != "" {
			return strings.TrimSpace(ctx.ProjectRoot)
		}
		if ctx := s.ensureRepoContextByID(repoID); ctx != nil {
			return strings.TrimSpace(ctx.ProjectRoot)
		}
		return ""
	}
	return strings.TrimSpace(projectRoot)
}

func (s *SocketServer) listStores() []store.Store {
	s.reposMu.RLock()
	defer s.reposMu.RUnlock()

	seen := make(map[store.Store]struct{})
	stores := make([]store.Store, 0, len(s.repos))
	for _, ctx := range s.repos {
		if ctx.Store == nil {
			continue
		}
		if _, ok := seen[ctx.Store]; ok {
			continue
		}
		seen[ctx.Store] = struct{}{}
		stores = append(stores, ctx.Store)
	}
	return stores
}

func isStoreNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

type runStoreEntry struct {
	run   *model.Run
	store store.Store
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
	requestStart := time.Now()
	projectID := projectIDFromContext(req.Context)

	filter := &store.ListRunsFilter{
		IssueID:    req.IssueId,
		Status:     protoRunStatusSliceToModel(req.Status),
		Agent:      req.Agent,
		TextSearch: req.TextSearch,
		TimeRange:  req.TimeRange,
		OlderThan:  req.OlderThan,
	}

	storeStart := time.Now()
	entries := make([]runStoreEntry, 0)
	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
		runs, err := st.ListRuns(filter)
		if err != nil {
			storeDuration := time.Since(storeStart)
			s.maybeLogListRunsTiming(req, 0, storeDuration, 0, time.Since(requestStart), err)
			return errorResponse("store_error")
		}
		for _, run := range runs {
			entries = append(entries, runStoreEntry{run: run, store: st})
		}
	} else {
		for _, st := range s.listStores() {
			runs, err := st.ListRuns(filter)
			if err != nil {
				storeDuration := time.Since(storeStart)
				s.maybeLogListRunsTiming(req, 0, storeDuration, 0, time.Since(requestStart), err)
				return errorResponse("store_error")
			}
			for _, run := range runs {
				entries = append(entries, runStoreEntry{run: run, store: st})
			}
		}
	}
	storeDuration := time.Since(storeStart)

	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].run
		right := entries[j].run
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		return left.UpdatedAt.After(right.UpdatedAt)
	})

	total := len(entries)

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	offset, _ := DecodeCursor(req.Cursor)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	paginatedEntries := entries[offset:end]

	paginatedRuns := make([]*model.Run, len(paginatedEntries))
	for i, entry := range paginatedEntries {
		paginatedRuns[i] = entry.run
	}

	enrichStart := time.Now()
	protoRuns := make([]*orchpb.Run, len(paginatedRuns))
	protoRuns = enrichRunsParallel(paginatedRuns, protoRuns)
	applyIssueMetadataToRunEntries(paginatedEntries, protoRuns)
	enrichDuration := time.Since(enrichStart)

	s.maybeLogListRunsTiming(req, len(paginatedRuns), storeDuration, enrichDuration, time.Since(requestStart), nil)

	var nextCursor string
	if end < total {
		nextCursor = EncodeCursor(end)
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ListRuns{
			ListRuns: &orchpb.ListRunsResponse{
				Runs:       protoRuns,
				Total:      int32(total),
				NextCursor: nextCursor,
			},
		},
	}
}

func applyIssueMetadataToRunEntries(entries []runStoreEntry, protoRuns []*orchpb.Run) {
	if len(entries) == 0 || len(protoRuns) == 0 {
		return
	}

	issuesByStore := make(map[store.Store]map[string]*model.Issue)

	for i, entry := range entries {
		if i >= len(protoRuns) || protoRuns[i] == nil || entry.run == nil || entry.store == nil {
			continue
		}

		byID, ok := issuesByStore[entry.store]
		if !ok {
			issues, err := entry.store.ListIssues()
			if err != nil {
				issuesByStore[entry.store] = nil
				continue
			}

			byID = make(map[string]*model.Issue, len(issues))
			for _, issue := range issues {
				if issue == nil {
					continue
				}
				byID[issue.ID] = issue
			}
			issuesByStore[entry.store] = byID
		}

		if byID == nil {
			continue
		}

		issue := byID[entry.run.IssueID]
		if issue == nil {
			continue
		}

		protoRuns[i].IssueStatus = sanitizeUTF8(string(issue.Status))
		if issue.Topic != "" {
			protoRuns[i].IssueTopic = sanitizeUTF8(issue.Topic)
		} else {
			protoRuns[i].IssueTopic = sanitizeUTF8(issue.Summary)
		}
	}
}

func daemonListRunsTimingEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(listRunsTimingEnv)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *SocketServer) maybeLogListRunsTiming(
	req *orchpb.ListRunsRequest,
	runCount int,
	storeDuration, enrichDuration, totalDuration time.Duration,
	err error,
) {
	slow := totalDuration >= listRunsSlowThreshold
	if !daemonListRunsTimingEnabled() && !slow {
		return
	}

	issueID := strings.TrimSpace(req.IssueId)
	hasTextSearch := strings.TrimSpace(req.TextSearch) != ""
	hasOlderThan := strings.TrimSpace(req.OlderThan) != ""

	if err != nil {
		s.logger.Printf(
			"list_runs timing total=%s store=%s enrich=%s runs=%d issue=%q statuses=%d limit=%d text_search=%t older_than=%t slow=%t error=%v",
			totalDuration,
			storeDuration,
			enrichDuration,
			runCount,
			issueID,
			len(req.Status),
			req.Limit,
			hasTextSearch,
			hasOlderThan,
			slow,
			err,
		)
		return
	}

	s.logger.Printf(
		"list_runs timing total=%s store=%s enrich=%s runs=%d issue=%q statuses=%d limit=%d text_search=%t older_than=%t slow=%t",
		totalDuration,
		storeDuration,
		enrichDuration,
		runCount,
		issueID,
		len(req.Status),
		req.Limit,
		hasTextSearch,
		hasOlderThan,
		slow,
	)
}

func enrichRunProto(pr *orchpb.Run, run *model.Run, runner git.Runner) {
	if run.WorktreePath != "" && run.Branch != "" {
		pr.BranchState = computeBranchState(run.WorktreePath, run.Branch, "main", runner)
		stats := git.GetDiffStats(run.WorktreePath, run.Branch, "main")
		if stats.Additions > 0 || stats.Deletions > 0 || stats.FilesChanged > 0 {
			pr.DiffStats = &orchpb.DiffStats{
				Additions:    int32(stats.Additions),
				Deletions:    int32(stats.Deletions),
				FilesChanged: int32(stats.FilesChanged),
				Files:        sanitizeUTF8Slice(stats.Files),
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
					Files:        sanitizeUTF8Slice(status.DiffStats.Files),
				}
			}
		}

		if prInfo, ok := prInfoMap[run.Branch]; ok && prInfo != nil {
			proto.PrNumber = int32(prInfo.Number)
			proto.PrState = sanitizeUTF8(strings.ToLower(prInfo.State))
		}

		protoRuns[i] = proto
	}

	return protoRuns
}

func formatElapsedTime(startedAt, updatedAt time.Time, status model.Status) string {
	if startedAt.IsZero() {
		return "-"
	}

	var elapsed time.Duration
	if status == model.StatusRunning || status == model.StatusWaiting || status == model.StatusRateLimited {
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
	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	projectID := projectIDFromContext(req.Context)

	var run *model.Run
	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		resolved, err := st.GetRun(ref)
		if err != nil {
			return errorResponse("not_found")
		}
		run = resolved
	} else {
		for _, st := range s.listStores() {
			resolved, err := st.GetRun(ref)
			if err != nil {
				if isStoreNotFoundError(err) {
					continue
				}
				return errorResponse("store_error")
			}
			if resolved == nil {
				continue
			}
			if run != nil {
				return errorResponse("ambiguous_run_ref")
			}
			run = resolved
		}
		if run == nil {
			return errorResponse("not_found")
		}
	}

	protoEvents := make([]*orchpb.Event, len(run.Events))
	for i, e := range run.Events {
		protoEvents[i] = modelEventToProto(e)
	}

	pr := modelRunToProto(run)
	enrichRunProto(pr, run, s.gitRunner)

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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("no store available")
	}

	projectRoot := s.resolveProjectRootFromContextOrProto(req.Context, req.ProjectRoot)
	if projectRoot == "" {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("unknown project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("project_root required")
	}

	opts := &StartRunOptions{
		IssueID:        req.IssueId,
		RunID:          req.RunId,
		Agent:          req.Agent,
		AgentCmd:       req.AgentCmd,
		AgentProfile:   req.AgentProfile,
		Model:          req.Model,
		ModelVariant:   req.ModelVariant,
		Preset:         req.Preset,
		BaseBranch:     req.BaseBranch,
		Branch:         req.Branch,
		WorktreeDir:    req.WorktreeDir,
		NoPR:           req.NoPr,
		PromptTemplate: req.PromptTemplate,
		PRTargetBranch: req.PrTargetBranch,
		DryRun:         req.DryRun,
		Reuse:          req.Reuse,
		Multiplexer:    req.Multiplexer,
		Target:         req.Target,
		ProjectRoot:    projectRoot,
	}
	if strings.TrimSpace(req.Target) != "" && strings.TrimSpace(req.Target) != "local" {
		cfg, cfgErr := config.LoadFromProjectRoot(projectRoot)
		if cfgErr != nil {
			return errorResponse(fmt.Sprintf("failed to load config for target %q: %v", req.Target, cfgErr))
		}
		targetCfg := cfg.GetTarget(strings.TrimSpace(req.Target))
		if targetCfg == nil {
			return errorResponse(fmt.Sprintf("target %q not found in config", req.Target))
		}
		opts.ProjectRoot = strings.TrimSpace(targetCfg.Repo)
	}

	if issue, err := st.ResolveIssue(req.IssueId); err == nil {
		opts.IssueSnapshot = issue
	}

	projectID := projectIDFromContext(req.Context)
	payload := &WorkerEffectPayload{StartRun: opts}
	completedLease, err := s.withWorkerLease(projectID, "start_run", req.IssueId, req.RunId, payload)
	if err != nil {
		return errorResponse(err.Error())
	}
	effectResult, err := decodeWorkerEffectResult(completedLease.ResultJSON)
	if err != nil {
		return errorResponse(err.Error())
	}
	result := effectResult.StartRunResult
	if result == nil {
		return errorResponse("worker lease completed without start_run result")
	}

	if !req.DryRun {
		if err := s.syncStartRunResultToMasterStore(st, req, result); err != nil {
			s.logger.Printf("warning: failed to sync start_run projection for %s#%s: %v", req.IssueId, result.RunID, err)
		}
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_StartRun{
			StartRun: &orchpb.StartRunResponse{
				RunId:        result.RunID,
				Branch:       result.Branch,
				WorktreePath: result.WorktreePath,
				SessionName:  result.SessionName,
				Status:       result.Status,
			},
		},
	}
}

func (s *SocketServer) syncStartRunResultToMasterStore(st store.Store, req *orchpb.StartRunRequest, result *StartRunResult) error {
	if st == nil || result == nil {
		return nil
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: result.RunID}
	run, err := st.GetRun(ref)
	if err != nil {
		metadata := map[string]string{}
		if req.Agent != "" {
			metadata["agent"] = req.Agent
		}
		if req.Model != "" {
			metadata["model"] = req.Model
		}
		if req.ModelVariant != "" {
			metadata["model_variant"] = req.ModelVariant
		}
		if req.Target != "" {
			metadata["target"] = req.Target
		}

		run, err = st.CreateRun(req.IssueId, result.RunID, metadata)
		if err != nil {
			return err
		}
		if run == nil {
			return fmt.Errorf("store returned nil run for %s#%s", req.IssueId, result.RunID)
		}
		_ = st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusQueued))
	}
	if run == nil {
		return fmt.Errorf("run unavailable for %s#%s", req.IssueId, result.RunID)
	}

	if result.WorktreePath != "" {
		_ = st.AppendEvent(run.Ref(), model.NewArtifactEvent("worktree", map[string]string{"path": result.WorktreePath}))
	}
	if result.Branch != "" {
		_ = st.AppendEvent(run.Ref(), model.NewArtifactEvent("branch", map[string]string{"name": result.Branch}))
	}
	if result.SessionName != "" {
		_ = st.AppendEvent(run.Ref(), model.NewArtifactEvent("session", map[string]string{"name": result.SessionName}))
	}

	status := model.NormalizeStatus(result.Status)
	if status == "" {
		status = model.StatusRunning
	}
	_ = st.AppendEvent(run.Ref(), model.NewStatusEvent(status))

	return nil
}

func (s *SocketServer) handleProtoContinueRun(req *orchpb.ContinueRunRequest) *orchpb.Response {
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("no store available")
	}

	projectRoot := s.resolveProjectRootFromContextOrProto(req.Context, req.ProjectRoot)
	if projectRoot == "" && req.RepoRoot != "" {
		projectRoot = s.resolveProjectRootFromContextOrProto(req.Context, req.RepoRoot)
	}
	if projectRoot == "" {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("unknown project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("project_root required")
	}

	opts := &ContinueRunOptions{
		IssueID:        req.IssueId,
		RunID:          req.RunId,
		ShortID:        req.ShortId,
		Branch:         req.Branch,
		Agent:          req.Agent,
		AgentCmd:       req.AgentCmd,
		AgentProfile:   req.AgentProfile,
		WorktreeDir:    req.WorktreeDir,
		NoPR:           req.NoPr,
		PromptTemplate: req.PromptTemplate,
		PRTargetBranch: req.PrTargetBranch,
		Multiplexer:    req.Multiplexer,
		SessionName:    req.SessionName,
		ProjectRoot:    projectRoot,
		RepoRoot:       req.RepoRoot,
	}
	if req.ShortId != "" {
		if run, err := st.GetRunByShortID(req.ShortId); err == nil && run != nil {
			opts.Target = strings.TrimSpace(run.Target)
		}
	} else if req.IssueId != "" && req.RunId != "" {
		if run, err := st.GetRun(&model.RunRef{IssueID: req.IssueId, RunID: req.RunId}); err == nil && run != nil {
			opts.Target = strings.TrimSpace(run.Target)
		}
	}

	projectID := projectIDFromContext(req.Context)
	payload := &WorkerEffectPayload{ContinueRun: opts}
	completedLease, err := s.withWorkerLease(projectID, "continue_run", req.IssueId, req.RunId, payload)
	if err != nil {
		return errorResponse(err.Error())
	}
	effectResult, err := decodeWorkerEffectResult(completedLease.ResultJSON)
	if err != nil {
		return errorResponse(err.Error())
	}
	result := effectResult.ContinueRunResult
	if result == nil {
		return errorResponse("worker lease completed without continue_run result")
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ContinueRun{
			ContinueRun: &orchpb.ContinueRunResponse{
				RunId:         result.RunID,
				Branch:        result.Branch,
				WorktreePath:  result.WorktreePath,
				SessionName:   result.SessionName,
				Status:        result.Status,
				ContinuedFrom: result.ContinuedFrom,
				IssueId:       result.IssueID,
			},
		},
	}
}

func (s *SocketServer) handleProtoStopRun(req *orchpb.StopRunRequest) *orchpb.Response {
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("no store available")
	}

	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	run, err := st.GetRun(ref)
	if err != nil {
		return errorResponse("not_found")
	}

	projectID := projectIDFromContext(req.Context)
	projectRoot := s.resolveProjectRootFromContextOrProto(req.Context, "")
	payload := &WorkerEffectPayload{}
	if strings.TrimSpace(projectRoot) != "" {
		payload.StopRun = &StopRunPayload{ProjectRoot: projectRoot, Target: strings.TrimSpace(run.Target)}
	}
	if _, err := s.withWorkerLease(projectID, "stop_run", run.IssueID, run.RunID, payload); err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok:       true,
		Response: &orchpb.Response_StopRun{StopRun: &orchpb.StopRunResponse{}},
	}
}

func (s *SocketServer) handleProtoResolveRun(req *orchpb.ResolveRunRequest) *orchpb.Response {
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
	projectID := projectIDFromContext(req.Context)
	issues := make([]*model.Issue, 0)

	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		list, err := st.ListIssues()
		if err != nil {
			return errorResponse("store_error")
		}
		issues = append(issues, list...)
	} else {
		for _, st := range s.listStores() {
			list, err := st.ListIssues()
			if err != nil {
				return errorResponse("store_error")
			}
			issues = append(issues, list...)
		}
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
	projectID := projectIDFromContext(req.Context)

	var issue *model.Issue
	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		resolved, err := st.ResolveIssue(req.IssueId)
		if err != nil {
			return errorResponse("not_found")
		}
		issue = resolved
	} else {
		for _, st := range s.listStores() {
			resolved, err := st.ResolveIssue(req.IssueId)
			if err != nil {
				if isStoreNotFoundError(err) {
					continue
				}
				return errorResponse("store_error")
			}
			if resolved == nil {
				continue
			}
			if issue != nil {
				return errorResponse("ambiguous_issue_id")
			}
			issue = resolved
		}
		if issue == nil {
			return errorResponse("not_found")
		}
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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("no store available")
	}

	params := &CreateIssueParams{
		IssueID: req.IssueId,
		Title:   req.Title,
		Body:    req.Body,
		Tags:    req.Tags,
	}

	result, err := s.processCreateIssueCore(st, params)
	if err != nil {
		return errorResponse(err.Error())
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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
	projectRoot := s.resolveProjectRootFromContextOrProto(req.Context, "")
	if projectRoot == "" {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("unknown project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("project_id required")
	}

	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("no store available")
	}

	params := &ControlAgentLaunchParams{
		ProjectRoot: projectRoot,
		Agent:       req.Agent,
		NewSession:  req.NewSession,
	}

	result, err := s.processControlAgentLaunchCore(st, params)
	if err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetControlAgentLaunch{
			GetControlAgentLaunch: &orchpb.GetControlAgentLaunchResponse{
				Command:    result.Command,
				PromptFile: result.PromptFile,
				Port:       int32(result.Port),
				SessionId:  result.SessionID,
				Resumed:    result.Resumed,
			},
		},
	}
}

func (s *SocketServer) handleProtoGetControlAgentConfig(req *orchpb.GetControlAgentConfigRequest) *orchpb.Response {
	projectRoot := s.resolveProjectRootFromContextOrProto(req.Context, "")
	if projectRoot == "" {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("unknown project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("project_id required")
	}

	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("no store available")
	}

	result, err := s.processControlAgentConfigCore(st, projectRoot, "")
	if err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetControlAgentConfig{
			GetControlAgentConfig: &orchpb.GetControlAgentConfigResponse{
				PromptContent: result.PromptContent,
				Agent:         result.Agent,
				Model:         result.Model,
				ModelVariant:  result.ModelVariant,
				ExtraArgs:     result.ExtraArgs,
			},
		},
	}
}

func (s *SocketServer) handleProtoGetAttachInfo(req *orchpb.GetAttachInfoRequest) *orchpb.Response {
	var run *model.Run
	projectID := projectIDFromContext(req.Context)

	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

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
	} else {
		for _, st := range s.listStores() {
			var (
				resolved *model.Run
				err      error
			)
			if req.ShortId != "" {
				resolved, err = st.GetRunByShortID(req.ShortId)
				if err != nil {
					if isStoreNotFoundError(err) {
						continue
					}
					if strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
						return errorResponse("ambiguous_short_id")
					}
					return errorResponse("store_error")
				}
			} else {
				ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
				resolved, err = st.GetRun(ref)
				if err != nil {
					if isStoreNotFoundError(err) {
						continue
					}
					return errorResponse("store_error")
				}
			}

			if resolved == nil {
				continue
			}
			if run != nil {
				if req.ShortId != "" {
					return errorResponse("ambiguous_short_id")
				}
				return errorResponse("ambiguous_run_ref")
			}
			run = resolved
		}
		if run == nil {
			return errorResponse("not_found")
		}
	}

	attachInfo := &orchpb.GetAttachInfoResponse{
		Command:           []string{"orch", "attach", fmt.Sprintf("%s#%s", run.IssueID, run.RunID)},
		Multiplexer:       multiplexerToProto(run.Multiplexer),
		SessionName:       run.SessionName,
		WorktreePath:      run.WorktreePath,
		Agent:             run.Agent,
		ServerPort:        int32(run.ServerPort),
		OpencodeSessionId: run.OpenCodeSessionID,
		IssueId:           run.IssueID,
		RunId:             run.RunID,
	}

	if run.Target != "" {
		if cfg, cfgErr := s.loadConfig(""); cfgErr == nil && cfg != nil {
			if target := cfg.GetTarget(run.Target); target != nil {
				attachInfo.TargetHost = target.Host
			}
		}
	}

	sessionName := run.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
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
	} else if attachInfo.TargetHost == "" {
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
	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	projectID := projectIDFromContext(req.Context)

	var run *model.Run
	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		resolved, err := st.GetRun(ref)
		if err != nil {
			return errorResponse("not_found")
		}
		run = resolved
	} else {
		for _, st := range s.listStores() {
			resolved, err := st.GetRun(ref)
			if err != nil {
				if isStoreNotFoundError(err) {
					continue
				}
				return errorResponse("store_error")
			}
			if resolved == nil {
				continue
			}
			if run != nil {
				return errorResponse("ambiguous_run_ref")
			}
			run = resolved
		}
		if run == nil {
			return errorResponse("not_found")
		}
	}

	var content string
	var source string
	var err error

	if run.Agent == string(agent.AgentOpenCode) {
		content, source, err = s.captureOpenCodeSession(run)
		if err != nil {
			return errorResponse(err.Error())
		}
	} else {
		muxType, _ := multiplexer.ParseType(run.Multiplexer)
		mux, _ := multiplexer.GetMultiplexer(muxType)
		if mux != nil && run.SessionName != "" {
			content, _ = mux.CapturePane(run.SessionName, 100)
		}
		source = run.Multiplexer
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_CaptureSession{
			CaptureSession: &orchpb.CaptureSessionResponse{
				Content:       sanitizeUTF8(content),
				TimestampUnix: time.Now().Unix(),
				Source:        sanitizeUTF8(source),
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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("no store available")
	}

	params := &SendMessageParams{
		IssueID: req.IssueId,
		RunID:   req.RunId,
		Message: req.Message,
	}

	if err := s.processSendMessage(st, params); err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok:       true,
		Response: &orchpb.Response_SendMessage{SendMessage: &orchpb.SendMessageResponse{}},
	}
}

func (s *SocketServer) handleProtoGetDiffStats(req *orchpb.GetDiffStatsRequest) *orchpb.Response {
	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	projectID := projectIDFromContext(req.Context)

	var run *model.Run
	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		resolved, err := st.GetRun(ref)
		if err != nil {
			return errorResponse("not_found")
		}
		run = resolved
	} else {
		for _, st := range s.listStores() {
			resolved, err := st.GetRun(ref)
			if err != nil {
				if isStoreNotFoundError(err) {
					continue
				}
				return errorResponse("store_error")
			}
			if resolved == nil {
				continue
			}
			if run != nil {
				return errorResponse("ambiguous_run_ref")
			}
			run = resolved
		}
		if run == nil {
			return errorResponse("not_found")
		}
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
					Files:        sanitizeUTF8Slice(stats.Files),
				},
			},
		},
	}
}

func (s *SocketServer) handleProtoGetBranchState(req *orchpb.GetBranchStateRequest) *orchpb.Response {
	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	projectID := projectIDFromContext(req.Context)

	var run *model.Run
	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		resolved, err := st.GetRun(ref)
		if err != nil {
			return errorResponse("not_found")
		}
		run = resolved
	} else {
		for _, st := range s.listStores() {
			resolved, err := st.GetRun(ref)
			if err != nil {
				if isStoreNotFoundError(err) {
					continue
				}
				return errorResponse("store_error")
			}
			if resolved == nil {
				continue
			}
			if run != nil {
				return errorResponse("ambiguous_run_ref")
			}
			run = resolved
		}
		if run == nil {
			return errorResponse("not_found")
		}
	}

	state := computeBranchStateWithRunner(s.gitRunner, run.WorktreePath, run.Branch, "main")

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetBranchState{
			GetBranchState: &orchpb.GetBranchStateResponse{
				State: state,
			},
		},
	}
}

func computeBranchState(worktreePath, branch, baseBranch string, runner ...git.Runner) orchpb.BranchState {
	if len(runner) > 0 && runner[0] != nil {
		return computeBranchStateWithRunner(runner[0], worktreePath, branch, baseBranch)
	}
	return computeBranchStateWithRunner(git.NewRunner(), worktreePath, branch, baseBranch)
}

func computeBranchStateWithRunner(runner git.Runner, worktreePath, branch, baseBranch string) orchpb.BranchState {
	if worktreePath == "" {
		return orchpb.BranchState_BRANCH_STATE_UNSPECIFIED
	}

	if runner == nil {
		runner = git.NewRunner()
	}

	output, err := runner.StatusPorcelain(context.Background(), worktreePath)
	if err != nil {
		return orchpb.BranchState_BRANCH_STATE_UNSPECIFIED
	}
	if len(output) > 0 {
		return orchpb.BranchState_BRANCH_STATE_DIRTY
	}

	merged, err := runner.MergedBranches(context.Background(), worktreePath, baseBranch)
	if err == nil {
		for _, line := range merged {
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
	ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
	projectID := projectIDFromContext(req.Context)

	var run *model.Run
	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		resolved, err := st.GetRun(ref)
		if err != nil {
			return errorResponse("not_found")
		}
		run = resolved
	} else {
		for _, st := range s.listStores() {
			resolved, err := st.GetRun(ref)
			if err != nil {
				if isStoreNotFoundError(err) {
					continue
				}
				return errorResponse("store_error")
			}
			if resolved == nil {
				continue
			}
			if run != nil {
				return errorResponse("ambiguous_run_ref")
			}
			run = resolved
		}
		if run == nil {
			return errorResponse("not_found")
		}
	}

	var diff string
	if run.WorktreePath != "" && run.Branch != "" {
		output, err := s.gitRunner.Diff(context.Background(), run.WorktreePath, "main..."+run.Branch)
		if err == nil {
			diff = output
		}
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetDiff{
			GetDiff: &orchpb.GetDiffResponse{
				Diff: sanitizeUTF8(diff),
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
		SessionName: req.SessionName,
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
			SessionName:       conn.SessionName,
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

func (s *SocketServer) handleProtoRegisterWorker(req *orchpb.RegisterWorkerRequest) *orchpb.Response {
	if err := s.requireWorkerAuth(req.AuthToken); err != nil {
		return errorResponse(err.Error())
	}

	worker, ttl := s.registerWorker(req.WorkerId, req.WorkerType, req.Host, req.Mode, req.Capabilities)

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_RegisterWorker{
			RegisterWorker: &orchpb.RegisterWorkerResponse{
				WorkerId:            worker.ID,
				HeartbeatTtlSeconds: ttl,
			},
		},
	}
}

func (s *SocketServer) handleProtoUnregisterWorker(req *orchpb.UnregisterWorkerRequest) *orchpb.Response {
	workerID := strings.TrimSpace(req.WorkerId)
	if workerID == "" {
		return errorResponse("worker_id required")
	}

	s.unregisterWorker(workerID)
	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_UnregisterWorker{
			UnregisterWorker: &orchpb.UnregisterWorkerResponse{},
		},
	}
}

func (s *SocketServer) handleProtoWorkerHeartbeat(req *orchpb.WorkerHeartbeatRequest) *orchpb.Response {
	if err := s.requireWorkerAuth(req.AuthToken); err != nil {
		return errorResponse(err.Error())
	}

	ttl, err := s.heartbeatWorker(req.WorkerId)
	if err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_WorkerHeartbeat{
			WorkerHeartbeat: &orchpb.WorkerHeartbeatResponse{HeartbeatTtlSeconds: ttl},
		},
	}
}

func (s *SocketServer) handleProtoListWorkers(_ *orchpb.ListWorkersRequest) *orchpb.Response {
	workers := s.listWorkers()
	protoWorkers := make([]*orchpb.WorkerInfo, 0, len(workers))
	for _, worker := range workers {
		protoWorkers = append(protoWorkers, &orchpb.WorkerInfo{
			Id:                worker.ID,
			WorkerType:        worker.WorkerType,
			Host:              worker.Host,
			Mode:              worker.Mode,
			RegisteredAtUnix:  worker.RegisteredAt.Unix(),
			LastHeartbeatUnix: worker.LastHeartbeat.Unix(),
			Active:            worker.Active,
			Capabilities:      append([]string(nil), worker.Capabilities...),
		})
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_ListWorkers{
			ListWorkers: &orchpb.ListWorkersResponse{Workers: protoWorkers},
		},
	}
}

func (s *SocketServer) handleProtoLeaseWork(req *orchpb.LeaseWorkRequest) *orchpb.Response {
	if err := s.requireWorkerAuth(req.AuthToken); err != nil {
		return errorResponse(err.Error())
	}

	workerID := strings.TrimSpace(req.WorkerId)
	if workerID == "" {
		return errorResponse("worker_id required")
	}

	if _, err := s.heartbeatWorker(workerID); err != nil {
		return errorResponse(err.Error())
	}

	lease := s.leaseWorkForWorker(workerID)

	leaseResp := &orchpb.LeaseWorkResponse{}
	if lease != nil {
		leaseResp = &orchpb.LeaseWorkResponse{
			LeaseId:       lease.LeaseID,
			WorkerId:      lease.WorkerID,
			ProjectId:     lease.ProjectID,
			Effect:        lease.Effect,
			IssueId:       lease.IssueID,
			RunId:         lease.RunID,
			LeasedAtUnix:  lease.LeasedAt.Unix(),
			ExpiresAtUnix: lease.ExpiresAt.Unix(),
			PayloadJson:   lease.PayloadJSON,
		}
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_LeaseWork{
			LeaseWork: leaseResp,
		},
	}
}

func (s *SocketServer) handleProtoAcknowledgeEffect(req *orchpb.AcknowledgeEffectRequest) *orchpb.Response {
	if err := s.requireWorkerAuth(req.AuthToken); err != nil {
		return errorResponse(err.Error())
	}

	if err := s.acknowledgeWorkerLease(req.WorkerId, req.LeaseId, req.Success, req.Error, req.ResultJson); err != nil {
		return errorResponse(err.Error())
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_AcknowledgeEffect{
			AcknowledgeEffect: &orchpb.AcknowledgeEffectResponse{},
		},
	}
}

func (s *SocketServer) handleProtoStartExternalWorker(req *orchpb.StartExternalWorkerRequest) *orchpb.Response {
	workerID, pid, err := s.startManagedExternalWorker(req.WorkerId)
	if err != nil {
		return errorResponse(err.Error())
	}
	return &orchpb.Response{Ok: true, Response: &orchpb.Response_StartExternalWorker{StartExternalWorker: &orchpb.StartExternalWorkerResponse{WorkerId: workerID, Pid: int32(pid)}}}
}

func (s *SocketServer) handleProtoStopExternalWorker(req *orchpb.StopExternalWorkerRequest) *orchpb.Response {
	stopped, err := s.stopManagedExternalWorker(req.WorkerId, req.All)
	if err != nil {
		return errorResponse(err.Error())
	}
	return &orchpb.Response{Ok: true, Response: &orchpb.Response_StopExternalWorker{StopExternalWorker: &orchpb.StopExternalWorkerResponse{StoppedCount: int32(stopped)}}}
}

func (s *SocketServer) handleProtoListExternalWorkers(_ *orchpb.ListExternalWorkersRequest) *orchpb.Response {
	items := s.listManagedExternalWorkers()
	out := make([]*orchpb.ExternalWorkerProcessInfo, 0, len(items))
	for _, item := range items {
		out = append(out, &orchpb.ExternalWorkerProcessInfo{WorkerId: item.WorkerID, Pid: int32(item.PID), StartedAtUnix: item.StartedAt.Unix()})
	}
	return &orchpb.Response{Ok: true, Response: &orchpb.Response_ListExternalWorkers{ListExternalWorkers: &orchpb.ListExternalWorkersResponse{Workers: out}}}
}

func (s *SocketServer) handleProtoGetRunByShortID(req *orchpb.GetRunByShortIDRequest) *orchpb.Response {
	projectID := projectIDFromContext(req.Context)

	var run *model.Run
	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		resolved, err := st.GetRunByShortID(req.ShortId)
		if err != nil {
			return errorResponse("not_found")
		}
		run = resolved
	} else {
		for _, st := range s.listStores() {
			resolved, err := st.GetRunByShortID(req.ShortId)
			if err != nil {
				if isStoreNotFoundError(err) {
					continue
				}
				if strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
					return errorResponse("ambiguous_short_id")
				}
				return errorResponse("store_error")
			}
			if resolved == nil {
				continue
			}
			if run != nil {
				return errorResponse("ambiguous_short_id")
			}
			run = resolved
		}
		if run == nil {
			return errorResponse("not_found")
		}
	}

	pr := modelRunToProto(run)
	enrichRunProto(pr, run, s.gitRunner)

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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
	projectRoot := s.resolveProjectRootFromContextOrProto(req.Context, "")
	if projectRoot == "" {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("unknown project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("project_id required")
	}
	repoID, err := s.repoIDForProjectRoot(projectRoot)
	if err != nil {
		return errorResponse(err.Error())
	}

	s.openCodeServersMu.RLock()
	_, alreadyRunning := s.openCodeServers[repoID]
	s.openCodeServersMu.RUnlock()

	port, err := s.ensureOpenCodeServerRunning(projectRoot)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to ensure opencode server: %v", err))
	}

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
	input := strings.TrimSpace(req.ProjectRoot)
	if input == "" {
		return errorResponse("repo URL required")
	}

	projectRoot := ""
	repoURL := ""
	repoID := ""
	var err error

	if looksLikeRepoURL(input) {
		parsedID, err := xdg.ParseRepoID(input)
		if err != nil {
			return errorResponse(fmt.Sprintf("invalid repo URL %q: %v", input, err))
		}
		repoID = strings.TrimSpace(parsedID)
		repoURL = input
		workspaceRoot, err := ensureManagedRepoWorkspace(repoID, repoURL)
		if err != nil {
			return errorResponse(err.Error())
		}
		projectRoot = workspaceRoot
	} else {
		projectRoot = s.resolveProtoProjectRoot(input)
		if projectRoot == "" {
			return errorResponse("repo URL required")
		}
		repoID, err = s.repoIDForProjectRoot(projectRoot)
		if err != nil {
			return errorResponse(err.Error())
		}
	}

	var repoStore store.Store
	if s.storeFactory != nil {
		if cfg, err := config.LoadFromProjectRoot(projectRoot); err == nil && cfg != nil {
			if issuesRoot := strings.TrimSpace(cfg.GetIssuesPath()); issuesRoot != "" {
				repoStore = s.getOrCreateStore(issuesRoot, projectRoot)
			}
		}
	}

	if _, err := s.registerRepoContext(repoID, projectRoot, repoURL, repoStore); err != nil {
		return errorResponse(err.Error())
	}

	if err := s.persistRepoRegistry(); err != nil {
		return errorResponse(fmt.Sprintf("failed to persist repo registry: %v", err))
	}

	if repoURL != "" {
		s.logger.Printf("registered repo: %s (%s -> %s)", repoID, repoURL, projectRoot)
	} else {
		s.logger.Printf("registered repo: %s (%s)", repoID, projectRoot)
	}

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
	seen := make(map[string]struct{}, len(s.repos))
	for key, info := range s.repos {
		if info == nil {
			continue
		}

		repoID := strings.TrimSpace(info.RepoID)
		if repoID == "" {
			repoID = key
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}

		repos = append(repos, &orchpb.RepoInfo{
			Id:          repoID,
			ProjectRoot: info.ProjectRoot,
		})
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].Id < repos[j].Id
	})

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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
			if s.gitRunner.DeleteBranch(context.Background(), repoRoot, run.Branch) == nil {
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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
	projectID := projectIDFromContext(req.Context)
	aggregated := &store.ValidationResult{}

	if projectID != "" {
		st := s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		result, err := st.ValidateIssueFiles(req.IssueId)
		if err != nil {
			return errorResponse(fmt.Sprintf("validation failed: %v", err))
		}
		if result != nil {
			aggregated.Total += result.Total
			aggregated.Valid += result.Valid
			aggregated.Errors = append(aggregated.Errors, result.Errors...)
			aggregated.Duplicates = append(aggregated.Duplicates, result.Duplicates...)
		}
	} else {
		stores := s.listStores()
		for _, st := range stores {
			result, err := st.ValidateIssueFiles(req.IssueId)
			if err != nil {
				if req.IssueId != "" && isStoreNotFoundError(err) {
					continue
				}
				return errorResponse(fmt.Sprintf("validation failed: %v", err))
			}
			if result == nil {
				continue
			}
			aggregated.Total += result.Total
			aggregated.Valid += result.Valid
			aggregated.Errors = append(aggregated.Errors, result.Errors...)
			aggregated.Duplicates = append(aggregated.Duplicates, result.Duplicates...)
		}
	}

	protoResult := &orchpb.ValidateIssueFilesResponse{
		Total: int32(aggregated.Total),
		Valid: int32(aggregated.Valid),
	}

	for _, e := range aggregated.Errors {
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

	for _, d := range aggregated.Duplicates {
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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
	projectID := projectIDFromContext(req.Context)
	var st store.Store
	var run *model.Run
	var err error

	if projectID != "" {
		st = s.resolveStoreFromContextOrProto(req.Context, "")
		if st == nil {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}

		if req.ShortId != "" {
			run, err = st.GetRunByShortID(req.ShortId)
		} else {
			ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
			run, err = st.GetRun(ref)
		}
		if err != nil {
			return errorResponse(fmt.Sprintf("run not found: %v", err))
		}
	} else {
		for _, candidateStore := range s.listStores() {
			var (
				resolved *model.Run
				err      error
			)

			if req.ShortId != "" {
				resolved, err = candidateStore.GetRunByShortID(req.ShortId)
				if err != nil {
					if isStoreNotFoundError(err) {
						continue
					}
					if strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
						return errorResponse("ambiguous_short_id")
					}
					return errorResponse(fmt.Sprintf("run lookup failed: %v", err))
				}
			} else {
				ref := &model.RunRef{IssueID: req.IssueId, RunID: req.RunId}
				resolved, err = candidateStore.GetRun(ref)
				if err != nil {
					if isStoreNotFoundError(err) {
						continue
					}
					return errorResponse(fmt.Sprintf("run lookup failed: %v", err))
				}
			}

			if resolved == nil {
				continue
			}
			if run != nil {
				if req.ShortId != "" {
					return errorResponse("ambiguous_short_id")
				}
				return errorResponse("ambiguous_run_ref")
			}
			run = resolved
			st = candidateStore
		}

		if run == nil || st == nil {
			return errorResponse("run not found")
		}
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

	stores := s.listStores()
	if len(stores) == 0 {
		return nil
	}

	expectedSessions := make(map[string]bool)
	for _, st := range stores {
		runs, err := st.ListRuns(&store.ListRunsFilter{})
		if err != nil {
			continue
		}
		for _, run := range runs {
			sessionName := run.SessionName
			if sessionName == "" {
				sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
			}
			expectedSessions[sessionName] = true
		}
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
	logPath := LogFilePath("")
	content, err := readLastNLines(logPath, int(req.Lines))
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to read daemon log: %v", err))
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetDaemonLog{
			GetDaemonLog: &orchpb.GetDaemonLogResponse{
				Content: sanitizeUTF8(content),
			},
		},
	}
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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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
					SessionId: run.SessionName,
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

	sessionName := run.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
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
				Sessions: sanitizeUTF8Slice(sessions),
			},
		},
	}
}

func (s *SocketServer) handleProtoResumeRun(req *orchpb.ResumeRunRequest) *orchpb.Response {
	st := s.resolveStoreFromContextOrProto(req.Context, "")
	if st == nil {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("no store available for project_id %q (register daemon project mapping)", projectID))
		}
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

	sessionName := run.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
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

func (s *SocketServer) loadConfig(projectRoot string) (*config.Config, error) {
	var (
		cfg *config.Config
		err error
	)
	if projectRoot != "" {
		cfg, err = config.LoadFromProjectRoot(projectRoot)
	} else {
		cfg, err = config.Load()
	}
	if err != nil && s.logger != nil {
		if projectRoot != "" {
			s.logger.Printf("config validation failed for project_root=%q: %v", projectRoot, err)
		} else {
			s.logger.Printf("config validation failed: %v", err)
		}
	}
	return cfg, err
}

func (s *SocketServer) handleProtoGetConfig(req *orchpb.GetConfigRequest) *orchpb.Response {
	projectRoot := s.resolveProjectRootFromContextOrProto(req.Context, req.ProjectRoot)
	if projectRoot == "" {
		if projectID := projectIDFromContext(req.Context); projectID != "" {
			return errorResponse(fmt.Sprintf("unknown project_id %q (register daemon project mapping)", projectID))
		}
		return errorResponse("project_id required")
	}

	cfg, err := s.loadConfig(projectRoot)
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to load config: %v", err))
	}

	resp := &orchpb.GetConfigResponse{
		Agent:               cfg.Agent,
		Model:               cfg.Model,
		ModelVariant:        cfg.ModelVariant,
		WorktreeDir:         cfg.WorktreeDir,
		BaseBranch:          cfg.BaseBranch,
		PrTargetBranch:      cfg.PRTargetBranch,
		LogLevel:            cfg.LogLevel,
		PromptTemplate:      cfg.PromptTemplate,
		Multiplexer:         cfg.Multiplexer,
		MonitorMultiplexer:  cfg.MonitorMultiplexer,
		AgentMultiplexer:    cfg.AgentMultiplexer,
		NoPr:                cfg.NoPR,
		DefaultPreset:       cfg.DefaultPreset,
		ControlAgent:        cfg.ControlAgent,
		ControlModel:        cfg.ControlModel,
		ControlModelVariant: cfg.ControlModelVariant,
		DiffTool:            cfg.DiffTool,
	}

	if len(cfg.Monitor.PSColumns) > 0 {
		resp.Monitor = &orchpb.MonitorConfigProto{
			PsColumns: cfg.Monitor.PSColumns,
		}
	}

	for _, p := range cfg.GetAllPresets() {
		resp.Presets = append(resp.Presets, &orchpb.PresetProto{
			Name:    p.Name,
			Backend: p.Backend,
			Model:   p.Model,
			Variant: p.Variant,
			Profile: p.Profile,
		})
	}

	resp.Opencode = &orchpb.OpenCodeConfigProto{
		DefaultModel:     cfg.OpenCode.DefaultModel,
		DefaultVariant:   cfg.OpenCode.DefaultVariant,
		PromptTemplate:   cfg.OpenCode.PromptTemplate,
		ExtraArgs:        cfg.OpenCode.ExtraArgs,
		ControlExtraArgs: cfg.OpenCode.ControlExtraArgs,
	}

	resp.Claude = &orchpb.ClaudeConfigProto{
		PromptTemplate:   cfg.Claude.PromptTemplate,
		ExtraArgs:        cfg.Claude.ExtraArgs,
		ControlExtraArgs: cfg.Claude.ControlExtraArgs,
	}

	resp.Codex = &orchpb.CodexConfigProto{
		PromptTemplate:   cfg.Codex.PromptTemplate,
		ExtraArgs:        cfg.Codex.ExtraArgs,
		ControlExtraArgs: cfg.Codex.ControlExtraArgs,
	}

	resp.Gemini = &orchpb.GeminiConfigProto{
		PromptTemplate:   cfg.Gemini.PromptTemplate,
		ExtraArgs:        cfg.Gemini.ExtraArgs,
		ControlExtraArgs: cfg.Gemini.ControlExtraArgs,
	}

	resp.Slack = &orchpb.SlackConfigProto{
		Enabled:    cfg.Slack.Enabled,
		WebhookUrl: cfg.Slack.WebhookURL,
		BotToken:   cfg.Slack.BotToken,
		Channel:    cfg.Slack.Channel,
		NotifyOn:   cfg.Slack.NotifyOn,
	}

	resp.Issues = &orchpb.IssuesConfigProto{
		Backend: cfg.Issues.Backend,
		Path:    cfg.Issues.Path,
	}

	resp.Github = &orchpb.GitHubConfigProto{
		Owner:        cfg.GitHub.Owner,
		Repo:         cfg.GitHub.Repo,
		LabelFilter:  cfg.GitHub.LabelFilter,
		PollInterval: int32(cfg.GitHub.PollInterval),
		StatusLabels: cfg.GitHub.StatusLabels,
	}

	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetConfig{
			GetConfig: resp,
		},
	}
}

func (s *SocketServer) handleProtoGetDaemonStatus(_ *orchpb.GetDaemonStatusRequest) *orchpb.Response {
	return &orchpb.Response{
		Ok: true,
		Response: &orchpb.Response_GetDaemonStatus{
			GetDaemonStatus: &orchpb.GetDaemonStatusResponse{
				Running: true,
				Pid:     int32(os.Getpid()),
				LogPath: LogFilePath(""),
				Version: "1.0.0",
			},
		},
	}
}
