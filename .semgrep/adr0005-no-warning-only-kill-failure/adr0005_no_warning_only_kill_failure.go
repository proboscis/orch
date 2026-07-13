//go:build semgrepfixture

// Semgrep test fixture for ADR-0005 R3/LS6. Parsed by `semgrep test`, never compiled.
package fixture

import "fmt"

type killMultiplexer interface {
	KillSession(string) error
}

type killLogger interface {
	Printf(string, ...any)
}

func badWarningOnlyKill(mux killMultiplexer, logger killLogger, sessionName string) error {
	if err := mux.KillSession(sessionName); err != nil {
		// ruleid: adr0005-no-warning-only-kill-failure
		logger.Printf("warning: failed to kill session %s: %v", sessionName, err)
	}
	return nil
}

func okPropagatedKill(mux killMultiplexer, sessionName string) error {
	if err := mux.KillSession(sessionName); err != nil {
		// ok: adr0005-no-warning-only-kill-failure
		return fmt.Errorf("kill session %s: %w", sessionName, err)
	}
	return nil
}
