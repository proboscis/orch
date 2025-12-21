package daemon

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
)

// monitorRun checks the state of a single run and updates if needed
// Uses the same logic as claude-squad:
//   - If output changed → Running (agent is actively working)
//   - If output NOT changed AND has prompt → Blocked (waiting for input)
//   - If output NOT changed AND no prompt → no change
func (d *Daemon) monitorRun(run *model.Run) error {
	state := d.getOrCreateState(run)
	state.LastCheckAt = time.Now()

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	// Check if tmux session exists
	if !tmux.HasSession(sessionName) {
		// Session is gone - mark as failed
		d.logger.Printf("%s#%s: session gone, marking failed", run.IssueID, run.RunID)
		return d.updateStatus(run, model.StatusFailed)
	}

	// Capture pane output
	output, err := tmux.CapturePane(sessionName, 100)
	if err != nil {
		d.logger.Printf("%s#%s: failed to capture pane: %v", run.IssueID, run.RunID, err)
		return nil // Don't fail the run just because capture failed
	}

	// Check for output changes (same as claude-squad's HasUpdated)
	outputHash := hashString(output)
	outputChanged := outputHash != state.OutputHash
	hasPrompt := d.isWaitingForInput(output)

	if outputChanged {
		// Output changed - update tracking
		state.OutputHash = outputHash
		state.LastOutput = output
		state.LastOutputAt = time.Now()
	}

	// Detect state using claude-squad logic:
	// - updated=true → Running
	// - updated=false && hasPrompt → Blocked
	// - updated=false && !hasPrompt → no change (but check for done/failed)
	newStatus := d.detectStatus(run, output, state, outputChanged, hasPrompt)

	if newStatus != "" && newStatus != run.Status {
		d.logger.Printf("%s#%s: status change %s -> %s", run.IssueID, run.RunID, run.Status, newStatus)
		return d.updateStatus(run, newStatus)
	}

	return nil
}

// detectStatus analyzes the output to determine the run status
// Priority order:
//  1. Agent exited (shell prompt) → Unknown
//  2. Completion/error patterns → Done/Failed
//  3. Has input prompt → Blocked (even if output is changing slightly)
//  4. Output changing → Running
//  5. Otherwise → no change
func (d *Daemon) detectStatus(run *model.Run, output string, state *RunState, outputChanged, hasPrompt bool) model.Status {
	// Check for agent exit first (shell prompt showing = agent died/exited)
	if d.isAgentExited(output) {
		return model.StatusUnknown
	}

	// Check for completion patterns (terminal states)
	if d.isCompleted(output) {
		return model.StatusDone
	}

	// Check for error patterns (terminal states)
	if d.isFailed(output) {
		return model.StatusFailed
	}

	// If Claude is showing an input prompt, it's blocked - even if output has minor changes
	// This prevents oscillation due to token counter updates, cursor blinks, etc.
	if hasPrompt {
		return model.StatusBlocked
	}

	// Output changed and no prompt → agent is actively working
	if outputChanged {
		return model.StatusRunning
	}

	// No change, no prompt - check for stalling (just log, don't change status)
	if time.Since(state.LastOutputAt) > StallThreshold {
		d.logger.Printf("%s#%s: stalling (no output for %v)", run.IssueID, run.RunID, time.Since(state.LastOutputAt))
	}

	// No status change
	return ""
}

// isCompleted checks if the output indicates the agent completed successfully
func (d *Daemon) isCompleted(output string) bool {
	// Be very conservative about marking as done
	// Only do so if we see very explicit completion messages
	// AND the session appears to have ended

	lines := getLastLines(output, 5)
	lowerOutput := strings.ToLower(lines)

	// Very explicit completion patterns only
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

// isFailed checks if the output indicates the agent failed
func (d *Daemon) isFailed(output string) bool {
	lines := getLastLines(output, 10)
	lowerOutput := strings.ToLower(lines)

	// Error patterns
	errorPatterns := []string{
		"fatal error",
		"unrecoverable error",
		"agent crashed",
		"session terminated",
		"authentication failed",
		"rate limit exceeded",
	}

	for _, pattern := range errorPatterns {
		if strings.Contains(lowerOutput, pattern) {
			return true
		}
	}

	return false
}

// isWaitingForInput checks if the agent is waiting for user input
// Detects permission dialogs, input prompts, and menus
func (d *Daemon) isWaitingForInput(output string) bool {
	promptPatterns := []string{
		// Permission dialog
		"No, and tell Claude what to do differently",
		"tell Claude what to do differently",
		// Input prompt (with text typed)
		"↵ send",
		// Input prompt (empty/idle)
		"? for shortcuts",
		// Accept edits prompt
		"accept edits",
		// Bypass permissions mode (status bar indicator)
		"bypass permissions",
		"shift+tab to cycle",
		// Resume/project menu
		"Esc to cancel",
		"to show all projects",
	}

	for _, pattern := range promptPatterns {
		if strings.Contains(output, pattern) {
			return true
		}
	}

	return false
}

// isAgentExited checks if the agent process has exited and shell prompt is showing
// This happens when the user kills Claude or it crashes
func (d *Daemon) isAgentExited(output string) bool {
	// First, check for ANY Claude UI patterns - if present, agent is still running
	claudePatterns := []string{
		"↵ send",
		"accept edits",
		"? for shortcuts",
		"tell Claude what to do differently",
		"tokens",              // token counter at bottom
		"Esc to cancel",       // menu option
		"to show all projects", // menu option
	}

	for _, pattern := range claudePatterns {
		if strings.Contains(output, pattern) {
			return false // Claude is still running
		}
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return false
	}

	// Get the last non-empty line
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

	// Shell prompt pattern: line ends with common prompt characters
	// AND contains path or git info typical of shell prompts
	if strings.Contains(lastLine, "git:(") && strings.Contains(lastLine, ")") {
		// Looks like a shell prompt with git branch info
		return true
	}

	// Check for common shell prompt endings (must be at/near end of line)
	if strings.HasSuffix(lastLine, "$ ") ||
		strings.HasSuffix(lastLine, "% ") ||
		strings.HasSuffix(lastLine, "# ") ||
		strings.HasSuffix(lastLine, "❯ ") ||
		strings.HasSuffix(lastLine, "➜ ") ||
		strings.HasSuffix(strings.TrimRight(lastLine, " "), "$") ||
		strings.HasSuffix(strings.TrimRight(lastLine, " "), "%") ||
		strings.HasSuffix(strings.TrimRight(lastLine, " "), "✗") {
		return true
	}

	return false
}

// updateStatus appends a status event to the run
func (d *Daemon) updateStatus(run *model.Run, status model.Status) error {
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	event := model.NewStatusEvent(status)
	return d.store.AppendEvent(ref, event)
}

// hashString returns a simple hash of a string
func hashString(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// getLastLines returns the last n lines of a string
func getLastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
