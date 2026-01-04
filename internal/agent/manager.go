package agent

import (
	"context"
	"strings"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
)

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
		return &OpenCodeManager{
			Port:      run.ServerPort,
			SessionID: run.OpenCodeSessionID,
			Directory: run.WorktreePath,
			RunRef:    run.Ref().String(),
		}
	}
	return &TmuxManager{SessionName: getSessionName(run)}
}

func getSessionName(run *model.Run) string {
	if run.TmuxSession != "" {
		return run.TmuxSession
	}
	return model.GenerateTmuxSession(run.IssueID, run.RunID)
}

type TmuxManager struct {
	SessionName string
}

func (m *TmuxManager) IsAlive(run *model.Run) bool {
	return tmux.HasSession(m.SessionName)
}

func (m *TmuxManager) CaptureOutput(run *model.Run) (string, error) {
	return tmux.CapturePane(m.SessionName, 100)
}

func (m *TmuxManager) DetectPrompt(output string) bool {
	return IsWaitingForInput(output)
}

func (m *TmuxManager) GetStatus(run *model.Run, output string, state *RunState, outputChanged, hasPrompt bool) model.Status {
	if IsAgentExited(output) {
		return model.StatusUnknown
	}
	if IsCompleted(output) {
		return model.StatusDone
	}
	if IsAPILimited(output) {
		return model.StatusBlockedAPI
	}
	if IsFailed(output) {
		return model.StatusFailed
	}
	if outputChanged {
		return model.StatusRunning
	}
	if hasPrompt {
		return model.StatusBlocked
	}
	return ""
}

func (m *TmuxManager) SendMessage(ctx context.Context, run *model.Run, message string, opts *SendOptions) error {
	if !tmux.HasSession(m.SessionName) {
		return &SessionNotFoundError{SessionName: m.SessionName}
	}

	if opts != nil && opts.NoEnter {
		return tmux.SendKeysLiteral(m.SessionName, message)
	}
	return tmux.SendKeys(m.SessionName, message)
}

type OpenCodeManager struct {
	Port      int
	SessionID string
	Directory string
	RunRef    string
}

func (m *OpenCodeManager) IsAlive(run *model.Run) bool {
	client := NewOpenCodeClient(m.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !client.IsServerRunning(ctx) {
		return false
	}

	sessions, err := client.GetSessionIDs(ctx)
	if err != nil {
		return false
	}

	return sessions[m.SessionID]
}

func (m *OpenCodeManager) CaptureOutput(run *model.Run) (string, error) {
	client := NewOpenCodeClient(m.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
		role := strings.ToUpper(msg.Info.Role)
		if role == "" {
			role = "UNKNOWN"
		}

		allLines = append(allLines, "--- ["+role+"] ---")

		for _, part := range msg.Parts {
			if part.Type != "text" || part.Text == "" {
				continue
			}
			partLines := strings.Split(part.Text, "\n")
			allLines = append(allLines, partLines...)
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

func (m *OpenCodeManager) GetStatus(run *model.Run, output string, state *RunState, outputChanged, hasPrompt bool) model.Status {
	if run.Status == model.StatusBooting || run.Status == model.StatusQueued {
		return model.StatusRunning
	}

	client := NewOpenCodeClient(m.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionStatus, found, err := client.GetSingleSessionStatus(ctx, m.SessionID, "")
	if err != nil {
		return ""
	}

	if !found {
		return model.StatusBlocked
	}

	switch sessionStatus {
	case SessionStatusBusy:
		return model.StatusRunning
	case SessionStatusIdle:
		return model.StatusBlocked
	case SessionStatusRetry:
		return model.StatusBlockedAPI
	default:
		return ""
	}
}

func (m *OpenCodeManager) SendMessage(ctx context.Context, run *model.Run, message string, opts *SendOptions) error {
	if m.Port <= 0 {
		return &OpenCodeConfigError{RunRef: m.RunRef, Missing: "server port"}
	}
	if m.SessionID == "" {
		return &OpenCodeConfigError{RunRef: m.RunRef, Missing: "session ID"}
	}

	client := NewOpenCodeClient(m.Port)
	return client.SendMessagePrompt(ctx, m.SessionID, message, run.WorktreePath)
}

func IsWaitingForInput(output string) bool {
	promptPatterns := []string{
		"No, and tell Claude what to do differently",
		"tell Claude what to do differently",
		"↵ send",
		"? for shortcuts",
		"accept edits",
		"bypass permissions",
		"shift+tab to cycle",
		"Esc to cancel",
		"to show all projects",
		"Type your message",
		"ctrl+s send",
		"enter newline",
		"ctrl+c interrupt",
	}

	for _, pattern := range promptPatterns {
		if strings.Contains(output, pattern) {
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
		"ctrl+s send",
		"enter newline",
		"ctrl+c interrupt",
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
