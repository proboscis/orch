package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/orchapi"
)

const remoteCodexTmuxSubmitDelay = 250 * time.Millisecond
const remoteClaudeTmuxMultilineSubmitDelay = 100 * time.Millisecond

var runSSHOutputCommand = func(args []string) ([]byte, error) {
	cmd := exec.Command("ssh", args...)
	return cmd.CombinedOutput()
}

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

func runSSHScriptOutput(targetHost, script string) ([]byte, error) {
	args := sshScriptArgs(targetHost, false, script)
	return runSSHOutputArgs(args)
}

func runSSHOutputArgs(args []string) ([]byte, error) {
	out, err := runSSHOutputCommand(args)
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return nil, fmt.Errorf("%w: %s", err, trimmed)
		}
		return nil, err
	}
	return out, nil
}

func runRemotePython(targetHost, script string, args ...string) ([]byte, error) {
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal remote python args: %w", err)
	}
	wrapped := strings.Join([]string{
		"import json, os, sys",
		`sys.argv = ["-"] + json.loads(os.environ["ORCH_REMOTE_PY_ARGS"])`,
		script,
	}, "\n")
	command := strings.Join([]string{
		"ORCH_REMOTE_PY_ARGS=" + shellQuote(string(encodedArgs)) + " python3 - <<'PY'",
		wrapped,
		"PY",
	}, "\n")
	return runSSHScriptOutput(targetHost, command)
}

func captureRemoteFromInfo(info *orchapi.AttachInfo, lines int) (*orchapi.CaptureResult, error) {
	if info == nil {
		return nil, fmt.Errorf("attach info required")
	}
	if strings.TrimSpace(info.TargetHost) == "" {
		return nil, fmt.Errorf("target host missing for %s#%s", info.IssueID, info.RunID)
	}
	if lines <= 0 {
		lines = 100
	}

	if strings.EqualFold(info.Agent, string(agent.AgentOpenCode)) {
		return captureRemoteOpenCodeFromInfo(info, lines)
	}
	return captureRemoteMultiplexerFromInfo(info, lines)
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

func captureRemoteMultiplexerFromInfo(info *orchapi.AttachInfo, lines int) (*orchapi.CaptureResult, error) {
	sessionName := sessionNameFromAttachInfo(info)
	if sessionName == "" {
		return nil, fmt.Errorf("run %s#%s has no session name on host %s", info.IssueID, info.RunID, info.TargetHost)
	}

	var script string
	source := "tmux"
	if info.Multiplexer == orchapi.MultiplexerZellij {
		source = "zellij"
		script = strings.Join([]string{
			"set -e",
			"tmp=$(mktemp)",
			`trap 'rm -f "$tmp"' EXIT`,
			"zellij --session " + shellQuote(sessionName) + " action dump-screen " + `"$tmp"`,
			`cat "$tmp"`,
		}, "; ")
	} else {
		args := []string{"-T", info.TargetHost, "tmux", "capture-pane", "-t", sessionName, "-p", "-S", fmt.Sprintf("-%d", lines)}
		out, err := runSSHOutputArgs(args)
		if err != nil {
			return nil, fmt.Errorf("failed to capture %s session %q on host %s: %w", source, sessionName, info.TargetHost, err)
		}
		return &orchapi.CaptureResult{
			Content:   string(out),
			Timestamp: time.Now(),
			Source:    source,
		}, nil
	}

	out, err := runSSHScriptOutput(info.TargetHost, script)
	if err != nil {
		return nil, fmt.Errorf("failed to capture %s session %q on host %s: %w", source, sessionName, info.TargetHost, err)
	}

	content := string(out)
	if source == "zellij" {
		allLines := strings.Split(content, "\n")
		if len(allLines) > lines {
			allLines = allLines[len(allLines)-lines:]
		}
		content = strings.Join(allLines, "\n")
	}

	return &orchapi.CaptureResult{
		Content:   content,
		Timestamp: time.Now(),
		Source:    source,
	}, nil
}

func captureRemoteOpenCodeFromInfo(info *orchapi.AttachInfo, lines int) (*orchapi.CaptureResult, error) {
	if info.ServerPort <= 0 {
		return nil, fmt.Errorf("run %s#%s has no opencode server port on host %s", info.IssueID, info.RunID, info.TargetHost)
	}
	if strings.TrimSpace(info.OpenCodeSessionID) == "" {
		return nil, fmt.Errorf("run %s#%s has no opencode session ID on host %s", info.IssueID, info.RunID, info.TargetHost)
	}

	py := `import json, sys, time, urllib.request
port = ` + fmt.Sprintf("%q", fmt.Sprintf("%d", info.ServerPort)) + `
session = ` + fmt.Sprintf("%q", info.OpenCodeSessionID) + `
worktree = ` + fmt.Sprintf("%q", strings.TrimSpace(info.WorktreePath)) + `
max_lines = ` + fmt.Sprintf("%d", lines) + `
url = f"http://127.0.0.1:{port}/session/{session}/message"
for attempt in range(3):
    req = urllib.request.Request(url)
    if worktree:
        req.add_header("X-OpenCode-Directory", worktree)
    with urllib.request.urlopen(req, timeout=5) as resp:
        payload = resp.read().decode()
    try:
        messages = json.loads(payload)
    except json.JSONDecodeError:
        if attempt == 2:
            raise
        time.sleep(0.15)
        continue
    all_lines = []
    for message in messages:
        parts = message.get("parts") or []
        if not parts:
            continue
        info = message.get("info") or {}
        role = (info.get("role") or "unknown").upper()
        header = f"--- [{role}] ---"
        text_lines = []
        tool_use_count = 0
        for part in parts:
            part_type = part.get("type")
            if part_type == "tool_use":
                tool_use_count += 1
            text = part.get("text") or ""
            if part_type == "text" and text:
                text_lines.extend(text.splitlines())
        if text_lines:
            all_lines.append(header)
            all_lines.extend(text_lines)
            continue
        if tool_use_count:
            all_lines.append(f"{header} ({tool_use_count} tool uses)")
    if max_lines > 0 and len(all_lines) > max_lines:
        all_lines = all_lines[-max_lines:]
    sys.stdout.write("\n".join(all_lines))
    break`
	command := strings.Join([]string{
		"python3 - <<'PY'",
		py,
		"PY",
	}, "\n")
	out, err := runSSHScriptOutput(info.TargetHost, command)
	if err != nil {
		return nil, fmt.Errorf("failed to capture opencode session %q on host %s: %w", info.OpenCodeSessionID, info.TargetHost, err)
	}

	return &orchapi.CaptureResult{
		Content:   string(out),
		Timestamp: time.Now(),
		Source:    "opencode",
	}, nil
}

func sendRemoteFromInfo(info *orchapi.AttachInfo, message string, noEnter bool) error {
	if info == nil {
		return fmt.Errorf("attach info required")
	}
	if strings.TrimSpace(info.TargetHost) == "" {
		return fmt.Errorf("target host missing for %s#%s", info.IssueID, info.RunID)
	}
	if strings.EqualFold(info.Agent, string(agent.AgentOpenCode)) {
		return sendRemoteOpenCodeFromInfo(info, message)
	}
	return sendRemoteMultiplexerFromInfo(info, message, noEnter)
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

func sendRemoteMultiplexerFromInfo(info *orchapi.AttachInfo, message string, noEnter bool) error {
	sessionName := sessionNameFromAttachInfo(info)
	if sessionName == "" {
		return fmt.Errorf("run %s#%s has no session name on host %s", info.IssueID, info.RunID, info.TargetHost)
	}

	if multiplexer.NeedsBracketedPaste(message) {
		message = multiplexer.NormalizeLineEndings(message)
		switch info.Multiplexer {
		case orchapi.MultiplexerZellij:
			return sendRemoteZellijBracketedPaste(info, sessionName, message, noEnter)
		default:
			return sendRemoteTmuxBracketedPaste(info, sessionName, message, noEnter)
		}
	}

	switch info.Multiplexer {
	case orchapi.MultiplexerZellij:
		if _, err := runSSHOutputArgs([]string{"-T", info.TargetHost, "zellij", "--session", sessionName, "action", "write-chars", "--", message}); err != nil {
			return fmt.Errorf("failed to send message to session %q on host %s: %w", sessionName, info.TargetHost, err)
		}
		if !noEnter {
			if _, err := runSSHOutputArgs([]string{"-T", info.TargetHost, "zellij", "--session", sessionName, "action", "write", "10"}); err != nil {
				return fmt.Errorf("failed to send submit key to session %q on host %s: %w", sessionName, info.TargetHost, err)
			}
		}
		return nil
	default:
		if _, err := runSSHOutputArgs([]string{"-T", info.TargetHost, "tmux", "send-keys", "-t", sessionName, "-l", message}); err != nil {
			return fmt.Errorf("failed to send message to session %q on host %s: %w", sessionName, info.TargetHost, err)
		}
		if !noEnter {
			if strings.EqualFold(info.Agent, string(agent.AgentCodex)) {
				time.Sleep(remoteCodexTmuxSubmitDelay)
			}
			if _, err := runSSHOutputArgs([]string{"-T", info.TargetHost, "tmux", "send-keys", "-t", sessionName, "Enter"}); err != nil {
				return fmt.Errorf("failed to send submit key to session %q on host %s: %w", sessionName, info.TargetHost, err)
			}
		}
		return nil
	}
}

func sendRemoteTmuxBracketedPaste(info *orchapi.AttachInfo, sessionName, message string, noEnter bool) error {
	lines := []string{
		"set -e",
		`buf="orch-send-$$"`,
		`trap 'tmux delete-buffer -b "$buf" >/dev/null 2>&1 || true' EXIT`,
		"tmux set-buffer -b \"$buf\" " + shellQuote(message),
		"tmux paste-buffer -b \"$buf\" -p -t " + shellQuote(sessionName),
	}
	if !noEnter {
		if strings.EqualFold(info.Agent, string(agent.AgentCodex)) {
			lines = append(lines, "sleep 0.25")
		}
		lines = append(lines, "tmux send-keys -t "+shellQuote(sessionName)+" Enter")
		if shouldSendClaudeMultilineConfirm(info.Agent, multiplexer.TypeTmux, message, noEnter) {
			lines = append(lines, "sleep 0.1")
			lines = append(lines, "tmux send-keys -t "+shellQuote(sessionName)+" Enter")
		}
	}
	if _, err := runSSHScriptOutput(info.TargetHost, strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("failed to send message to session %q on host %s: %w", sessionName, info.TargetHost, err)
	}
	return nil
}

func shouldSendClaudeMultilineConfirm(agentName string, muxType multiplexer.Type, message string, noEnter bool) bool {
	return !noEnter &&
		muxType == multiplexer.TypeTmux &&
		strings.EqualFold(agentName, string(agent.AgentClaude)) &&
		multiplexer.NeedsBracketedPaste(message)
}

func sendRemoteZellijBracketedPaste(info *orchapi.AttachInfo, sessionName, message string, noEnter bool) error {
	lines := []string{
		"set -e",
		"zellij --session " + shellQuote(sessionName) + " action write 27 91 50 48 48 126",
		"zellij --session " + shellQuote(sessionName) + " action write-chars -- " + shellQuote(message),
		"zellij --session " + shellQuote(sessionName) + " action write 27 91 50 48 49 126",
	}
	if !noEnter {
		lines = append(lines, "zellij --session "+shellQuote(sessionName)+" action write 10")
	}
	if _, err := runSSHScriptOutput(info.TargetHost, strings.Join(lines, "\n")); err != nil {
		return fmt.Errorf("failed to send message to session %q on host %s: %w", sessionName, info.TargetHost, err)
	}
	return nil
}

func sendRemoteOpenCodeFromInfo(info *orchapi.AttachInfo, message string) error {
	if info.ServerPort <= 0 {
		return fmt.Errorf("run %s#%s has no opencode server port on host %s", info.IssueID, info.RunID, info.TargetHost)
	}
	if strings.TrimSpace(info.OpenCodeSessionID) == "" {
		return fmt.Errorf("run %s#%s has no opencode session ID on host %s", info.IssueID, info.RunID, info.TargetHost)
	}

	py := `import json, sys, urllib.request
port, session, worktree, message = sys.argv[1:5]
url = f"http://127.0.0.1:{port}/session/{session}/message"
body = json.dumps({"parts":[{"type":"text","text":message}]}).encode()
req = urllib.request.Request(url, data=body, method="POST")
req.add_header("Content-Type", "application/json")
if worktree:
    req.add_header("X-OpenCode-Directory", worktree)
with urllib.request.urlopen(req, timeout=10) as resp:
    sys.stdout.write(resp.read().decode())`
	if _, err := runRemotePython(info.TargetHost, py, fmt.Sprintf("%d", info.ServerPort), info.OpenCodeSessionID, strings.TrimSpace(info.WorktreePath), message); err != nil {
		return fmt.Errorf("failed to send opencode message to session %q on host %s: %w", info.OpenCodeSessionID, info.TargetHost, err)
	}
	return nil
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
