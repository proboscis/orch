package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/github"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/xdg"
)

const (
	socketFile = "daemon.sock"
)

func readAll(conn net.Conn) ([]byte, error) {
	var data []byte
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return data, nil
}

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

	Agent      string   `json:"agent,omitempty"`
	TextSearch string   `json:"text_search,omitempty"`
	TimeRange  string   `json:"time_range,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	TagsMode   string   `json:"tags_mode,omitempty"`

	EventType   string            `json:"event_type,omitempty"`
	EventName   string            `json:"event_name,omitempty"`
	EventAttrs  map[string]string `json:"event_attrs,omitempty"`
	EventSource string            `json:"event_source,omitempty"`
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

	controlSessionLocks   map[string]*sync.Mutex
	controlSessionLocksMu sync.Mutex

	openCodeServers   map[string]*managedServer
	openCodeServersMu sync.RWMutex
}

type managedServer struct {
	ProjectRoot string
	Port        int
	Cmd         *exec.Cmd
	StartTime   time.Time
	LastHealthy time.Time
	LogFile     *os.File
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
		openCodeServers:     make(map[string]*managedServer),
	}
}

func deriveRepoID(projectRoot string) string {
	repoID, err := xdg.RepoID(projectRoot)
	if err != nil || repoID == "" {
		repoID = filepath.Base(projectRoot)
	}
	return repoID
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
	s.StartOpenCodeServerHealthCheck()

	return nil
}

func (s *SocketServer) Stop() {
	close(s.stopCh)
	s.StopAllOpenCodeServers()
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
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	peek := make([]byte, 4)
	n, err := conn.Read(peek)
	if err != nil || n == 0 {
		conn.Close()
		return
	}

	s.handleProtoConnection(conn, peek[:n])
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

	port, err := s.ensureOpenCodeServerRunning(req.ProjectRoot)
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

func (s *SocketServer) ensureOpenCodeServerRunning(projectRoot string) (int, error) {
	s.openCodeServersMu.Lock()
	defer s.openCodeServersMu.Unlock()

	if srv, ok := s.openCodeServers[projectRoot]; ok {
		if s.isServerProcessAlive(srv) {
			client := agent.NewOpenCodeClient(srv.Port)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if client.IsServerRunning(ctx) {
				srv.LastHealthy = time.Now()
				s.logger.Printf("opencode server healthy on port %d for %s", srv.Port, projectRoot)
				return srv.Port, nil
			}
		}
		s.logger.Printf("existing server for %s not healthy, stopping and restarting", projectRoot)
		s.stopServerLocked(srv)
		delete(s.openCodeServers, projectRoot)
	}

	// Collect ports already in use by the registry to exclude from allocation.
	// This prevents race conditions when starting servers for different projects
	// in quick succession - a new server process may not have bound to its port
	// yet, so TCP check alone would see it as "available".
	usedPorts := make(map[int]bool, len(s.openCodeServers))
	for _, srv := range s.openCodeServers {
		usedPorts[srv.Port] = true
	}

	port := findAvailablePortExcluding(agent.OpenCodeServerPortStart, agent.OpenCodeServerPortEnd, usedPorts)
	if port == 0 {
		return 0, fmt.Errorf("no available port found for opencode server")
	}

	srv, err := s.startServerProcess(projectRoot, port)
	if err != nil {
		return 0, fmt.Errorf("failed to start opencode server: %w", err)
	}

	client := agent.NewOpenCodeClient(port)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.WaitForHealthy(ctx, 30*time.Second); err != nil {
		s.stopServerLocked(srv)
		return 0, fmt.Errorf("server started but failed health check: %w", err)
	}

	srv.LastHealthy = time.Now()
	s.openCodeServers[projectRoot] = srv
	s.logger.Printf("started opencode server on port %d (pid: %d) for %s", port, srv.Cmd.Process.Pid, projectRoot)
	return port, nil
}

func (s *SocketServer) startServerProcess(projectRoot string, port int) (*managedServer, error) {
	opencodeBin, err := exec.LookPath("opencode")
	if err != nil {
		home, _ := os.UserHomeDir()
		opencodeBin = filepath.Join(home, ".opencode", "bin", "opencode")
		if _, err := os.Stat(opencodeBin); err != nil {
			return nil, fmt.Errorf("opencode binary not found")
		}
	}

	logDir := xdg.StateDir()
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}
	repoID := deriveRepoID(projectRoot)
	logPath := filepath.Join(logDir, fmt.Sprintf("opencode-server-%s.log", repoID))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	cmd := exec.Command(opencodeBin, "serve", "--port", fmt.Sprintf("%d", port), "--hostname", "0.0.0.0")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		`OPENCODE_PERMISSION={"edit":"allow","bash":"allow","skill":"allow","webfetch":"allow","doom_loop":"allow","external_directory":"allow"}`,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	return &managedServer{
		ProjectRoot: projectRoot,
		Port:        port,
		Cmd:         cmd,
		StartTime:   time.Now(),
		LogFile:     logFile,
	}, nil
}

func (s *SocketServer) isServerProcessAlive(srv *managedServer) bool {
	if srv == nil || srv.Cmd == nil || srv.Cmd.Process == nil {
		return false
	}
	err := srv.Cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

func (s *SocketServer) stopServerLocked(srv *managedServer) {
	if srv == nil {
		return
	}
	if srv.Cmd != nil && srv.Cmd.Process != nil {
		srv.Cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- srv.Cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			srv.Cmd.Process.Kill()
		}
	}
	if srv.LogFile != nil {
		srv.LogFile.Close()
	}
}

func (s *SocketServer) StartOpenCodeServerHealthCheck() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.checkOpenCodeServerHealth()
			}
		}
	}()
}

func (s *SocketServer) checkOpenCodeServerHealth() {
	s.openCodeServersMu.Lock()
	defer s.openCodeServersMu.Unlock()

	for projectRoot, srv := range s.openCodeServers {
		if !s.isServerProcessAlive(srv) {
			s.logger.Printf("opencode server for %s died (pid: %d), will restart on next request", projectRoot, srv.Cmd.Process.Pid)
			s.stopServerLocked(srv)
			delete(s.openCodeServers, projectRoot)
			continue
		}

		client := agent.NewOpenCodeClient(srv.Port)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if client.IsServerRunning(ctx) {
			srv.LastHealthy = time.Now()
		} else {
			if time.Since(srv.LastHealthy) > 2*time.Minute {
				s.logger.Printf("opencode server for %s unhealthy for 2+ minutes, restarting", projectRoot)
				s.stopServerLocked(srv)
				delete(s.openCodeServers, projectRoot)
			}
		}
		cancel()
	}
}

func (s *SocketServer) StopAllOpenCodeServers() {
	s.openCodeServersMu.Lock()
	defer s.openCodeServersMu.Unlock()

	for projectRoot, srv := range s.openCodeServers {
		s.logger.Printf("stopping opencode server for %s (pid: %d)", projectRoot, srv.Cmd.Process.Pid)
		s.stopServerLocked(srv)
	}
	s.openCodeServers = make(map[string]*managedServer)
}

func (s *SocketServer) getOpenCodeServerPort(projectRoot string) int {
	s.openCodeServersMu.RLock()
	defer s.openCodeServersMu.RUnlock()

	if srv, ok := s.openCodeServers[projectRoot]; ok {
		return srv.Port
	}
	return 0
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

const controlPromptFileName = "ORCH_CONTROL_PROMPT.md"

// writeControlPromptFile writes the control agent prompt to a file in the specified project directory.
// This is the daemon's implementation to avoid circular imports with the monitor package.
func (s *SocketServer) writeControlPromptFile(st store.Store, projectRoot string) (string, error) {
	// Use project root if provided, otherwise fall back to cwd
	targetDir := projectRoot
	if targetDir == "" {
		var err error
		targetDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	prompt, err := s.buildControlAgentPrompt(st, projectRoot)
	if err != nil {
		return "", err
	}

	promptPath := filepath.Join(targetDir, controlPromptFileName)
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return "", fmt.Errorf("failed to write control prompt file: %w", err)
	}

	return promptPath, nil
}

// buildControlAgentPrompt builds the control agent prompt with dynamic repo context.
func (s *SocketServer) buildControlAgentPrompt(st store.Store, projectRoot string) (string, error) {
	workDir := projectRoot
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	issuesRoot := st.RootPath()

	// Get existing issues
	issues, _ := st.ListIssues()

	// Get active runs
	runs, _ := st.ListRuns(&store.ListRunsFilter{
		Status: []model.Status{
			model.StatusRunning,
			model.StatusBlocked,
			model.StatusBlockedAPI,
			model.StatusBooting,
			model.StatusQueued,
			model.StatusPROpen,
		},
		Limit: 20,
	})

	// Build issues table
	var issuesText string
	if len(issues) > 0 {
		var sb strings.Builder
		sb.WriteString("| ID | Status | Title |\n")
		sb.WriteString("|----|--------|-------|\n")
		for i, issue := range issues {
			if i >= 20 {
				break
			}
			status := string(issue.Status)
			if status == "" {
				status = string(model.IssueStatusOpen)
			}
			title := issue.Title
			if title == "" {
				title = "-"
			}
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", issue.ID, status, title))
		}
		issuesText = sb.String()
	} else {
		issuesText = "No issues found."
	}

	// Build runs table
	var runsText string
	if len(runs) > 0 {
		var sb strings.Builder
		sb.WriteString("| Issue | Run ID | Status |\n")
		sb.WriteString("|-------|--------|--------|\n")
		for i, run := range runs {
			if i >= 10 {
				break
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", run.IssueID, run.ShortID(), string(run.Status)))
		}
		runsText = sb.String()
	} else {
		runsText = "No active runs."
	}

	gitBranch := s.getGitBranch(workDir)
	uncommittedChanges := s.getUncommittedChangesStatus(workDir)

	// Get agent config
	cfg, _ := config.Load()
	defaultAgent := "opencode"
	if cfg != nil {
		if cfg.ControlAgent != "" {
			defaultAgent = cfg.ControlAgent
		} else if cfg.Agent != "" {
			defaultAgent = cfg.Agent
		}
	}

	prompt := fmt.Sprintf(`You are the orch control agent for this repository.
You can run orch commands directly via bash to manage issues and runs.

## Repository Context

- IssuesRoot: %s
- Working directory: %s

## Git Context
%s
- Uncommitted changes: %s

## Available Agents

- Default: %s
- Configured backends: opencode, claude, codex, gemini, custom

Use `+"`--agent <name>`"+` to specify a different agent when starting runs.

## Existing Issues

%s

## Active Runs

%s

## Workflows

### Starting New Work
1. Create issue: `+"`orch issue create <id> --title \"...\" --body \"...\"`"+`
2. Start run: `+"`orch run <issue-id>`"+`
3. Monitor: Watch the runs table or use `+"`orch ps`"+`

### Handling Blocked Runs
When a run shows "blocked" status:
1. Capture: `+"`orch capture <run-ref>`"+` to see what the agent needs
2. Send feedback: `+"`orch send <issue-id> \"<message>\"`"+` to provide input
3. The agent will resume automatically after receiving input

### Continuing Work
- From a branch: `+"`orch continue <issue> --branch <branch-name>`"+`
- From a run: `+"`orch continue <issue>#<run-id>`"+`

## Available Orch Commands

Run these commands directly using bash (do not use any special protocol):

### Issue Management
- Create issue: `+"`orch issue create <id> --title \"<title>\" --body \"<body>\"`"+`
- List issues: `+"`orch issue list`"+`
- Open issue in editor: `+"`orch open <issue-id>`"+`

### Run Management
- Start a run: `+"`orch run <issue-id>`"+`
- Continue from branch: `+"`orch continue <issue> --branch <branch>`"+`
- List runs: `+"`orch ps`"+` (use `+"`--status running,blocked`"+` to filter)
- Stop a run: `+"`orch stop <issue-id>#<run-id>`"+`
- Resolve a run: `+"`orch resolve <issue-id>#<run-id>`"+`
- Show run details: `+"`orch show <issue-id>#<run-id>`"+`
- Capture run state: `+"`orch capture <run-ref>`"+` - see agent's last message
- Send feedback: `+"`orch send <issue-id> \"<message>\"`"+` - provide input to blocked agent

### Interactive Commands (DO NOT USE)
The following commands are interactive and will hang if called by an AI agent:
- `+"`orch attach`"+` - interactive tmux session (for humans only)
- `+"`orch monitor`"+` - interactive TUI (for humans only)

## Troubleshooting

- Issue not showing in list: `+"`orch validate-issue-files <issue-id>`"+` to check for formatting errors
- Validate all issue files: `+"`orch validate-issue-files`"+` to find malformed issues
- Orphaned sessions: `+"`orch repair`"+`
- View daemon logs: Check `+"`.orch/daemon.log`"+` in project root
- Force stop all: `+"`orch stop --all`"+`

## Instructions

- Execute orch commands directly via bash - no special protocol needed
- Follow the issue ID naming convention when creating new issues
`, issuesRoot, workDir, gitBranch, uncommittedChanges, defaultAgent, issuesText, runsText)

	return prompt, nil
}

// getGitBranch returns the current git branch name.
func (s *SocketServer) getGitBranch(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(output))
	if branch != "" {
		return "- Current branch: " + branch
	}
	return ""
}

// getUncommittedChangesStatus returns "Yes" or "No" based on git status.
func (s *SocketServer) getUncommittedChangesStatus(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return "Unknown"
	}
	if len(strings.TrimSpace(string(output))) > 0 {
		return "Yes"
	}
	return "No"
}

// handleGetControlAgentLaunch handles the get_control_agent_launch API request.
// It writes the control prompt file, resolves agent configuration, and returns
// a ready-to-execute command for launching the control agent.
func (s *SocketServer) handleGetControlAgentLaunch(req SendRequest, encoder *json.Encoder) {
	if req.ProjectRoot == "" {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": "project_root required",
		})
		return
	}

	if req.IssuesRoot == "" {
		if cfg, err := config.LoadFromProjectRoot(req.ProjectRoot); err == nil && cfg != nil {
			req.IssuesRoot = cfg.GetIssuesPath()
			s.logger.Printf("derived issues_root=%q from project config", req.IssuesRoot)
		}
	}

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": "no store available for project",
		})
		return
	}

	promptPath, err := s.writeControlPromptFile(st, req.ProjectRoot)
	if err != nil {
		s.logger.Printf("failed to write control prompt file: %v", err)
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": fmt.Sprintf("failed to write control prompt file: %v", err),
		})
		return
	}

	// Load configuration
	cfg, cfgErr := config.Load()

	// Resolve agent type
	agentName := req.AgentType
	if agentName == "" && cfgErr == nil {
		agentName = cfg.ControlAgent
		if agentName == "" {
			agentName = cfg.Agent
		}
	}
	if agentName == "" {
		agentName = "opencode"
	}

	// Get model configuration
	var modelName, modelVariant string
	if cfgErr == nil {
		modelName = cfg.ControlModel
		if modelName == "" {
			modelName = cfg.Model
		}
		modelVariant = cfg.ControlModelVariant
		if modelVariant == "" {
			modelVariant = cfg.ModelVariant
		}
	}

	// Parse and validate agent type
	aType, err := agent.ParseAgentType(agentName)
	if err != nil {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": fmt.Sprintf("invalid agent type: %v", err),
		})
		return
	}

	adapter, err := agent.GetAdapter(aType)
	if err != nil {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": fmt.Sprintf("failed to get adapter: %v", err),
		})
		return
	}

	if !adapter.IsAvailable() {
		encoder.Encode(map[string]interface{}{
			"ok":    false,
			"error": fmt.Sprintf("%s CLI not available", agentName),
		})
		return
	}

	prompt := getControlPromptInstruction()
	var command string
	var port int
	var sessionID string
	var newSession bool

	// Handle session clearing for new_session request
	if req.Force { // Using Force as the "new_session" flag
		lock := s.getControlSessionLock(req.ProjectRoot)
		lock.Lock()
		sessionPath := s.controlSessionPath(req.ProjectRoot)
		os.Remove(sessionPath)
		lock.Unlock()
		newSession = true
	}

	// Handle based on injection method
	if adapter.PromptInjection() == agent.InjectionHTTP {
		// OpenCode uses HTTP injection via a managed server
		serverPort, err := s.ensureOpenCodeServerRunning(req.ProjectRoot)
		if err != nil {
			encoder.Encode(map[string]interface{}{
				"ok":    false,
				"error": fmt.Sprintf("failed to ensure opencode server: %v", err),
			})
			return
		}
		port = serverPort

		// Get or create session
		session, err := s.getOrCreateOpenCodeControlSession(req.ProjectRoot, port)
		if err != nil {
			s.logger.Printf("warning: server running but failed to get session: %v", err)
			// Still return the command, but without session
			command = fmt.Sprintf("opencode attach http://127.0.0.1:%d --dir %s", port, req.ProjectRoot)
		} else {
			sessionID = session
			// Include --dir to match the directory the session was created with
			command = fmt.Sprintf("opencode attach http://127.0.0.1:%d --session %s --dir %s", port, sessionID, req.ProjectRoot)
		}
	} else {
		// CLI-based agents (claude, codex, gemini, custom)
		// Get extra args for control agent
		var extraArgs []string
		if cfgErr == nil {
			extraArgs = cfg.GetControlExtraArgs(agentName)
		}

		// Build launch config
		launchCfg := &agent.LaunchConfig{
			Type:            aType,
			IssuesRoot:      st.RootPath(),
			Prompt:          prompt,
			ContinueSession: !newSession,
			Model:           modelName,
			ModelVariant:    modelVariant,
			ExtraArgs:       extraArgs,
		}

		// For claude, try to get existing session
		if agentName == "claude" && !newSession {
			stored := s.getStoredControlSession(req.ProjectRoot)
			if stored != "" {
				launchCfg.Resume = true
				launchCfg.SessionName = stored
				sessionID = stored
			} else {
				// Try to discover Claude session
				discovered := s.discoverClaudeSession(req.ProjectRoot)
				if discovered != "" {
					launchCfg.Resume = true
					launchCfg.SessionName = discovered
					sessionID = discovered
					// Save discovered session
					s.saveControlSession(req.ProjectRoot, discovered, "claude")
				}
			}
		}

		cmd, err := adapter.LaunchCommand(launchCfg)
		if err != nil {
			encoder.Encode(map[string]interface{}{
				"ok":    false,
				"error": fmt.Sprintf("failed to build launch command: %v", err),
			})
			return
		}
		command = cmd
	}

	s.logger.Printf("get_control_agent_launch: agent=%s, command=%s, port=%d, session=%s",
		agentName, command, port, sessionID)

	encoder.Encode(map[string]interface{}{
		"ok":          true,
		"command":     command,
		"prompt_file": promptPath,
		"port":        port,
		"session_id":  sessionID,
		"agent":       agentName,
	})
}

// getStoredControlSession returns the stored session ID for a project, or empty string if none.
func (s *SocketServer) getStoredControlSession(projectRoot string) string {
	sessionPath := s.controlSessionPath(projectRoot)
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return ""
	}
	var stored struct {
		SessionID string `json:"session_id"`
		AgentType string `json:"agent_type"`
	}
	if json.Unmarshal(data, &stored) != nil {
		return ""
	}
	return stored.SessionID
}

func findAvailablePort(start, end int) int {
	return findAvailablePortExcluding(start, end, nil)
}

func findAvailablePortExcluding(start, end int, exclude map[int]bool) int {
	for port := start; port <= end; port++ {
		if exclude != nil && exclude[port] {
			continue
		}
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

	if run.Agent == string(agent.AgentOpenCode) {
		return s.processSendOpenCode(st, ref, run, req.Message)
	}
	return s.processSendTmux(run, req.Message, req.NoEnter)
}

func (s *SocketServer) processSendOpenCode(st store.Store, ref *model.RunRef, run *model.Run, message string) error {
	if run.ServerPort <= 0 {
		return fmt.Errorf("run %s missing server port (not running or server not started)", ref.String())
	}
	if run.OpenCodeSessionID == "" {
		return fmt.Errorf("run %s missing session ID (agent may still be booting)", ref.String())
	}

	client := agent.NewOpenCodeClient(run.ServerPort)

	healthCtx, healthCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer healthCancel()

	if !client.IsServerRunning(healthCtx) {
		s.logger.Printf("opencode server not running for %s (port %d), updating status", ref.String(), run.ServerPort)

		if run.Status == model.StatusBlocked || run.Status == model.StatusRunning || run.Status == model.StatusBlockedAPI {
			st.AppendEvent(ref, model.NewStatusEvent(model.StatusUnknown))
			s.logger.Printf("updated run %s status to unknown (server stopped)", ref.String())
		}

		return &agent.ServerStoppedError{
			RunRef:       ref.String(),
			Port:         run.ServerPort,
			WorktreePath: run.WorktreePath,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := client.SendMessageAsync(ctx, run.OpenCodeSessionID, message, run.WorktreePath, nil, "")
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	s.logger.Printf("message sent successfully to %s", ref.String())
	return nil
}

func (s *SocketServer) processSendTmux(run *model.Run, message string, noEnter bool) error {
	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	mux := multiplexer.GetDefault()
	if mux == nil {
		return fmt.Errorf("no multiplexer available")
	}

	if !mux.HasSession(sessionName) {
		return &agent.SessionNotFoundError{SessionName: sessionName}
	}

	var err error
	if noEnter {
		err = mux.SendKeysLiteral(sessionName, message)
	} else {
		err = mux.SendKeys(sessionName, message)
	}
	if err != nil {
		return fmt.Errorf("failed to send keys: %w", err)
	}

	s.logger.Printf("message sent successfully to tmux session %s", sessionName)
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
			IssueID:    req.IssueID,
			Agent:      req.Agent,
			TextSearch: req.TextSearch,
			TimeRange:  req.TimeRange,
			Limit:      0,
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

	computeAlive := func(run *model.Run) bool {
		manager := agent.GetManager(run)
		return manager.IsAlive(run)
	}

	summaries := make([]*RunSummary, len(paginatedRuns))
	for i, run := range paginatedRuns {
		summaries[i] = RunToSummaryWithAlive(run, computeAlive)
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

func (s *SocketServer) listAllRepoRuns(req SendRequest) ([]*model.Run, error) {
	s.reposMu.RLock()
	stores := make([]store.Store, 0, len(s.repos))
	for _, ctx := range s.repos {
		if ctx.Store != nil {
			stores = append(stores, ctx.Store)
		}
	}
	s.reposMu.RUnlock()

	var allRuns []*model.Run
	for _, st := range stores {
		filter := &store.ListRunsFilter{
			IssueID:    req.IssueID,
			Agent:      req.Agent,
			TextSearch: req.TextSearch,
			TimeRange:  req.TimeRange,
		}
		for _, status := range req.Status {
			filter.Status = append(filter.Status, model.Status(status))
		}
		runs, err := st.ListRuns(filter)
		if err != nil {
			continue
		}
		allRuns = append(allRuns, runs...)
	}

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

func matchesTags(issueTags []string, filterTags map[string]bool, mode string) bool {
	if len(filterTags) == 0 {
		return true
	}

	issueTagSet := make(map[string]bool)
	for _, t := range issueTags {
		issueTagSet[strings.ToLower(t)] = true
	}

	if mode == "and" {
		for tag := range filterTags {
			if !issueTagSet[tag] {
				return false
			}
		}
		return true
	}

	for tag := range filterTags {
		if issueTagSet[tag] {
			return true
		}
	}
	return false
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

	computeAlive := func(r *model.Run) bool {
		manager := agent.GetManager(r)
		return manager.IsAlive(r)
	}

	encoder.Encode(GetRunResponse{
		OK:  true,
		Run: RunToFullWithAlive(run, computeAlive),
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
	client := NewProtoClientWithIssuesRoot(projectRoot, issuesRoot)
	client.SetTimeout(35 * time.Second)
	return client.SendMessage(run.IssueID, run.RunID, message)
}

// IsDaemonSocketAvailable checks if the global daemon socket exists.
func IsDaemonSocketAvailable(_ string) bool {
	socketPath := xdg.SocketPath()
	_, err := os.Stat(socketPath)
	return err == nil
}

func (s *SocketServer) handleStartRun(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" {
		encoder.Encode(StartRunResponse{OK: false, Error: "invalid_request: issue_id required"})
		return
	}

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(StartRunResponse{OK: false, Error: "no store available"})
		return
	}

	issue, err := st.ResolveIssue(req.IssueID)
	if err != nil {
		encoder.Encode(StartRunResponse{OK: false, Error: "issue not found: " + req.IssueID})
		return
	}

	cfg, err := config.Load()
	if err != nil {
		encoder.Encode(StartRunResponse{OK: false, Error: "failed to load config: " + err.Error()})
		return
	}

	agentName := req.AgentType
	if agentName == "" {
		agentName = cfg.Agent
	}
	if agentName == "" {
		agentName = "claude"
	}

	agentType, err := agent.ParseAgentType(agentName)
	if err != nil {
		encoder.Encode(StartRunResponse{OK: false, Error: "invalid agent: " + err.Error()})
		return
	}

	adapter, err := agent.GetAdapter(agentType)
	if err != nil {
		encoder.Encode(StartRunResponse{OK: false, Error: "failed to get adapter: " + err.Error()})
		return
	}

	if !adapter.IsAvailable() {
		encoder.Encode(StartRunResponse{OK: false, Error: "agent not available: " + agentName})
		return
	}

	runID := model.GenerateRunID()
	branch := model.GenerateBranchName(req.IssueID, runID)
	tmuxSession := model.GenerateTmuxSession(req.IssueID, runID)

	ctx := s.GetRepoContext(req.RepoID)
	if ctx == nil && req.ProjectRoot != "" {
		ctx = s.GetRepoContext(req.ProjectRoot)
	}

	var repoRoot string
	if ctx != nil {
		repoRoot = ctx.ProjectRoot
	} else if req.ProjectRoot != "" {
		repoRoot = req.ProjectRoot
	}

	if repoRoot == "" {
		encoder.Encode(StartRunResponse{OK: false, Error: "no project root available"})
		return
	}

	worktreeDir := cfg.WorktreeDir
	if worktreeDir == "" {
		home, _ := os.UserHomeDir()
		worktreeDir = filepath.Join(home, ".orch", "worktrees")
	}

	_ = model.GenerateWorktreeName(req.IssueID, runID, agentName)

	metadata := map[string]string{"agent": agentName}
	run, err := st.CreateRun(req.IssueID, runID, metadata)
	if err != nil {
		encoder.Encode(StartRunResponse{OK: false, Error: "failed to create run: " + err.Error()})
		return
	}

	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusQueued))

	baseBranch := cfg.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	worktreeResult, err := git.CreateWorktree(&git.WorktreeConfig{
		RepoRoot:    repoRoot,
		WorktreeDir: worktreeDir,
		IssueID:     req.IssueID,
		RunID:       runID,
		Agent:       agentName,
		BaseBranch:  baseBranch,
		Branch:      branch,
	})
	if err != nil {
		st.AppendEvent(run.Ref(), model.NewErrorArtifactEvent(err.Error()))
		st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
		encoder.Encode(StartRunResponse{OK: false, Error: "failed to create worktree: " + err.Error()})
		return
	}

	st.AppendEvent(run.Ref(), model.NewArtifactEvent("worktree", map[string]string{"path": worktreeResult.WorktreePath}))
	st.AppendEvent(run.Ref(), model.NewArtifactEvent("branch", map[string]string{"name": worktreeResult.Branch}))

	agentPrompt := s.buildRunPrompt(issue, st.RootPath())
	promptPath := filepath.Join(worktreeResult.WorktreePath, "ORCH_PROMPT.md")
	if err := os.WriteFile(promptPath, []byte(agentPrompt), 0644); err != nil {
		st.AppendEvent(run.Ref(), model.NewErrorArtifactEvent(err.Error()))
		st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
		encoder.Encode(StartRunResponse{OK: false, Error: "failed to write prompt file: " + err.Error()})
		return
	}

	initialPrompt := "ultrathink Please read 'ORCH_PROMPT.md' in the current directory and follow the instructions found there."

	launchCfg := &agent.LaunchConfig{
		Type:       agentType,
		WorkDir:    worktreeResult.WorktreePath,
		IssueID:    req.IssueID,
		RunID:      runID,
		RunPath:    run.Path,
		IssuesRoot: st.RootPath(),
		Branch:     worktreeResult.Branch,
		Prompt:     initialPrompt,
		Port:       agent.OpenCodeServerPortStart,
		Model:      req.Message,
		ExtraArgs:  cfg.GetExtraArgs(agentName),
	}

	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		st.AppendEvent(run.Ref(), model.NewErrorArtifactEvent(err.Error()))
		st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
		encoder.Encode(StartRunResponse{OK: false, Error: "failed to build agent command: " + err.Error()})
		return
	}

	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusBooting))

	muxType, _ := multiplexer.ParseType("")
	mux, _ := multiplexer.GetAuto()
	if mux == nil {
		mux, _, _ = multiplexer.GetWithFallback(muxType)
	}

	if mux == nil {
		st.AppendEvent(run.Ref(), model.NewErrorArtifactEvent("no multiplexer available"))
		st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
		encoder.Encode(StartRunResponse{OK: false, Error: "no terminal multiplexer available"})
		return
	}

	serverAlreadyRunning := false
	if adapter.PromptInjection() == agent.InjectionHTTP {
		resp, err := s.ensureOpenCodeServerRunning(worktreeResult.WorktreePath)
		if err != nil {
			st.AppendEvent(run.Ref(), model.NewErrorArtifactEvent(err.Error()))
			st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
			encoder.Encode(StartRunResponse{OK: false, Error: "failed to start opencode server: " + err.Error()})
			return
		}
		serverAlreadyRunning = true
		launchCfg.Port = resp
	}

	if !serverAlreadyRunning {
		env := launchCfg.Env()
		if opencodeAdapter, ok := adapter.(*agent.OpenCodeAdapter); ok {
			env = append(env, opencodeAdapter.Env()...)
		}

		err = mux.NewSession(&multiplexer.SessionConfig{
			SessionName: tmuxSession,
			WorkDir:     worktreeResult.WorktreePath,
			Command:     agentCmd,
			Env:         env,
		})
		if err != nil {
			st.AppendEvent(run.Ref(), model.NewErrorArtifactEvent(err.Error()))
			st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
			encoder.Encode(StartRunResponse{OK: false, Error: "failed to create session: " + err.Error()})
			return
		}

		st.AppendEvent(run.Ref(), model.NewArtifactEvent("session", map[string]string{
			"name":        tmuxSession,
			"multiplexer": string(mux.Type()),
		}))
	}

	switch adapter.PromptInjection() {
	case agent.InjectionTmux:
		if launchCfg.Prompt != "" {
			if pattern := adapter.ReadyPattern(); pattern != "" {
				if err := mux.WaitForReady(tmuxSession, pattern, 30*time.Second); err != nil {
					s.logger.Printf("agent did not become ready: %v", err)
				}
			}
			if err := mux.SendKeys(tmuxSession, launchCfg.Prompt); err != nil {
				s.logger.Printf("failed to send prompt to session: %v", err)
			}
		}

	case agent.InjectionHTTP:
		port := launchCfg.Port
		if port == 0 {
			port = agent.OpenCodeServerPortStart
		}

		client := agent.NewOpenCodeClient(port)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if err := client.WaitForHealthy(ctx, 60*time.Second); err != nil {
			s.logger.Printf("server health check failed: %v", err)
		} else {
			st.AppendEvent(run.Ref(), model.NewArtifactEvent("server", map[string]string{
				"port": fmt.Sprintf("%d", port),
			}))

			session, err := client.CreateSession(ctx, fmt.Sprintf("%s#%s", req.IssueID, runID), launchCfg.WorkDir)
			if err != nil {
				s.logger.Printf("failed to create session: %v", err)
			} else {
				st.AppendEvent(run.Ref(), model.NewArtifactEvent("opencode_session", map[string]string{
					"id": session.ID,
				}))

				var modelRef *agent.ModelRef
				if launchCfg.Model != "" {
					modelRef = agent.ParseModel(launchCfg.Model)
				}
				if err := client.SendMessageAsync(ctx, session.ID, launchCfg.Prompt, launchCfg.WorkDir, modelRef, launchCfg.ModelVariant); err != nil {
					s.logger.Printf("failed to send prompt: %v", err)
				}
			}
		}
	}

	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusRunning))

	s.logger.Printf("started run: %s#%s (agent=%s, worktree=%s)", req.IssueID, runID, agentName, worktreeResult.WorktreePath)

	encoder.Encode(StartRunResponse{
		OK:           true,
		RunID:        runID,
		Branch:       worktreeResult.Branch,
		WorktreePath: worktreeResult.WorktreePath,
		TmuxSession:  tmuxSession,
		Status:       string(model.StatusRunning),
	})
}

func (s *SocketServer) buildRunPrompt(issue *model.Issue, issuesRoot string) string {
	prTargetBranch := "main"
	return fmt.Sprintf(`## Context

This file (ORCH_PROMPT.md) is auto-generated by orch. The original issue is at:
- IssuesRoot: %s
- Issue file: %s

## Issue

<issue>
%s
</issue>

## Instructions

- Read the issue carefully, especially the **Acceptance Criteria** section
- Implement the changes described in the issue
- **CRITICAL: Verify EACH acceptance criterion by actually running the code**
  - If the issue requires outputs (CSV, reports, etc.), run the entrypoint and confirm outputs exist
  - Don't just check that code compiles - verify it produces correct results
- Run tests to verify your changes work correctly
- When complete, create a pull request targeting `+"`%s`"+` with:
  - Evidence that each acceptance criterion is met (command outputs, file listings, etc.)
  - Summary of changes made
  - Reference to the issue: %s
`, issuesRoot, issue.Path, issue.Body, prTargetBranch, issue.ID)
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

	if run.Agent == string(agent.AgentOpenCode) && run.ServerPort > 0 && run.OpenCodeSessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client := agent.NewOpenCodeClient(run.ServerPort)
		if err := client.Abort(ctx, run.OpenCodeSessionID); err != nil {
			s.logger.Printf("warning: failed to cancel opencode session %s: %v", run.OpenCodeSessionID, err)
		} else {
			s.logger.Printf("canceled opencode session %s for %s#%s", run.OpenCodeSessionID, run.IssueID, run.RunID)
		}
	} else {
		if run.Agent == string(agent.AgentOpenCode) {
			s.logger.Printf("debug: skipping opencode API cancel (port=%d, session=%q), falling back to multiplexer",
				run.ServerPort, run.OpenCodeSessionID)
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
	sb.WriteString("id: " + model.QuoteYAMLValue(req.IssueID) + "\n")
	sb.WriteString("title: " + model.QuoteYAMLValue(title) + "\n")
	if req.Summary != "" {
		sb.WriteString("summary: " + model.QuoteYAMLValue(req.Summary) + "\n")
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
		serverPort = s.getOpenCodeServerPort(run.WorktreePath)
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

func (s *SocketServer) handleAppendEvent(req SendRequest, encoder *json.Encoder) {
	if req.IssueID == "" || req.RunID == "" {
		encoder.Encode(AppendEventResponse{OK: false, Error: "invalid_request: issue_id and run_id required"})
		return
	}
	if req.EventType == "" || req.EventName == "" {
		encoder.Encode(AppendEventResponse{OK: false, Error: "invalid_request: event_type and event_name required"})
		return
	}
	if req.EventSource == "" {
		encoder.Encode(AppendEventResponse{OK: false, Error: "invalid_request: event_source required"})
		return
	}

	st := s.resolveStore(req)
	if st == nil {
		encoder.Encode(AppendEventResponse{OK: false, Error: "no store available"})
		return
	}

	ref := &model.RunRef{IssueID: req.IssueID, RunID: req.RunID}
	run, err := st.GetRun(ref)
	if err != nil {
		encoder.Encode(AppendEventResponse{OK: false, Error: "run not found"})
		return
	}

	source := model.EventSource(req.EventSource)
	if req.EventType == "status" {
		newStatus := model.Status(req.EventName)
		if !model.CanTransitionStatus(run.Status, newStatus, source) {
			reason := fmt.Sprintf("cannot transition from %s to %s (source=%s)", run.Status, newStatus, source)
			s.logger.Printf("%s#%s: %s", req.IssueID, req.RunID, reason)
			encoder.Encode(AppendEventResponse{OK: true, Skipped: true, Reason: reason})
			return
		}
	}

	event := model.NewEvent(model.EventType(req.EventType), req.EventName, req.EventAttrs)
	if err := st.AppendEvent(ref, event); err != nil {
		encoder.Encode(AppendEventResponse{OK: false, Error: "failed to append event: " + err.Error()})
		return
	}

	encoder.Encode(AppendEventResponse{OK: true})
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
