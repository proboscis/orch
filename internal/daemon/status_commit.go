package daemon

import (
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/store"
)

// commitRunStatus is the single constructor-and-append site for run status
// events in the daemon (docs/design/run-state-machine.md §1). Every status
// transition decided by stepRun — monitor plane (Daemon.updateStatus) and
// launch plane (SocketServer.reportLaunchProgress) — is committed here, and
// only here. All other status writers are frozen legacy, enumerated by
// `nosemgrep: run-status-write-surface` annotations, and shrink toward zero
// (coupling-core roadmap Phase B).
//
// It re-checks the authoritative store state beneath the stepRun matrix:
// terminal protection, transition legality, and the same-status no-op.
// reason, when non-empty, is recorded on the status event as the
// machine-readable verdict reason (model.AttrStatusReason).
//
// Returns the store-derived from-status and whether a transition was
// committed. committed == false with a nil error means the write was
// absorbed (same-status no-op, or illegal for the daemon source); the
// caller must not publish or notify in that case.
func commitRunStatus(st store.Store, run *model.Run, status model.Status, reason string, debugf func(format string, args ...interface{})) (model.Status, bool, error) {
	return commitRunStatusFromSource(st, run, status, reason, model.EventSourceDaemon, debugf)
}

// commitRunStatusFromSource is commitRunStatus with an explicit event
// source. The only non-daemon source today is the revive ladder (ADR-0005
// R5): terminal re-entry via send/attach is a user command, and the store's
// append guard requires source=user for a terminal exit. The source travels
// on the status event (model.AttrStatusSource) so the store re-validates the
// same fact this function checked.
func commitRunStatusFromSource(st store.Store, run *model.Run, status model.Status, reason string, source model.EventSource, debugf func(format string, args ...interface{})) (model.Status, bool, error) {
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	if source == "" {
		source = model.EventSourceDaemon
	}

	// Check current status - the daemon source cannot overwrite terminal
	// states; a user source may (CanTransitionStatus).
	var fromStatus model.Status
	if currentRun, err := st.GetRun(ref); err == nil && currentRun != nil {
		fromStatus = currentRun.Status
		// Re-affirming the current (status, reason) pair is a no-op:
		// appending duplicate status events bloats the run record and churns
		// UpdatedAt, which breaks recency display/sorting for every client.
		// The no-op identity is the PAIR (§9.3 D-G1): a run already waiting
		// on its normal prompt that then hits a gate must not keep a stale
		// reason forever, so a confirmed reading change re-appends and fires
		// the listener once (L8/L10c).
		if fromStatus == status && currentRun.StatusReason() == reason {
			return fromStatus, false, nil
		}
		if !model.CanTransitionStatus(currentRun.Status, status, source) {
			if debugf != nil {
				debugf("%s#%s: %s source cannot transition from %s to %s", run.IssueID, run.RunID, source, currentRun.Status, status)
			}
			return fromStatus, false, nil
		}
	}

	event := model.NewStatusEventWithReasonAndSource(status, reason, source) // nosemgrep: run-status-write-surface
	if err := st.AppendEvent(ref, event); err != nil {
		return fromStatus, false, err
	}
	return fromStatus, true, nil
}
