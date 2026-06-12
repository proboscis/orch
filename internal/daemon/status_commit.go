package daemon

import (
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
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
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}

	// Check current status - daemon cannot overwrite terminal states
	var fromStatus model.Status
	if currentRun, err := st.GetRun(ref); err == nil && currentRun != nil {
		fromStatus = currentRun.Status
		// Re-affirming the current status is a no-op: appending duplicate
		// status events bloats the run record and churns UpdatedAt, which
		// breaks recency display/sorting for every client.
		if fromStatus == status {
			return fromStatus, false, nil
		}
		if !model.CanTransitionStatus(currentRun.Status, status, model.EventSourceDaemon) {
			if debugf != nil {
				debugf("%s#%s: daemon cannot transition from %s to %s", run.IssueID, run.RunID, currentRun.Status, status)
			}
			return fromStatus, false, nil
		}
	}

	event := model.NewStatusEventWithReason(status, reason) // nosemgrep: run-status-write-surface
	if err := st.AppendEvent(ref, event); err != nil {
		return fromStatus, false, err
	}
	return fromStatus, true, nil
}
