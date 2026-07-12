//go:build semgrepfixture

// Semgrep test fixture for ADR-0005 R7. Parsed by `semgrep test`, never compiled.
package fixture

func badTickDocumentation() string {
	// ruleid: adr0005-tick-stays-dead
	return "Use `orch tick --all` to resume waiting runs."
}

func okSendDocumentation() string {
	// ok: adr0005-tick-stays-dead
	return "Use `orch send <RUN_REF> <message>` to answer waiting runs."
}
