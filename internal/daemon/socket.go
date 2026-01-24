package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/github"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/xdg"
)

const (
	socketFile = "daemon.sock"
)

// SocketFilePath returns the global daemon socket path.
func SocketFilePath(_ string) string {
	return xdg.SocketPath()
}

// LegacySocketFilePath returns the legacy per-project socket path.
func LegacySocketFilePath(projectRoot string) string {
	return filepath.Join(OrchDir(projectRoot), socketFile)
}

type SendRequest struct {
	Type        string   `json:"type"`
	IssueID     string   `json:"issue_id"`
	RunID       string   `json:"run_id"`
	Message     string   `json:"message"`
	NoEnter     bool     `json:"no_enter,omitempty"`
	IssuesRoot  string   `json:"issues_root,omitempty"`
	ProjectRoot string   `json:"project_root,omitempty"`
	RepoID      string   `json:"repo_id,omitempty"`
	Status      []string `json:"status,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	Cursor      string   `json:"cursor,omitempty"`
	Title       string   `json:"title,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Body        string   `json:"body,omitempty"`
	Force       bool     `json:"force,omitempty"`
	ShortID     string   `json:"short_id,omitempty"`
	Comment     string   `json:"comment,omitempty"`
	All         bool     `json:"all,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
	AgentType   string   `json:"agent_type,omitempty"`
}

type SendResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// RepoContext holds per-repo state in the daemon
type RepoContext struct {
	ProjectRoot   string
	RepoID        string
	Store         store.Store
	GitHubBackend *github.Backend
	Config        interface{}
}

type StoreFactory func(issuesRoot string) (store.Store, error)

type SocketServer struct {
	storeFactory StoreFactory
	listener     net.Listener
	logger       Logger
	stopCh       chan struct{}

	repos   map[string]*RepoContext
	reposMu sync.RWMutex

	githubBackend *github.Backend

	monitors   map[string]*MonitorConnection
	monitorsMu sync.RWMutex

	// Per-project locks for control session operations to prevent race conditions
	// when multiple orch-monitor instances start in quick succession
	controlSessionLocks   map[string]*sync.Mutex
	controlSessionLocksMu sync.Mutex
}

type Logger interface {
	Printf(format string, v ...interface{})
}

func NewSocketServer(factory StoreFactory, logger Logger) *SocketServer {
	return &SocketServer{
		storeFactory:        factory,
		logger:              logger,
		stopCh:              make(chan struct{}),
		monitors:            make(map[string]*MonitorConnection),
		repos:               make(map[string]*RepoContext),
		controlSessionLocks: make(map[string]*sync.Mutex),
	}
}

func deriveRepoID(projectRoot string) string {
	repoID, err := xdg.RepoID(projectRoot)
	if err != nil || repoID == "" {
		repoID = filepath.Base(projectRoot)
	}
	return repoID
}

func openCodeServerSessionName(projectRoot string) string {
	return fmt.Sprintf("orch-opencode-server-%s", deriveRepoID(projectRoot))
}

// RegisterRepo adds a new repo context to the daemon.
func (s *SocketServer) RegisterRepo(projectRoot string, st store.Store) (string, error) {
	repoID := deriveRepoID(projectRoot)

	s.reposMu.Lock()
	defer s.reposMu.Unlock()

	s.repos[repoID] = &RepoContext{
		ProjectRoot: projectRoot,
		RepoID:      repoID,
		Store:       st,
	}

	return repoID, nil
}

// GetRepoContext returns the context for a repo by ID or project root.
func (s *SocketServer) GetRepoContext(repoIDOrPath string) *RepoContext {
	s.reposMu.RLock()
	defer s.reposMu.RUnlock()

	// Try direct repoID lookup
	if ctx, ok := s.repos[repoIDOrPath]; ok {
		return ctx
	}

	// Try to find by project root path
	for _, ctx := range s.repos {
		if ctx.ProjectRoot == repoIDOrPath {
			return ctx
		}
	}

	return nil
}

func (s *SocketServer) GetAllRepoContexts() []*RepoContext {
	s.reposMu.RLock()
	defer s.reposMu.RUnlock()

	contexts := make([]*RepoContext, 0, len(s.repos))
	for _, ctx := range s.repos {
		contexts = append(contexts, ctx)
	}
	return contexts
}

func (s *SocketServer) resolveStore(req SendRequest) store.Store {
	s.logger.Printf("resolveStore: type=%s repoID=%q projectRoot=%q issuesRoot=%q",
		req.Type, req.RepoID, req.ProjectRoot, req.IssuesRoot)

	if req.RepoID != "" {
		if ctx := s.GetRepoContext(req.RepoID); ctx != nil {
			s.logger.Printf("resolveStore: found by repoID=%q", req.RepoID)
			return ctx.Store
		}
	}

	if req.ProjectRoot != "" {
		repoID, _ := xdg.RepoID(req.ProjectRoot)
		if repoID != "" {
			if ctx := s.GetRepoContext(repoID); ctx != nil {
				s.logger.Printf("resolveStore: found by projectRoot repoID=%q", repoID)
				return ctx.Store
			}
		}
		if ctx := s.GetRepoContext(req.ProjectRoot); ctx != nil {
			s.logger.Printf("resolveStore: found by projectRoot path=%q", req.ProjectRoot)
			return ctx.Store
		}
	}

	if req.IssuesRoot != "" {
		s.logger.Printf("resolveStore: creating store for issuesRoot=%q", req.IssuesRoot)
		return s.getOrCreateStore(req.IssuesRoot, req.ProjectRoot)
	}

	s.logger.Printf("resolveStore: no store found (all fields empty or no match)")
	return nil
}

func (s *SocketServer) getOrCreateStore(issuesRoot, projectRoot string) store.Store {
	s.reposMu.Lock()
	defer s.reposMu.Unlock()

	cacheKey := issuesRoot
	if ctx, ok := s.repos[cacheKey]; ok {
		return ctx.Store
	}

	st, err := s.storeFactory(issuesRoot)
	if err != nil {
		s.logger.Printf("failed to create store for %s: %v", issuesRoot, err)
		return nil
	}

	repoID := cacheKey
	if projectRoot != "" {
		if id, err := xdg.RepoID(projectRoot); err == nil && id != "" {
			repoID = id
		}
	}

	s.repos[cacheKey] = &RepoContext{
		ProjectRoot: projectRoot,
		RepoID:      repoID,
		Store:       st,
	}

	return st
}

func (s *SocketServer) SetGitHubBackend(backend *github.Backend) {
	s.githubBackend = backend
}

func (s *SocketServer) Start() error {
	socketPath := xdg.SocketPath()

	// Ensure runtime directory exists
	if err := xdg.EnsureRuntimeDir(); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	if err := os.Chmod(socketPath, 0600); err != nil {
		s.listener.Close()
		os.Remove(socketPath)
		return fmt.Errorf("failed to secure socket permissions: %w", err)
	}

	s.logger.Printf("socket server listening on %s", socketPath)

	go s.acceptLoop()
	s.StartStaleMonitorCleanup()

	return nil
}

func (s *SocketServer) Stop() {
	close(s.stopCh)
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(xdg.SocketPath())
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
	case "close_issue":
		s.handleCloseIssue(req, encoder)
	case "get_attach_info":
		s.handleGetAttachInfo(req, encoder)
	case "get_run_by_short_id":
		s.handleGetRunByShortID(req, encoder)
	case "register_monitor":
		s.handleRegisterMonitor(req, encoder)
	case "unregister_monitor":
		s.handleUnregisterMonitor(req, encoder)
	case "monitor_heartbeat":
		s.handleMonitorHeartbeat(req, encoder)
	case "list_monitors":
		s.handleListMonitors(req, encoder)
	case "kill_monitor":
		s.handleKillMonitor(req, encoder)
	case "register_repo":
		s.handleRegisterRepo(req, encoder)
	case "list_repos":
		s.handleListRepos(req, encoder)
	case "get_control_session":
		s.handleGetControlSession(req, encoder)
	case "set_control_session":
		s.handleSetControlSession(req, encoder)
	case "clear_control_session":
		s.handleClearControlSession(req, encoder)
	case "ensure_opencode_server":
		s.handleEnsureOpenCodeServer(req, encoder)
	default:
		encoder.Encode(SendResponse{OK: false, Error: "unknown_type"})
	}
}

// handleRegisterRepo handles repo registration requests from clients.
func (s *SocketServer) handleRegisterRepo(req SendRequest, encoder *json.Encoder) {
	if req.ProjectRoot == "" {
		encoder.Encode(SendResponse{OK: false, Error: "project_root required"})
		return
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
	encoder.Encode(map[string]interface{}{
		"ok":      true,
		"repo_id": repoID,
	})
}

// handleListRepos returns list of registered repos.
func (s *SocketServer) handleListRepos(req SendRequest, encoder *json.Encoder) {
	s.reposMu.RLock()
	repos := make([]map[string]string, 0, len(s.repos))
	for _, ctx := range s.repos {
		repos = append(repos, map[string]string{
			"repo_id":      ctx.RepoID,
			"project_root": ctx.ProjectRoot,
		})
	}
	s.reposMu.RUnlock()

	encoder.Encode(map[string]interface{}{
		"ok":    true,
		"repos": repos,
	})
}

func (s *SocketServer) controlSessionPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".orch", "control-session.json")
}

// getControlSessionLock returns a per-project mutex for control session operations.
// This prevents race conditions when multiple orch-monitor instances start in quick succession.
func (s *SocketServer) getControlSessionLock(projectRoot string) *sync.Mutex {
	s.controlSessionLocksMu.Lock()
	defer s.controlSessionLocksMu.Unlock()

	if lock, ok := s.controlSessionLocks[projectRoot]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	s.controlSessionLocks[projectRoot] = lock
	return lock
}

// discoverClaudeSession finds the most recent Claude Code session for a project directory.
// It scans ~/.claude/projects/{project-dir}/*.jsonl for UUID-named session files.
func (s *SocketServer) discoverClaudeSession(projectRoot string) string {
	// Claude stores projects with path converted: /Users/foo/bar -> -Users-foo-bar
	claudeDirName := strings.ReplaceAll(projectRoot, "/", "-")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		s.logger.Printf("failed to get home dir: %v", err)
		return ""
	}

	claudeProjectsDir := filepath.Join(homeDir, ".claude", "projects", claudeDirName)
	entries, err := os.ReadDir(claudeProjectsDir)
	if err != nil {
		s.logger.Printf("no Claude project dir found: %s", claudeProjectsDir)
		return ""
	}

	// UUID pattern: 8-4-4-4-12 hex chars
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

	type sessionFile struct {
		name    string
		modTime time.Time
	}
	var sessions []sessionFile

	for _, entry := range entries {
		if entry.IsDir() || !uuidPattern.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		sessions = append(sessions, sessionFile{
			name:    entry.Name(),
			modTime: info.ModTime(),
		})
	}

	if len(sessions) == 0 {
		s.logger.Printf("no Claude sessions found in: %s", claudeProjectsDir)
		return ""
	}

	// Sort by modification time, newest first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].modTime.After(sessions[j].modTime)
	})

	// Return UUID without .jsonl extension
	sessionID := strings.TrimSuffix(sessions[0].name, ".jsonl")
	s.logger.Printf("discovered Claude session for %s: %s", projectRoot, sessionID)
	return sessionID
}

// saveControlSession persists the control session to disk.
func (s *SocketServer) saveControlSession(projectRoot, sessionID, agentType string) error {
	orchDir := filepath.Join(projectRoot, ".orch")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		return fmt.Errorf("failed to create .orch dir: %w", err)
	}

	sessionPath := s.controlSessionPath(projectRoot)
	sessionData := map[string]string{
		"session_id": sessionID,
		"agent_type": agentType,
	}
	data, _ := json.MarshalIndent(sessionData, "", "  ")
	if err := os.WriteFile(sessionPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}
	return nil
}

func (s *SocketServer) handleGetControlSession(req SendRequest, encoder *json.Encoder) {
	if req.ProjectRoot == "" {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": "project_root required",
		})
		return
	}

	// Acquire per-project lock to prevent race conditions
	lock := s.getControlSessionLock(req.ProjectRoot)
	lock.Lock()
	defer lock.Unlock()

	requestedAgent := req.AgentType // The agent type the client wants to use

	// Load stored session
	sessionPath := s.controlSessionPath(req.ProjectRoot)
	var storedSession struct {
		SessionID string `json:"session_id"`
		AgentType string `json:"agent_type"`
	}

	data, err := os.ReadFile(sessionPath)
	if err == nil {
		json.Unmarshal(data, &storedSession)
	}

	// If agent type changed, clear the stored session
	if storedSession.SessionID != "" && storedSession.AgentType != "" && requestedAgent != "" {
		if storedSession.AgentType != requestedAgent {
			s.logger.Printf("agent changed from %s to %s, clearing stored session", storedSession.AgentType, requestedAgent)
			os.Remove(sessionPath)
			storedSession.SessionID = ""
			storedSession.AgentType = ""
		}
	}

	// If we have a valid stored session, return it
	if storedSession.SessionID != "" {
		encoder.Encode(map[string]interface{}{
			"ok":         true,
			"session_id": storedSession.SessionID,
			"agent_type": storedSession.AgentType,
		})
		return
	}

	// No stored session - try to discover one based on agent type
	var discoveredSession string
	if requestedAgent == "claude" {
		discoveredSession = s.discoverClaudeSession(req.ProjectRoot)
		if discoveredSession != "" {
			// Save the discovered session
			if err := s.saveControlSession(req.ProjectRoot, discoveredSession, "claude"); err != nil {
				s.logger.Printf("failed to save discovered session: %v", err)
			}
		}
	}
	// For opencode, discovery is handled via its HTTP API (client-side for now)

	encoder.Encode(map[string]interface{}{
		"ok":         true,
		"session_id": discoveredSession,
		"agent_type": requestedAgent,
	})
}

func (s *SocketServer) handleSetControlSession(req SendRequest, encoder *json.Encoder) {
	if req.ProjectRoot == "" {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": "project_root required",
		})
		return
	}

	if req.SessionID == "" {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": "session_id required",
		})
		return
	}

	// Acquire per-project lock to prevent race conditions
	lock := s.getControlSessionLock(req.ProjectRoot)
	lock.Lock()
	defer lock.Unlock()

	orchDir := filepath.Join(req.ProjectRoot, ".orch")
	if err := os.MkdirAll(orchDir, 0755); err != nil {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": fmt.Sprintf("failed to create .orch dir: %v", err),
		})
		return
	}

	sessionPath := s.controlSessionPath(req.ProjectRoot)
	sessionData := map[string]string{"session_id": req.SessionID}
	if req.AgentType != "" {
		sessionData["agent_type"] = req.AgentType
	}
	data, _ := json.MarshalIndent(sessionData, "", "  ")
	if err := os.WriteFile(sessionPath, data, 0644); err != nil {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": fmt.Sprintf("failed to write session file: %v", err),
		})
		return
	}

	s.logger.Printf("set control session for %s: %s (agent: %s)", req.ProjectRoot, req.SessionID, req.AgentType)
	encoder.Encode(map[string]interface{}{
		"ok": true,
	})
}

func (s *SocketServer) handleClearControlSession(req SendRequest, encoder *json.Encoder) {
	if req.ProjectRoot == "" {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": "project_root required",
		})
		return
	}

	// Acquire per-project lock to prevent race conditions
	lock := s.getControlSessionLock(req.ProjectRoot)
	lock.Lock()
	defer lock.Unlock()

	sessionPath := s.controlSessionPath(req.ProjectRoot)
	if err := os.Remove(sessionPath); err != nil && !os.IsNotExist(err) {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": fmt.Sprintf("failed to remove session file: %v", err),
		})
		return
	}

	s.logger.Printf("cleared control session for %s", req.ProjectRoot)
	encoder.Encode(map[string]interface{}{
		"ok": true,
	})
}

func (s *SocketServer) handleEnsureOpenCodeServer(req SendRequest, encoder *json.Encoder) {
	if req.ProjectRoot == "" {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": "project_root required",
		})
		return
	}

	tmuxSessionName := openCodeServerSessionName(req.ProjectRoot)

	port, err := s.ensureOpenCodeServerRunning(req.ProjectRoot, tmuxSessionName)
	if err != nil {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	sessionID, err := s.getOrCreateOpenCodeControlSession(req.ProjectRoot, port)
	if err != nil {
		s.logger.Printf("warning: server running but failed to get session: %v", err)
		encoder.Encode(map[string]interface{}{
			"ok":   true,
			"port": port,
		})
		return
	}

	encoder.Encode(map[string]interface{}{
		"ok":         true,
		"port":       port,
		"session_id": sessionID,
	})
}

func (s *SocketServer) ensureOpenCodeServerRunning(projectRoot string, tmuxSessionName string) (int, error) {
	mux := multiplexer.NewTmuxMultiplexer()

	if mux.HasSession(tmuxSessionName) {
		for i := 0; i < 10; i++ {
			foundPort := agent.FindRunningOpenCodeServerForWorktree(projectRoot, agent.OpenCodeServerPortStart, agent.OpenCodeServerPortEnd)
			if foundPort > 0 {
				s.logger.Printf("opencode server healthy on port %d (session: %s)", foundPort, tmuxSessionName)
				return foundPort, nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		s.logger.Printf("session %s exists but server not healthy, killing and restarting", tmuxSessionName)
		if err := mux.KillSession(tmuxSessionName); err != nil {
			s.logger.Printf("warning: failed to kill stale session %s: %v", tmuxSessionName, err)
		}
	}

	port := findAvailablePort(agent.OpenCodeServerPortStart, agent.OpenCodeServerPortEnd)
	if port == 0 {
		return 0, fmt.Errorf("no available port found for opencode server")
	}

	adapter := &agent.OpenCodeAdapter{}
	launchCmd, err := adapter.LaunchCommand(&agent.LaunchConfig{
		Type: agent.AgentOpenCode,
		Port: port,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get launch command: %w", err)
	}

	err = mux.NewSession(&multiplexer.SessionConfig{
		SessionName: tmuxSessionName,
		WorkDir:     projectRoot,
		Command:     launchCmd,
		Env:         adapter.Env(),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to start opencode server: %w", err)
	}

	client := agent.NewOpenCodeClient(port)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.WaitForHealthy(ctx, 30*time.Second); err != nil {
		s.logger.Printf("opencode server failed to become healthy: %v", err)
		return 0, fmt.Errorf("server started but failed health check: %w", err)
	}

	s.logger.Printf("started opencode server on port %d (session: %s)", port, tmuxSessionName)
	return port, nil
}

func (s *SocketServer) getOrCreateOpenCodeControlSession(projectRoot string, port int) (string, error) {
	lock := s.getControlSessionLock(projectRoot)
	lock.Lock()
	defer lock.Unlock()

	sessionPath := s.controlSessionPath(projectRoot)
	if data, err := os.ReadFile(sessionPath); err == nil {
		var stored struct {
			SessionID string `json:"session_id"`
			AgentType string `json:"agent_type"`
			Port      int    `json:"port"`
		}
		if json.Unmarshal(data, &stored) == nil && stored.SessionID != "" && stored.AgentType == "opencode" {
			client := agent.NewOpenCodeClient(port)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if session, err := client.GetSession(ctx, stored.SessionID, projectRoot); err == nil && session != nil {
				s.logger.Printf("reusing existing opencode control session: %s", stored.SessionID)
				if stored.Port != port {
					s.saveControlSession(projectRoot, stored.SessionID, "opencode")
				}
				return stored.SessionID, nil
			}
		}
	}

	client := agent.NewOpenCodeClient(port)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.CreateSession(ctx, "orch-control", projectRoot)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	if err := s.saveControlSession(projectRoot, session.ID, "opencode"); err != nil {
		s.logger.Printf("warning: failed to save control session: %v", err)
	}

	prompt := getControlPromptInstruction()
	if prompt != "" {
		go func() {
			sendCtx, sendCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer sendCancel()
			if err := client.SendMessageAsync(sendCtx, session.ID, prompt, projectRoot, nil, ""); err != nil {
				s.logger.Printf("warning: failed to send initial prompt to control session: %v", err)
			} else {
				s.logger.Printf("sent initial prompt to control session %s", session.ID)
			}
		}()
	}

	s.logger.Printf("created new opencode control session: %s", session.ID)
	return session.ID, nil
}

func getControlPromptInstruction() string {
	return "ultrathink Please read 'ORCH_CONTROL_PROMPT.md' in the current directory and follow the instructions found there."
}

func findAvailablePort(start, end int) int {
	for port := start; port <= end; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			return port
		}
	}
	return 0
}

func (s *SocketServer) handleSend(req SendRequest, encoder *json.Encoder) {
	err := s.processSend(req)
	if err != nil {
		encoder.Encode(SendResponse{OK: false, Error: err.Error()})
		return
	}
	encoder.Encode(SendResponse{OK: true})
}

func (s *SocketServer) processSend(req SendRequest) error {
	s.logger.Printf("processing send for %s#%s", req.IssueID, req.RunID)

	st := s.resolveStore(req)
	if st == nil {
		return fmt.Errorf("no store available for project")
	}

	ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
	run, err := st.GetRun(ref)
	if err != nil {
		return fmt.Errorf("run %s#%s not found: %w", req.IssueID, req.RunID, err)
	}

	if run.Agent != string(agent.AgentOpenCode) {
		return fmt.Errorf("run %s#%s is not an opencode agent (agent=%s)", req.IssueID, req.RunID, run.Agent)
	}

	if run.ServerPort <= 0 {
		return fmt.Errorf("run %s#%s missing server port (not running or server not started)", req.IssueID, req.RunID)
	}
	if run.OpenCodeSessionID == "" {
		return fmt.Errorf("run %s#%s missing session ID (agent may still be booting)", req.IssueID, req.RunID)
	}

	client := agent.NewOpenCodeClient(run.ServerPort)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use async endpoint to return immediately after queuing the message
	// (SendMessagePrompt blocks until the agent finishes processing)
	err = client.SendMessageAsync(ctx, run.OpenCodeSessionID, req.Message, run.WorktreePath, nil, "")
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	s.logger.Printf("message sent successfully to %s#%s", req.IssueID, req.RunID)
	return nil
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

	// If --all flag is set, aggregate runs from all repos
	var runs []*model.Run
	if req.All {
		runs, err = s.listAllRepoRuns(req)
	} else {
		st := s.resolveStore(req)
		if st == nil {
			encoder.Encode(ListRunsResponse{OK: false, Error: "no store available"})
			return
		}
		filter := &store.ListRunsFilter{
			IssueID: req.IssueID,
			Limit:   0,
		}
		for _, status := range req.Status {
			filter.Status = append(filter.Status, model.Status(status))
		}
		runs, err = st.ListRuns(filter)
	}

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

// listAllRepoRuns aggregates runs from all registered repos.
func (s *SocketServer) listAllRepoRuns(req SendRequest) ([]*model.Run, error) {
	// Copy store pointers under lock, then release before I/O
	s.reposMu.RLock()
	stores := make([]store.Store, 0, len(s.repos))
	for _, ctx := range s.repos {
		if ctx.Store != nil {
			stores = append(stores, ctx.Store)
		}
	}
	s.reposMu.RUnlock()

	// Now do I/O without holding the lock
	var allRuns []*model.Run
	for _, st := range stores {
		filter := &store.ListRunsFilter{
			IssueID: req.IssueID,
		}
		for _, status := range req.Status {
			filter.Status = append(filter.Status, model.Status(status))
		}
		runs, err := st.ListRuns(filter)
		if err != nil {
			continue // Skip errors, aggregate what we can
		}
		allRuns = append(allRuns, runs...)
	}

	// Sort by updated time, most recent first
	sort.Slice(allRuns, func(i, j int) bool {
		return allRuns[i].UpdatedAt.After(allRuns[j].UpdatedAt)
	})

	return allRuns, nil
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

	var issues []*model.Issue
	if s.githubBackend != nil {
		issues, err = s.githubBackend.ListFromCache()
	} else {
		st := s.resolveStore(req)
		if st == nil {
			encoder.Encode(ListIssuesResponse{OK: false, Error: "no store available"})
			return
		}
		issues, err = st.ListIssues()
	}
	if err != nil {
		s.logger.Printf("error listing issues: %v", err)
		encoder.Encode(ListIssuesResponse{OK: false, Error: "store_error"})
		return
	}

	// Sort by modification time, newest first
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ModifiedAt.After(issues[j].ModifiedAt)
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

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(GetRunResponse{OK: false, Error: "no store available"})
		return
	}

	ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
	run, err := st.GetRun(ref)
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

	var issue *model.Issue
	var err error

	if s.githubBackend != nil {
		issue, err = s.githubBackend.GetByIDFromCache(req.IssueID)
	} else {
		st := s.resolveStore(req)
		if st == nil {
			encoder.Encode(GetIssueResponse{OK: false, Error: "no store available"})
			return
		}
		issue, err = st.ResolveIssue(req.IssueID)
	}

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

func SendViaDaemon(projectRoot, issuesRoot string, run *model.Run, message string, noEnter bool) error {
	socketPath := xdg.SocketPath()

	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(35 * time.Second))

	req := SendRequest{
		Type:        "send",
		IssueID:     run.IssueID,
		RunID:       run.RunID,
		Message:     message,
		NoEnter:     noEnter,
		ProjectRoot: projectRoot,
		IssuesRoot:  issuesRoot,
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

// IsDaemonSocketAvailable checks if the global daemon socket exists.
func IsDaemonSocketAvailable(_ string) bool {
	socketPath := xdg.SocketPath()
	_, err := os.Stat(socketPath)
	return err == nil
}

func (s *SocketServer) handleStopRun(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(StopRunResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(StopRunResponse{OK: false, Error: "no store available"})
		return
	}

	var stoppedRuns []string

	if req.RunID != "" {
		ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
		run, err := st.GetRun(ref)
		if err != nil {
			s.logger.Printf("error getting run %s#%s: %v", req.IssueID, req.RunID, err)
			encoder.Encode(StopRunResponse{OK: false, Error: "not_found"})
			return
		}
		if err := s.stopSingleRun(run, st); err != nil {
			s.logger.Printf("error stopping run %s#%s: %v", req.IssueID, req.RunID, err)
			encoder.Encode(StopRunResponse{OK: false, Error: err.Error()})
			return
		}
		stoppedRuns = append(stoppedRuns, run.RunID)
	} else {
		runs, err := st.ListRuns(&store.ListRunsFilter{
			IssueID: req.IssueID,
			Status:  []model.Status{model.StatusRunning, model.StatusBooting, model.StatusBlocked, model.StatusBlockedAPI, model.StatusQueued},
		})
		if err != nil {
			s.logger.Printf("error listing runs for %s: %v", req.IssueID, err)
			encoder.Encode(StopRunResponse{OK: false, Error: "store_error"})
			return
		}
		for _, run := range runs {
			if err := s.stopSingleRun(run, st); err != nil {
				s.logger.Printf("error stopping run %s#%s: %v", run.IssueID, run.RunID, err)
			} else {
				stoppedRuns = append(stoppedRuns, run.RunID)
			}
		}
	}

	encoder.Encode(StopRunResponse{
		OK:           true,
		StoppedRuns:  stoppedRuns,
		StoppedCount: len(stoppedRuns),
	})
}

func (s *SocketServer) stopSingleRun(run *model.Run, st store.Store) error {
	if run.Status == model.StatusDone || run.Status == model.StatusFailed || run.Status == model.StatusCanceled {
		return nil
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	muxType, _ := multiplexer.ParseType(run.Multiplexer)
	mux, _ := multiplexer.GetMultiplexer(muxType)
	if mux != nil && mux.HasSession(sessionName) {
		if err := mux.KillSession(sessionName); err != nil {
			s.logger.Printf("warning: failed to kill session %s: %v", sessionName, err)
		}
	}

	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	event := &model.Event{
		Timestamp: time.Now(),
		Type:      "status",
		Name:      string(model.StatusCanceled),
	}
	return st.AppendEvent(ref, event)
}

func (s *SocketServer) handleResolveIssue(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(ResolveIssueResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(ResolveIssueResponse{OK: false, Error: "no store available"})
		return
	}

	issue, err := st.ResolveIssue(req.IssueID)
	if err != nil {
		s.logger.Printf("error getting issue %s: %v", req.IssueID, err)
		encoder.Encode(ResolveIssueResponse{OK: false, Error: "not_found"})
		return
	}

	if issue.Status == model.IssueStatusResolved {
		encoder.Encode(ResolveIssueResponse{OK: true, IssueID: req.IssueID})
		return
	}

	forceResolve := req.Force
	if !forceResolve {
		runs, err := st.ListRuns(&store.ListRunsFilter{IssueID: req.IssueID})
		if err != nil {
			s.logger.Printf("error listing runs for %s: %v", req.IssueID, err)
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

	if err := st.SetIssueStatus(req.IssueID, model.IssueStatusResolved); err != nil {
		s.logger.Printf("error resolving issue %s: %v", req.IssueID, err)
		encoder.Encode(ResolveIssueResponse{OK: false, Error: "store_error"})
		return
	}

	s.logger.Printf("resolved issue: %s", req.IssueID)
	encoder.Encode(ResolveIssueResponse{OK: true, IssueID: req.IssueID})
}

func (s *SocketServer) handleCreateIssue(req SendRequest, encoder *json.Encoder) {
	title := req.Title
	if title == "" {
		title = req.IssueID
	}
	if title == "" {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "invalid_request: title required"})
		return
	}

	if s.githubBackend != nil {
		issue, err := s.githubBackend.Create(title, req.Body, nil)
		if err != nil {
			s.logger.Printf("error creating GitHub issue: %v", err)
			encoder.Encode(CreateIssueResponse{OK: false, Error: "github_error: " + err.Error()})
			return
		}
		s.logger.Printf("created GitHub issue: %s (%s)", issue.ID, issue.Path)
		encoder.Encode(CreateIssueResponse{OK: true, IssueID: issue.ID, Path: issue.Path})
		return
	}

	if req.IssueID == "" {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	if strings.Contains(req.IssueID, "/") || strings.Contains(req.IssueID, "..") || strings.Contains(req.IssueID, "\\") {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "invalid_request: issue_id contains invalid characters"})
		return
	}

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "no store available"})
		return
	}

	// Use vault path for file-based issues (not project root)
	issuesRoot := st.RootPath()
	issuesDir := filepath.Join(issuesRoot, "issues")
	if _, err := os.Stat(filepath.Join(issuesRoot, "Issues")); err == nil {
		issuesDir = filepath.Join(issuesRoot, "Issues")
	}
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		s.logger.Printf("error creating issues directory: %v", err)
		encoder.Encode(CreateIssueResponse{OK: false, Error: "io_error"})
		return
	}

	issuePath := filepath.Join(issuesDir, req.IssueID+".md")
	if _, err := os.Stat(issuePath); err == nil {
		encoder.Encode(CreateIssueResponse{OK: false, Error: "already_exists"})
		return
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("type: issue\n")
	sb.WriteString("id: " + req.IssueID + "\n")
	sb.WriteString("title: " + title + "\n")
	if req.Summary != "" {
		sb.WriteString("summary: " + req.Summary + "\n")
	}
	sb.WriteString("status: open\n")
	sb.WriteString("---\n\n")
	sb.WriteString("# " + title + "\n\n")
	if req.Body != "" {
		sb.WriteString(req.Body)
		sb.WriteString("\n")
	}

	if err := os.WriteFile(issuePath, []byte(sb.String()), 0644); err != nil {
		s.logger.Printf("error writing issue file: %v", err)
		encoder.Encode(CreateIssueResponse{OK: false, Error: "io_error"})
		return
	}

	s.logger.Printf("created issue: %s at %s", req.IssueID, issuePath)
	encoder.Encode(CreateIssueResponse{OK: true, IssueID: req.IssueID, Path: issuePath})
}

func (s *SocketServer) handleCloseIssue(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(CloseIssueResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	if s.githubBackend != nil {
		number, err := model.ParseGitHubIssueNumber(req.IssueID)
		if err != nil {
			encoder.Encode(CloseIssueResponse{OK: false, Error: "invalid_issue_id: " + err.Error()})
			return
		}
		if err := s.githubBackend.Close(number, req.Comment); err != nil {
			s.logger.Printf("error closing GitHub issue %s: %v", req.IssueID, err)
			encoder.Encode(CloseIssueResponse{OK: false, Error: "github_error: " + err.Error()})
			return
		}
		s.logger.Printf("closed GitHub issue: %s", req.IssueID)
		encoder.Encode(CloseIssueResponse{OK: true, IssueID: req.IssueID})
		return
	}

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(CloseIssueResponse{OK: false, Error: "no store available"})
		return
	}

	if err := st.SetIssueStatus(req.IssueID, model.IssueStatusClosed); err != nil {
		s.logger.Printf("error closing issue %s: %v", req.IssueID, err)
		encoder.Encode(CloseIssueResponse{OK: false, Error: "not_found"})
		return
	}

	s.logger.Printf("closed issue: %s", req.IssueID)
	encoder.Encode(CloseIssueResponse{OK: true, IssueID: req.IssueID})
}

func (s *SocketServer) handleGetAttachInfo(req SendRequest, encoder *json.Encoder) {
	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(GetAttachInfoResponse{OK: false, Error: "no store available"})
		return
	}

	var run *model.Run
	var err error

	if req.RunID != "" {
		ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
		run, err = st.GetRun(ref)
	} else if req.ShortID != "" {
		run, err = st.GetRunByShortID(req.ShortID)
	} else if req.IssueID != "" {
		run, err = st.GetLatestRun(req.IssueID)
	} else {
		encoder.Encode(GetAttachInfoResponse{OK: false, Error: "invalid_request: issue_id, run_id, or short_id required"})
		return
	}

	if err != nil {
		s.logger.Printf("error getting run for attach: %v", err)
		encoder.Encode(GetAttachInfoResponse{OK: false, Error: "not_found"})
		return
	}

	tmuxSession := run.TmuxSession
	if tmuxSession == "" {
		tmuxSession = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	serverPort := run.ServerPort
	if run.Agent == "opencode" && serverPort == 0 {
		serverPort = agent.FindRunningOpenCodeServerForWorktree(run.WorktreePath, agent.OpenCodeServerPortStart, agent.OpenCodeServerPortEnd)
	}

	encoder.Encode(GetAttachInfoResponse{
		OK:                true,
		IssueID:           run.IssueID,
		RunID:             run.RunID,
		Agent:             run.Agent,
		TmuxSession:       tmuxSession,
		Multiplexer:       run.Multiplexer,
		WorktreePath:      run.WorktreePath,
		ServerPort:        serverPort,
		OpenCodeSessionID: run.OpenCodeSessionID,
		Branch:            run.Branch,
	})
}

func (s *SocketServer) handleGetRunByShortID(req SendRequest, encoder *json.Encoder) {
	shortID := req.ShortID
	if shortID == "" {
		encoder.Encode(GetRunResponse{OK: false, Error: "invalid_request: short_id required"})
		return
	}

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(GetRunResponse{OK: false, Error: "no store available"})
		return
	}

	run, err := st.GetRunByShortID(shortID)
	if err != nil {
		s.logger.Printf("error getting run by short id %s: %v", shortID, err)
		encoder.Encode(GetRunResponse{OK: false, Error: "not_found"})
		return
	}

	encoder.Encode(GetRunResponse{
		OK:  true,
		Run: RunToFull(run),
	})
}

func (s *SocketServer) handleRegisterMonitor(req SendRequest, encoder *json.Encoder) {
	monitorID := fmt.Sprintf("mon-%d-%d", req.Limit, time.Now().UnixNano()%100000)
	if req.ShortID != "" {
		monitorID = req.ShortID
	}

	monitorType := "go"
	if req.Title != "" {
		monitorType = req.Title
	}

	view := "dashboard"
	if req.Summary != "" {
		view = req.Summary
	}

	conn := &MonitorConnection{
		ID:          monitorID,
		PID:         req.Limit,
		Type:        monitorType,
		View:        view,
		StartedAt:   time.Now(),
		LastSeen:    time.Now(),
		Project:     req.ProjectRoot,
		TmuxSession: req.Body,
	}

	s.monitorsMu.Lock()
	s.monitors[monitorID] = conn
	s.monitorsMu.Unlock()

	s.logger.Printf("monitor registered: %s (pid=%d, type=%s, view=%s)", monitorID, conn.PID, conn.Type, conn.View)

	encoder.Encode(RegisterMonitorResponse{
		OK:        true,
		MonitorID: monitorID,
	})
}

func (s *SocketServer) handleUnregisterMonitor(req SendRequest, encoder *json.Encoder) {
	monitorID := req.ShortID
	if monitorID == "" {
		encoder.Encode(SendResponse{OK: false, Error: "invalid_request: monitor_id required"})
		return
	}

	s.monitorsMu.Lock()
	conn, exists := s.monitors[monitorID]
	if exists {
		delete(s.monitors, monitorID)
	}
	s.monitorsMu.Unlock()

	if !exists {
		encoder.Encode(SendResponse{OK: false, Error: "not_found"})
		return
	}

	s.logger.Printf("monitor unregistered: %s (pid=%d)", monitorID, conn.PID)
	encoder.Encode(SendResponse{OK: true})
}

func (s *SocketServer) handleMonitorHeartbeat(req SendRequest, encoder *json.Encoder) {
	monitorID := req.ShortID
	if monitorID == "" {
		encoder.Encode(HeartbeatResponse{OK: false, Error: "invalid_request: monitor_id required"})
		return
	}

	s.monitorsMu.Lock()
	conn, exists := s.monitors[monitorID]
	if exists {
		conn.LastSeen = time.Now()
	}
	s.monitorsMu.Unlock()

	if !exists {
		encoder.Encode(HeartbeatResponse{OK: false, Error: "not_found"})
		return
	}

	encoder.Encode(HeartbeatResponse{OK: true})
}

func (s *SocketServer) handleListMonitors(req SendRequest, encoder *json.Encoder) {
	s.monitorsMu.RLock()
	// Copy values while holding lock to avoid race with heartbeat updates
	monitors := make([]*MonitorConnection, 0, len(s.monitors))
	for _, conn := range s.monitors {
		if req.ProjectRoot != "" && !req.Force && conn.Project != req.ProjectRoot {
			continue
		}
		// Copy the struct to avoid race conditions
		snapshot := *conn
		monitors = append(monitors, &snapshot)
	}
	s.monitorsMu.RUnlock()

	sort.Slice(monitors, func(i, j int) bool {
		return monitors[i].StartedAt.Before(monitors[j].StartedAt)
	})

	encoder.Encode(ListMonitorsResponse{
		OK:       true,
		Monitors: monitors,
	})
}

func (s *SocketServer) handleKillMonitor(req SendRequest, encoder *json.Encoder) {
	killAll := req.Force
	global := req.Cursor != ""
	monitorID := req.ShortID

	if !killAll && monitorID == "" {
		encoder.Encode(KillMonitorResponse{OK: false, Error: "invalid_request: monitor_id required or use --all"})
		return
	}

	s.monitorsMu.Lock()
	var toKill []MonitorConnection
	var toKillIDs []string
	if killAll {
		for id, conn := range s.monitors {
			if global || conn.Project == req.ProjectRoot {
				toKill = append(toKill, *conn)
				toKillIDs = append(toKillIDs, id)
			}
		}
	} else if conn, exists := s.monitors[monitorID]; exists {
		toKill = append(toKill, *conn)
		toKillIDs = append(toKillIDs, monitorID)
	}
	s.monitorsMu.Unlock()

	if len(toKill) == 0 && !killAll {
		encoder.Encode(KillMonitorResponse{OK: false, Error: "not_found"})
		return
	}

	var killedIDs, failedIDs []string
	for i, conn := range toKill {
		if err := s.killMonitorProcess(&conn); err != nil {
			s.logger.Printf("failed to kill monitor %s (pid=%d): %v", conn.ID, conn.PID, err)
			failedIDs = append(failedIDs, conn.ID)
		} else {
			s.logger.Printf("killed monitor %s (pid=%d)", conn.ID, conn.PID)
			killedIDs = append(killedIDs, conn.ID)
			s.monitorsMu.Lock()
			delete(s.monitors, toKillIDs[i])
			s.monitorsMu.Unlock()
		}
	}

	encoder.Encode(KillMonitorResponse{
		OK:          true,
		KilledIDs:   killedIDs,
		KilledCount: len(killedIDs),
		FailedIDs:   failedIDs,
		FailedCount: len(failedIDs),
	})
}

func (s *SocketServer) killMonitorProcess(conn *MonitorConnection) error {
	if conn.PID <= 0 {
		return fmt.Errorf("invalid pid")
	}

	process, err := os.FindProcess(conn.PID)
	if err != nil {
		return err
	}

	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}

func (s *SocketServer) StartStaleMonitorCleanup() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.cleanupStaleMonitors()
			}
		}
	}()
}

func (s *SocketServer) cleanupStaleMonitors() {
	threshold := time.Now().Add(-60 * time.Second)

	s.monitorsMu.Lock()
	defer s.monitorsMu.Unlock()

	for id, conn := range s.monitors {
		if conn.LastSeen.Before(threshold) {
			s.logger.Printf("cleaning up stale monitor %s (pid=%d, last_seen=%s)", id, conn.PID, conn.LastSeen.Format(time.RFC3339))
			delete(s.monitors, id)
		}
	}
}
