package agent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/multiplexer"
)

type muxCache struct {
	mu            sync.RWMutex
	sessions      map[string]bool
	paneCommands  map[string][]string
	lastRefreshed time.Time
	ttl           time.Duration
}

var globalMuxCache = &muxCache{
	ttl: 5 * time.Second,
}

func RefreshMuxCache() {
	globalMuxCache.mu.Lock()
	defer globalMuxCache.mu.Unlock()

	mux := multiplexer.GetDefault()
	sessions, err := mux.ListSessions()
	if err == nil {
		globalMuxCache.sessions = make(map[string]bool)
		for _, s := range sessions {
			globalMuxCache.sessions[s] = true
		}
	}

	paneCommands, err := mux.ListPaneCommands()
	if err == nil {
		globalMuxCache.paneCommands = paneCommands
	}

	globalMuxCache.lastRefreshed = time.Now()
}

func GetMuxCache() (map[string]bool, map[string][]string) {
	globalMuxCache.mu.RLock()
	if time.Since(globalMuxCache.lastRefreshed) < globalMuxCache.ttl {
		sessions := globalMuxCache.sessions
		paneCommands := globalMuxCache.paneCommands
		globalMuxCache.mu.RUnlock()
		return sessions, paneCommands
	}
	globalMuxCache.mu.RUnlock()

	RefreshMuxCache()

	globalMuxCache.mu.RLock()
	defer globalMuxCache.mu.RUnlock()
	return globalMuxCache.sessions, globalMuxCache.paneCommands
}

func InvalidateMuxCache() {
	globalMuxCache.mu.Lock()
	defer globalMuxCache.mu.Unlock()
	globalMuxCache.lastRefreshed = time.Time{}
}

type RunState struct {
	LastOutput   string
	LastOutputAt time.Time
	LastCheckAt  time.Time
	OutputHash   string
	PRRecorded   bool
}

type SessionNotFoundError struct {
	SessionName string
}

func (e *SessionNotFoundError) Error() string {
	return "session " + e.SessionName + " not found (run may not be active)"
}

type OpenCodeConfigError struct {
	RunRef  string
	Missing string
}

func (e *OpenCodeConfigError) Error() string {
	return "opencode run " + e.RunRef + " missing " + e.Missing
}

// ServerStoppedError indicates the opencode server for a run has stopped.
// This error provides clear messaging when trying to interact with a run
// whose server is no longer running.
type ServerStoppedError struct {
	RunRef       string
	Port         int
	WorktreePath string
}

func (e *ServerStoppedError) Error() string {
	return "run " + e.RunRef + " has ended (opencode server stopped)"
}

type SendOptions struct {
	NoEnter bool
}

type AgentManager interface {
	IsAlive(run *model.Run) bool
	GetStatus(run *model.Run, output string, state *RunState, outputChanged, hasPrompt bool) model.Status
	CaptureOutput(run *model.Run) (string, error)
	DetectPrompt(output string) bool
	SendMessage(ctx context.Context, run *model.Run, message string, opts *SendOptions) error
}

func GetManager(run *model.Run) AgentManager {
	if run.Agent == string(AgentOpenCode) {
		port := run.ServerPort
		if port == 0 {
			port = FindRunningOpenCodeServerForWorktree(run.WorktreePath, OpenCodeServerPortStart, OpenCodeServerPortEnd)
		}
		return &OpenCodeManager{
			Port:      port,
			SessionID: run.OpenCodeSessionID,
			Directory: run.WorktreePath,
			RunRef:    run.Ref().String(),
		}
	}
	return &MuxManager{SessionName: getSessionName(run)}
}

func getSessionName(run *model.Run) string {
	if run.SessionName != "" {
		return run.SessionName
	}
	return model.GenerateSessionName(run.IssueID, run.RunID)
}

type MuxManager struct {
	SessionName string
}

func (m *MuxManager) IsAlive(run *model.Run) bool {
	sessions, paneCommands := GetMuxCache()
	return m.isAliveFromCache(sessions, paneCommands)
}

func (m *MuxManager) isAliveFromCache(sessions map[string]bool, paneCommands map[string][]string) bool {
	mux := multiplexer.GetDefault()
	if sessions != nil {
		if !sessions[m.SessionName] {
			return false
		}
	} else {
		if !mux.HasSession(m.SessionName) {
			return false
		}
	}

	if paneCommands != nil {
		alive, known := mux.AgentAlive(m.SessionName, paneCommands)
		if known {
			return alive
		}
	}

	return true
}

func (m *MuxManager) CaptureOutput(run *model.Run) (string, error) {
	mux := multiplexer.GetDefault()
	return mux.CapturePane(m.SessionName, 100)
}

func (m *MuxManager) DetectPrompt(output string) bool {
	return IsWaitingForInput(output)
}

func (m *MuxManager) GetStatus(run *model.Run, output string, state *RunState, outputChanged, hasPrompt bool) model.Status {
	if IsAgentExited(output) {
		return model.StatusUnknown
	}
	if IsCompleted(output) {
		return model.StatusDone
	}
	if IsAPILimited(output) {
		return model.StatusRateLimited
	}
	if IsFailed(output) {
		return model.StatusFailed
	}
	if hasPrompt {
		return model.StatusWaiting
	}
	if outputChanged {
		return model.StatusRunning
	}
	return ""
}

func (m *MuxManager) SendMessage(ctx context.Context, run *model.Run, message string, opts *SendOptions) error {
	mux := multiplexer.GetDefault()
	if !mux.HasSession(m.SessionName) {
		return &SessionNotFoundError{SessionName: m.SessionName}
	}

	noEnter := opts != nil && opts.NoEnter
	if err := multiplexer.SendMessage(mux, m.SessionName, message, noEnter, false, 0); err != nil {
		return err
	}
	return nil
}

type OpenCodeManager struct {
	Port      int
	SessionID string
	Directory string
	RunRef    string
}

func (m *OpenCodeManager) IsAlive(run *model.Run) bool {
	if m.Port <= 0 || strings.TrimSpace(m.SessionID) == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client := NewOpenCodeClient(m.Port)
	if !client.IsServerRunning(ctx) {
		return false
	}

	return m.sessionReachable(ctx, client)
}

func (m *OpenCodeManager) CaptureOutput(run *model.Run) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := m.readyClient(ctx)
	if err != nil {
		return "", err
	}

	messages, err := client.GetMessages(ctx, m.SessionID, m.Directory)
	if err != nil {
		return "", err
	}

	if len(messages) == 0 {
		return "", nil
	}

	return FormatOpenCodeMessages(messages, 100), nil
}

func FormatOpenCodeMessages(messages []Message, maxLines int) string {
	var allLines []string

	for _, msg := range messages {
		if len(msg.Parts) == 0 {
			continue
		}

		role := strings.ToUpper(msg.Info.Role)
		if role == "" {
			role = "UNKNOWN"
		}

		header := "--- [" + role + "] ---"
		var textLines []string
		toolUseCount := 0

		for _, part := range msg.Parts {
			if part.Type == "tool_use" {
				toolUseCount++
			}

			if part.Type != "text" || part.Text == "" {
				continue
			}
			partLines := strings.Split(part.Text, "\n")
			textLines = append(textLines, partLines...)
		}

		if len(textLines) > 0 {
			allLines = append(allLines, header)
			allLines = append(allLines, textLines...)
			continue
		}

		if toolUseCount > 0 {
			allLines = append(allLines, header+" ("+strconv.Itoa(toolUseCount)+" tool uses)")
		}
	}

	if len(allLines) <= maxLines {
		return strings.Join(allLines, "\n")
	}

	return strings.Join(allLines[len(allLines)-maxLines:], "\n")
}

func (m *OpenCodeManager) DetectPrompt(output string) bool {
	return false
}

const recentActivityThreshold = 30 * time.Second

func (m *OpenCodeManager) GetStatus(run *model.Run, output string, state *RunState, outputChanged, hasPrompt bool) model.Status {
	if run.Status == model.StatusBooting || run.Status == model.StatusQueued {
		return model.StatusRunning
	}
	if m.Port <= 0 || strings.TrimSpace(m.SessionID) == "" {
		return ""
	}

	client := NewOpenCodeClient(m.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionStatus, found, err := client.GetSingleSessionStatus(ctx, m.SessionID, m.Directory)
	if err == nil && found {
		switch sessionStatus {
		case SessionStatusBusy:
			return model.StatusRunning
		case SessionStatusIdle:
			return model.StatusWaiting
		case SessionStatusRetry:
			return model.StatusRateLimited
		default:
			return ""
		}
	}

	if m.hasActiveBusyChildren(ctx, client) {
		return model.StatusRunning
	}

	if m.hasRecentActivity(ctx, client) {
		return model.StatusRunning
	}

	if m.sessionExists(ctx, client) {
		return model.StatusWaiting
	}

	return model.StatusUnknown
}

func (m *OpenCodeManager) hasActiveBusyChildren(ctx context.Context, client *OpenCodeClient) bool {
	statusMap, err := client.GetSessionStatus(ctx, m.Directory)
	if err != nil {
		return false
	}

	for sessionID, status := range statusMap {
		if status != SessionStatusBusy {
			continue
		}
		session, err := client.GetSession(ctx, sessionID, m.Directory)
		if err != nil {
			continue
		}
		if session.ParentID == m.SessionID {
			return true
		}
	}

	return false
}

func (m *OpenCodeManager) hasRecentActivity(ctx context.Context, client *OpenCodeClient) bool {
	session, err := client.GetSession(ctx, m.SessionID, m.Directory)
	if err != nil {
		return false
	}

	timeSinceUpdate := time.Since(session.UpdatedAt())
	return timeSinceUpdate < recentActivityThreshold
}

func (m *OpenCodeManager) sessionExists(ctx context.Context, client *OpenCodeClient) bool {
	// Use GetSessionsForDirectory with the run's worktree path for consistent session detection.
	// OpenCode scopes sessions by project (directory). Without the directory header,
	// sessions created with worktree paths won't be found.
	// See: https://github.com/proboscis/orch/issues/347
	sessions, err := client.GetSessionsForDirectory(ctx, m.Directory)
	if err != nil {
		return false
	}
	for _, s := range sessions {
		if s.ID == m.SessionID {
			return true
		}
	}
	return false
}

func (m *OpenCodeManager) SendMessage(ctx context.Context, run *model.Run, message string, opts *SendOptions) error {
	client, err := m.readyClient(ctx)
	if err != nil {
		return err
	}

	return client.SendMessageAsync(ctx, m.SessionID, message, run.WorktreePath, nil, "")
}

func (m *OpenCodeManager) readyClient(ctx context.Context) (*OpenCodeClient, error) {
	if m.Port <= 0 {
		return nil, &OpenCodeConfigError{RunRef: m.RunRef, Missing: "server port"}
	}
	if strings.TrimSpace(m.SessionID) == "" {
		return nil, &OpenCodeConfigError{RunRef: m.RunRef, Missing: "session ID"}
	}

	client := NewOpenCodeClient(m.Port)
	if !client.IsServerRunning(ctx) {
		return nil, &ServerStoppedError{RunRef: m.RunRef, Port: m.Port, WorktreePath: m.Directory}
	}
	return client, nil
}

func (m *OpenCodeManager) sessionReachable(ctx context.Context, client *OpenCodeClient) bool {
	if client == nil || strings.TrimSpace(m.SessionID) == "" {
		return false
	}

	if _, found, err := client.GetSingleSessionStatus(ctx, m.SessionID, m.Directory); err == nil && found {
		return true
	}

	if _, err := client.GetSession(ctx, m.SessionID, m.Directory); err == nil {
		return true
	}

	return m.sessionExists(ctx, client)
}

func IsWaitingForInput(output string) bool {
	lines := strings.ToLower(getLastLines(output, 40))

	// Codex keeps shortcut hints visible even while actively working.
	// Treat explicit in-progress markers as stronger signals than prompt hints.
	busyPatterns := []string{
		"esc to interrupt",
		"working (",
		"background terminal running",
	}
	for _, pattern := range busyPatterns {
		if strings.Contains(lines, pattern) {
			return false
		}
	}

	promptPatterns := []string{
		"no, and tell claude what to do differently",
		"tell claude what to do differently",
		"↵ send",
		"? for shortcuts",
		"tab to queue message",
		"context left",
		"accept edits",
		"bypass permissions",
		"shift+tab to cycle",
		"esc to cancel",
		"to show all projects",
		"type your message",
		"ctrl+s send",
		"enter newline",
		"ctrl+c interrupt",
		"use /skills to list available skills",
	}

	for _, pattern := range promptPatterns {
		if strings.Contains(lines, pattern) {
			return true
		}
	}

	// Recent codex builds render an idle footer with no shortcut hints
	// (just "branch · cwd"), so none of the patterns above can match. The
	// composer line — "› ..." with U+203A — is the stable idle marker.
	// Echoed user messages share the prefix, but the busy markers above
	// already returned false while the agent is actively working.
	for _, line := range strings.Split(lines, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "›") {
			return true
		}
	}
	return false
}

func IsAgentExited(output string) bool {
	agentPatterns := []string{
		"↵ send",
		"accept edits",
		"? for shortcuts",
		"tell Claude what to do differently",
		"tokens",
		"Esc to cancel",
		"to show all projects",
		"Use /skills to list available skills",
		"tab to queue message",
		"ctrl+s send",
		"enter newline",
		"ctrl+c interrupt",
		"esc to interrupt",
		"opencode server listening",
		"POST /session",
		"POST /message",
	}

	for _, pattern := range agentPatterns {
		if strings.Contains(output, pattern) {
			return false
		}
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return false
	}

	lastLine := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			lastLine = line
			break
		}
	}

	if lastLine == "" {
		return false
	}

	if strings.Contains(lastLine, "git:(") && strings.Contains(lastLine, ")") {
		return true
	}

	trimmed := strings.TrimRight(lastLine, " ")
	if strings.HasSuffix(lastLine, "$ ") ||
		strings.HasSuffix(lastLine, "% ") ||
		strings.HasSuffix(lastLine, "# ") ||
		strings.HasSuffix(lastLine, "❯ ") ||
		strings.HasSuffix(lastLine, "➜ ") ||
		strings.HasSuffix(trimmed, "$") ||
		strings.HasSuffix(trimmed, "%") ||
		strings.HasSuffix(trimmed, "✗") ||
		strings.HasSuffix(trimmed, "❯") ||
		strings.HasSuffix(trimmed, "➜") {
		return true
	}

	return false
}

func IsCompleted(output string) bool {
	lines := getLastLines(output, 5)
	lowerOutput := strings.ToLower(lines)

	completionPatterns := []string{
		"task completed successfully",
		"all tasks completed",
		"session ended",
		"goodbye",
	}

	for _, pattern := range completionPatterns {
		if strings.Contains(lowerOutput, pattern) {
			return true
		}
	}
	return false
}

func IsAPILimited(output string) bool {
	lines := getLastLines(output, 30)
	lowerOutput := strings.ToLower(lines)

	apiLimitPatterns := []string{
		"cost limit reached",
		"rate limit exceeded",
		"rate limit reached",
		"quota exceeded",
		"insufficient quota",
		"resource exhausted",
		"you've hit your limit",
		"/rate-limit-options",
		"stop and wait for limit to reset",
	}

	for _, pattern := range apiLimitPatterns {
		if strings.Contains(lowerOutput, pattern) {
			return true
		}
	}
	return false
}

func IsFailed(output string) bool {
	lines := getLastLines(output, 10)
	lowerOutput := strings.ToLower(lines)

	errorPatterns := []string{
		"fatal error",
		"unrecoverable error",
		"agent crashed",
		"session terminated",
		"authentication failed",
	}

	for _, pattern := range errorPatterns {
		if strings.Contains(lowerOutput, pattern) {
			return true
		}
	}
	return false
}

func getLastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
