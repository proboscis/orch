package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/orchapi"
)

const remoteCodexTmuxSubmitDelay = 250 * time.Millisecond
const remoteClaudeTmuxMultilineSubmitDelay = 100 * time.Millisecond

var currentControlHostname = os.Hostname

func getRunAttachInfo(ctx context.Context, api orchapi.OrchAPI, ref orchapi.RunRef) (*orchapi.AttachInfo, error) {
	info, err := api.GetAttachInfo(ctx, ref)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("no attach info for %s", ref.String())
	}
	if !info.SessionExists && !shouldHandleRunLocally(info) {
		return nil, fmt.Errorf("session not found for %s#%s on host %s", info.IssueID, info.RunID, defaultTargetHost(info.TargetHost))
	}
	return info, nil
}

func isLocalControlHost(targetHost string) bool {
	targetHost = strings.TrimSpace(targetHost)
	if targetHost == "" {
		return false
	}
	if targetHost == "localhost" || targetHost == "127.0.0.1" || targetHost == "::1" {
		return true
	}
	if strings.Contains(targetHost, "@") {
		return false
	}
	host, _ := currentControlHostname()
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	short := strings.Split(host, ".")[0]
	targetShort := strings.Split(targetHost, ".")[0]
	return strings.EqualFold(targetHost, host) || strings.EqualFold(targetShort, short)
}

func shouldHandleRunLocally(info *orchapi.AttachInfo) bool {
	if info == nil {
		return false
	}
	if isLocalControlHost(info.TargetHost) {
		return true
	}
	if strings.TrimSpace(info.WorktreePath) == "" {
		return false
	}
	if st, err := os.Stat(info.WorktreePath); err == nil && st.IsDir() {
		return true
	}
	return false
}

func defaultTargetHost(targetHost string) string {
	targetHost = strings.TrimSpace(targetHost)
	if targetHost == "" {
		return "local"
	}
	return targetHost
}

func sessionNameFromAttachInfo(info *orchapi.AttachInfo) string {
	if info == nil {
		return ""
	}
	if strings.TrimSpace(info.SessionName) != "" {
		return strings.TrimSpace(info.SessionName)
	}
	return model.GenerateSessionName(info.IssueID, info.RunID)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func sshScriptArgs(targetHost string, tty bool, script string) []string {
	args := []string{}
	if tty {
		args = append(args, "-t")
	} else {
		args = append(args, "-T")
	}
	args = append(args, targetHost, "sh", "-lc", script)
	return args
}

func captureLocalFromInfo(info *orchapi.AttachInfo, lines int) (*orchapi.CaptureResult, error) {
	if info == nil {
		return nil, fmt.Errorf("attach info required")
	}
	if lines <= 0 {
		lines = 100
	}
	if strings.EqualFold(info.Agent, string(agent.AgentOpenCode)) {
		if info.ServerPort <= 0 {
			return nil, fmt.Errorf("run %s#%s has no opencode server port", info.IssueID, info.RunID)
		}
		if strings.TrimSpace(info.OpenCodeSessionID) == "" {
			return nil, fmt.Errorf("run %s#%s has no opencode session ID", info.IssueID, info.RunID)
		}
		client := agent.NewOpenCodeClient(info.ServerPort)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !client.IsServerRunning(ctx) {
			return nil, fmt.Errorf("opencode server not running for %s#%s", info.IssueID, info.RunID)
		}
		messages, err := client.GetMessages(ctx, info.OpenCodeSessionID, info.WorktreePath)
		if err != nil {
			return nil, fmt.Errorf("failed to get opencode messages for %s#%s: %w", info.IssueID, info.RunID, err)
		}
		return &orchapi.CaptureResult{
			Content:   agent.FormatOpenCodeMessages(messages, lines),
			Timestamp: time.Now(),
			Source:    "opencode",
		}, nil
	}

	sessionName := sessionNameFromAttachInfo(info)
	if sessionName == "" {
		return nil, fmt.Errorf("run %s#%s has no session name", info.IssueID, info.RunID)
	}
	mux, err := multiplexerForAttachInfo(info)
	if err != nil {
		return nil, err
	}
	if !mux.HasSession(sessionName) {
		return nil, fmt.Errorf("session %q not found for run %s#%s", sessionName, info.IssueID, info.RunID)
	}
	content, err := mux.CapturePane(sessionName, lines)
	if err != nil {
		return nil, fmt.Errorf("failed to capture session %q: %w", sessionName, err)
	}
	source := strings.TrimSpace(string(info.Multiplexer))
	if source == "" {
		source = "tmux"
	}
	return &orchapi.CaptureResult{
		Content:   content,
		Timestamp: time.Now(),
		Source:    source,
	}, nil
}

func sendLocalFromInfo(info *orchapi.AttachInfo, message string, noEnter bool) error {
	if info == nil {
		return fmt.Errorf("attach info required")
	}
	if strings.EqualFold(info.Agent, string(agent.AgentOpenCode)) {
		if info.ServerPort <= 0 {
			return fmt.Errorf("run %s#%s has no opencode server port", info.IssueID, info.RunID)
		}
		if strings.TrimSpace(info.OpenCodeSessionID) == "" {
			return fmt.Errorf("run %s#%s has no opencode session ID", info.IssueID, info.RunID)
		}
		client := agent.NewOpenCodeClient(info.ServerPort)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if !client.IsServerRunning(ctx) {
			return fmt.Errorf("opencode server not running for %s#%s", info.IssueID, info.RunID)
		}
		if err := client.SendMessagePrompt(ctx, info.OpenCodeSessionID, message, info.WorktreePath, nil, ""); err != nil {
			return fmt.Errorf("failed to send opencode message for %s#%s: %w", info.IssueID, info.RunID, err)
		}
		return nil
	}

	sessionName := sessionNameFromAttachInfo(info)
	if sessionName == "" {
		return fmt.Errorf("run %s#%s has no session name", info.IssueID, info.RunID)
	}
	mux, err := multiplexerForAttachInfo(info)
	if err != nil {
		return err
	}
	if !mux.HasSession(sessionName) {
		return fmt.Errorf("session %q not found for run %s#%s", sessionName, info.IssueID, info.RunID)
	}

	submitDelay := time.Duration(0)
	splitSubmit := strings.EqualFold(info.Agent, string(agent.AgentCodex)) && mux.Type() == multiplexer.TypeTmux && !noEnter
	if splitSubmit {
		submitDelay = remoteCodexTmuxSubmitDelay
	}
	if err := multiplexer.SendMessage(mux, sessionName, message, noEnter, splitSubmit, submitDelay); err != nil {
		return err
	}
	if shouldSendClaudeMultilineConfirm(info.Agent, mux.Type(), message, noEnter) {
		time.Sleep(remoteClaudeTmuxMultilineSubmitDelay)
		return mux.SendText(sessionName, "Enter")
	}
	return nil
}

func multiplexerForAttachInfo(info *orchapi.AttachInfo) (multiplexer.Multiplexer, error) {
	muxType := multiplexer.TypeTmux
	if strings.TrimSpace(string(info.Multiplexer)) != "" {
		parsed, err := multiplexer.ParseType(string(info.Multiplexer))
		if err != nil {
			return nil, fmt.Errorf("invalid multiplexer %q for run %s#%s", info.Multiplexer, info.IssueID, info.RunID)
		}
		if parsed != multiplexer.TypeAuto {
			muxType = parsed
		}
	}
	mux, _ := multiplexer.GetMultiplexer(muxType)
	if mux == nil {
		return nil, fmt.Errorf("no %s multiplexer available for run %s#%s", muxType, info.IssueID, info.RunID)
	}
	return mux, nil
}

func shouldSendClaudeMultilineConfirm(agentName string, muxType multiplexer.Type, message string, noEnter bool) bool {
	return !noEnter &&
		muxType == multiplexer.TypeTmux &&
		strings.EqualFold(agentName, string(agent.AgentClaude)) &&
		multiplexer.NeedsBracketedPaste(message)
}

func buildRemoteOpenCodeAttachScript(info *orchapi.AttachInfo) (string, error) {
	if info == nil {
		return "", fmt.Errorf("attach info required")
	}
	if info.ServerPort > 0 {
		args := []string{
			"exec", "opencode", "attach", shellQuote(fmt.Sprintf("http://127.0.0.1:%d", info.ServerPort)),
		}
		if strings.TrimSpace(info.OpenCodeSessionID) != "" {
			args = append(args, "--session", shellQuote(info.OpenCodeSessionID))
		}
		if strings.TrimSpace(info.WorktreePath) != "" {
			args = append(args, "--dir", shellQuote(info.WorktreePath))
		}
		return strings.Join(args, " "), nil
	}
	if strings.TrimSpace(info.OpenCodeSessionID) == "" || strings.TrimSpace(info.WorktreePath) == "" {
		return "", fmt.Errorf("no opencode server port and no session to resume")
	}
	return "cd " + shellQuote(info.WorktreePath) + " && exec opencode --session " + shellQuote(info.OpenCodeSessionID) + " " + shellQuote(info.WorktreePath), nil
}
