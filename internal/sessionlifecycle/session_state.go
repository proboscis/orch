// Package sessionlifecycle derives ADR-0005 session lifecycle facts from a
// run's event fold. It sits below daemon and store so both observation and the
// disposable FileStore index use one classifier.
package sessionlifecycle

import (
	"fmt"
	"strings"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/multiplexer"
)

// Derive returns the closed ADR-0005 session-state value and, for an
// unrevivable reaped session, the failed stored-fact precondition. It performs
// no filesystem, worker, or multiplexer probes.
func Derive(run *model.Run) (model.SessionState, string) {
	if run == nil {
		return model.SessionStateLive, ""
	}
	// Index entries intentionally omit Events. Their versioned lifecycle value
	// was produced by this function while the event fold was available.
	if len(run.Events) == 0 && run.SessionState != "" {
		return run.SessionState, run.SessionStateDetail
	}
	if !run.SessionReaped() {
		return model.SessionStateLive, ""
	}
	if err := CheckRevivePreconditions(run); err != nil {
		return model.SessionStateReapedUnrevivable, err.Error()
	}
	return model.SessionStateReapedRevivable, ""
}

// Apply stores Derive's result on run for transport and index caching.
func Apply(run *model.Run) {
	if run == nil {
		return
	}
	run.SessionState, run.SessionStateDetail = Derive(run)
}

// CheckRevivePreconditions decides revivability from stored facts alone. The
// daemon's revive path delegates here so observation and execution cannot
// drift into two classifications.
func CheckRevivePreconditions(run *model.Run) error {
	if run == nil {
		return fmt.Errorf("run required")
	}
	ref := run.Ref().String()
	switch run.Agent {
	case string(agent.AgentClaude), string(agent.AgentCodex):
	default:
		return fmt.Errorf("revive is not defined for agent %q (run %s): only claude/codex sessions record a resumable identity — use `orch restart-from --branch` for a fresh session", run.Agent, ref)
	}
	if strings.TrimSpace(run.AgentSessionID) == "" {
		return fmt.Errorf("revive precondition missing for %s: agent_session identity is not recorded — the session cannot be resumed; use `orch restart-from --branch` to continue on a fresh session", ref)
	}
	if strings.TrimSpace(run.WorktreePath) == "" {
		return fmt.Errorf("revive precondition missing for %s: no worktree is recorded — use `orch restart-from --branch`", ref)
	}
	if hasWorktreeRemovedNote(run) {
		return fmt.Errorf("revive precondition missing for %s: the worktree was removed (worktree_removed note) — use `orch restart-from --branch` to recreate one", ref)
	}
	rawMux := strings.TrimSpace(run.Multiplexer)
	if rawMux == "" {
		return fmt.Errorf("run %s has empty multiplexer; refusing session reap", ref)
	}
	muxType, err := multiplexer.ParseType(rawMux)
	if err != nil {
		return fmt.Errorf("run %s has invalid multiplexer %q: %w", ref, rawMux, err)
	}
	if muxType == multiplexer.TypeAuto {
		return fmt.Errorf("run %s has non-concrete multiplexer %q; refusing session reap", ref, rawMux)
	}
	return nil
}

func hasWorktreeRemovedNote(run *model.Run) bool {
	for _, event := range run.Events {
		if event == nil || event.Type != model.EventTypeNote {
			continue
		}
		if event.Name == "worktree_removed" || (event.Name == model.DaemonNoticeEventName && event.Attrs["kind"] == "worktree_removed") {
			return true
		}
	}
	return false
}
