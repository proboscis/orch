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
// Uses claude-squad logic:
//   - outputChanged=true → Running (agent is actively working)
//   - outputChanged=false && hasPrompt → Blocked
//   - outputChanged=false && agentExited → Unknown (shell prompt showing)
//   - outputChanged=false && !hasPrompt → check for done/failed, otherwise no change
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

	// Claude-squad logic for Running vs Blocked
	if outputChanged {
		// Output changed → agent is actively working → Running
		return model.StatusRunning
	}

	// Output hasn't changed
	if hasPrompt {
		// Has prompt and output stable → Blocked (waiting for input)
		return model.StatusBlocked
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
// Detects both permission dialogs and normal input prompts
func (d *Daemon) isWaitingForInput(output string) bool {
	// Claude Code permission dialog
	// Source: claude-squad uses this exact pattern
	if strings.Contains(output, "No, and tell Claude what to do differently") {
		return true
	}
	if strings.Contains(output, "tell Claude what to do differently") {
		return true
	}

	// Claude Code normal input prompt - waiting for user to type/send
	// The "↵ send" indicator shows Claude is at the input prompt
	if strings.Contains(output, "↵ send") {
		return true
	}

	// Also detect the "accept edits" prompt
	if strings.Contains(output, "accept edits") {
		return true
	}

	return false
}

// isAgentExited checks if the agent process has exited and shell prompt is showing
// This happens when the user kills Claude or it crashes
func (d *Daemon) isAgentExited(output string) bool {
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

	// Common shell prompt patterns at the end of output
	// These indicate the agent exited and we're back at shell
	shellPrompts := []string{
		"➜",  // oh-my-zsh arrow
		"❯",  // starship/other prompts
		"$",  // bash default
		"%",  // zsh default
		"#",  // root prompt
	}

	for _, prompt := range shellPrompts {
		if strings.Contains(lastLine, prompt) {
			// Make sure it's not Claude's output by checking for Claude-specific patterns
			if strings.Contains(output, "↵ send") ||
				strings.Contains(output, "accept edits") ||
				strings.Contains(output, "Claude") {
				return false // Still in Claude
			}
			return true
		}
	}

	// Also detect "git:(" pattern which appears in many shell prompts
	if strings.Contains(lastLine, "git:(") {
		if !strings.Contains(output, "↵ send") &&
			!strings.Contains(output, "accept edits") {
			return true
		}
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
