package daemon

import (
	"errors"
	"fmt"
	"strings"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
)

// explainDeadUnreapedSession adds the operator escape path only when the
// daemon has the durable facts that distinguish a dead session with recorded
// agent identity from an ordinary session lookup failure. It does not widen
// ADR-0005 R5: reviveIfReaped remains the sole auto-revive gate.
func explainDeadUnreapedSession(run *model.Run, err error) error {
	if run == nil || err == nil || run.SessionReaped() || strings.TrimSpace(run.AgentSessionID) == "" {
		return err
	}

	var notFound *agent.SessionNotFoundError
	if !errors.As(err, &notFound) {
		return err
	}

	return fmt.Errorf(
		"%w; the session is gone but was not reaped by the daemon, so the L-S3 latch is unset and auto-revive does not apply; use `orch restart-from %s` (or the `--branch` form) to continue the work, or wait for the monitor's verdict",
		err,
		run.Ref().String(),
	)
}
