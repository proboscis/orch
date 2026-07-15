package daemon

import (
	"errors"
	"fmt"
	"strings"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
)

// deadUnlatchedSessionGuidance explains a session-not-found hit by an entry
// verb (send/attach) on a run whose session died WITHOUT the daemon reaping
// it: the run has a recorded agent_session identity but no session_reaped
// note for its current generation, so the ADR-0005 L-S3 latch is unset and
// auto-revive (R5: daemon-reaped sessions only) does not apply. The guidance
// names the sanctioned escape path instead of leaving the user at a bare
// "session not found". Returns "" when the classification does not hold
// (reaped runs are owned by the revive path; runs without a recorded
// identity already fail with the revive-precondition wording).
func deadUnlatchedSessionGuidance(run *model.Run) string {
	if run == nil || run.SessionReaped() || strings.TrimSpace(run.AgentSessionID) == "" {
		return ""
	}
	ref := run.Ref().String()
	return fmt.Sprintf(
		"the session is gone but was not reaped by the daemon: agent_session %s (generation %d) is recorded, "+
			"but no session_reaped note exists for that generation, so the L-S3 latch is unset and "+
			"auto-revive (ADR-0005 R5: daemon-reaped sessions only) does not apply; "+
			"use `orch restart-from %s` (or `orch restart-from %s --branch`) to continue the work",
		run.AgentSessionID, run.AgentSessionGeneration, ref, ref)
}

// isSessionNotFoundFailure reports whether err is a session-not-found
// verdict from a delivery attempt. Worker-lease errors arrive with their
// type flattened to a string, so the *agent.SessionNotFoundError check is
// backed by the same message match legacyRemoteSessionGone relies on.
func isSessionNotFoundFailure(err error) bool {
	if err == nil {
		return false
	}
	var notFound *agent.SessionNotFoundError
	if errors.As(err, &notFound) {
		return true
	}
	return strings.Contains(err.Error(), "not found (run may not be active)")
}

// decorateSessionNotFound appends the dead-but-unlatched guidance to a
// session-not-found failure, using the master's authoritative run facts.
// Any other error — or a run the classification does not apply to — passes
// through untouched.
func decorateSessionNotFound(err error, run *model.Run) error {
	if err == nil || !isSessionNotFoundFailure(err) {
		return err
	}
	guidance := deadUnlatchedSessionGuidance(run)
	if guidance == "" {
		return err
	}
	return fmt.Errorf("%w — %s", err, guidance)
}
