package daemon

import (
	"crypto/md5"
	"encoding/hex"
	"regexp"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
)

const deadChecksBeforeFailed = 3

func (d *Daemon) monitorRun(run *model.Run) error {
	state := d.getOrCreateState(run)
	state.LastCheckAt = time.Now()

	mgr := agent.GetManager(run)

	if mgr.IsAlive(run) {
		state.WasAlive = true
		state.DeadCheckCount = 0
	} else {
		state.DeadCheckCount++
		if !state.WasAlive {
			d.logger.Printf("%s#%s: agent not alive yet (never confirmed alive), waiting", run.IssueID, run.RunID)
			return nil
		}
		if state.DeadCheckCount < deadChecksBeforeFailed {
			d.logger.Printf("%s#%s: agent not alive (%d/%d checks), waiting", run.IssueID, run.RunID, state.DeadCheckCount, deadChecksBeforeFailed)
			return nil
		}
		deadStatus := mgr.DeadStatus()
		d.logger.Printf("%s#%s: agent confirmed dead after %d checks, marking %s", run.IssueID, run.RunID, state.DeadCheckCount, deadStatus)
		return d.updateStatus(run, deadStatus)
	}

	output, err := mgr.CaptureOutput(run)
	if err != nil {
		d.logger.Printf("%s#%s: failed to capture output: %v", run.IssueID, run.RunID, err)
		return nil
	}

	contentHash := hashContent(output)
	outputChanged := contentHash != state.OutputHash
	hasPrompt := mgr.DetectPrompt(output)

	if outputChanged {
		state.OutputHash = contentHash
		state.LastOutput = output
		state.LastOutputAt = time.Now()
	}

	if prURL := d.detectPRCreation(output); prURL != "" {
		if !state.PRRecorded {
			d.logger.Printf("%s#%s: PR created: %s", run.IssueID, run.RunID, prURL)
			if err := d.recordPRArtifact(run, prURL); err != nil {
				d.logger.Printf("%s#%s: failed to record PR artifact: %v", run.IssueID, run.RunID, err)
			} else {
				state.PRRecorded = true
				if err := d.updateStatus(run, model.StatusPROpen); err != nil {
					d.logger.Printf("%s#%s: failed to update status to pr_open: %v", run.IssueID, run.RunID, err)
				}
				return nil
			}
		}
	}

	agentState := &agent.RunState{
		LastOutput:   state.LastOutput,
		LastOutputAt: state.LastOutputAt,
		LastCheckAt:  state.LastCheckAt,
		OutputHash:   state.OutputHash,
		PRRecorded:   state.PRRecorded,
	}
	newStatus := mgr.GetStatus(run, output, agentState, outputChanged, hasPrompt)

	if newStatus != "" && newStatus != run.Status {
		d.logger.Printf("%s#%s: status change %s -> %s", run.IssueID, run.RunID, run.Status, newStatus)
		return d.updateStatus(run, newStatus)
	}

	return nil
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

// hashContent hashes the main content area, excluding the status bar
// This prevents token counter updates from causing false "output changed" signals
func hashContent(output string) string {
	lines := strings.Split(output, "\n")
	// Skip the last 5 lines (status bar area: tokens, shortcuts, prompts)
	if len(lines) > 5 {
		lines = lines[:len(lines)-5]
	}
	return hashString(strings.Join(lines, "\n"))
}

// prURLRegex matches GitHub/GitLab PR URLs
var prURLRegex = regexp.MustCompile(`https://(?:github\.com|gitlab\.com)/[^\s]+/pull/\d+|https://(?:github\.com|gitlab\.com)/[^\s]+/merge_requests/\d+`)

// detectPRCreation scans output for PR creation URLs
// Returns the first PR URL found, or empty string if none
func (d *Daemon) detectPRCreation(output string) string {
	// Look for GitHub/GitLab PR URLs in the output
	match := prURLRegex.FindString(output)
	if match != "" {
		return match
	}
	return ""
}

func (d *Daemon) recordPRArtifact(run *model.Run, prURL string) error {
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	event := model.NewArtifactEvent("pr", map[string]string{
		"url": prURL,
	})
	return d.store.AppendEvent(ref, event)
}
