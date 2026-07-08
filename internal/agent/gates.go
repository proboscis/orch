package agent

// Interactive-gate detection (run-state-machine.md §9, observation O4e).
//
// A gate is a pre-agent full-screen dialog (login, workspace trust, …) that
// blocks the agent until a human interacts: the pane is stable, matches no
// busy marker and no composer-prompt pattern, so without these rules the run
// is indistinguishable from productive work (the 2026-07-07 2h login-screen
// stall). Detection is gather-side observation vocabulary only — the status
// transition (waiting, reason gate_<kind>) is decided in the daemon's stepRun
// after the L10a streak confirms the reading.
//
// Contribution contract, enforced mechanically by TestGateRuleTable:
//   - every rule needs >= 2 conjunctive lowercase substrings (no
//     single-string rules — the false-positive budget of §9.5);
//   - every rule needs >= 1 positive fixture in testdata/gates/, a REAL
//     captured pane named <agent>_<kind>*.txt — never write a rule from
//     memory of what a screen "probably says";
//   - the daemon-side fixture test (TestGateFixturePrecedence in
//     internal/daemon) locks each fixture's intended winner across the full
//     reading precedence, not just the substring match.
//
// opencode contributes no rows: it is observed via its server API, and its
// login problems surface as O8 bootstrap failures (launch_opencode_*).

import "strings"

// GateKind values are the machine-readable gate vocabulary; the daemon turns
// them into status reasons as "gate_" + kind.
const (
	GateLogin = "login"
	GateTrust = "trust"
)

// gateRule declares one gate screen: the gate is detected when ALL substrings
// co-occur (lowercased) within the last gateWindowLines lines of the pane and
// no busy marker is visible.
type gateRule struct {
	Kind string
	All  []string
}

// gateWindowLines matches the IsWaitingForInput window so gate and prompt
// readings look at the same evidence.
const gateWindowLines = 40

// gateRules is the compiled-in declarative gate table, keyed by agent name
// (model.Run.Agent). Append rules here together with their fixture; the
// meta-test audits every row.
var gateRules = map[string][]gateRule{
	string(AgentCodex): {
		// testdata/gates/codex_login.txt — codex with no auth.json parks at
		// the ChatGPT sign-in menu.
		{Kind: GateLogin, All: []string{"sign in with chatgpt", "press enter to continue"}},
		// testdata/gates/codex_trust.txt — codex in an untrusted directory.
		{Kind: GateTrust, All: []string{"do you trust the contents of this directory", "press enter to continue"}},
	},
	string(AgentClaude): {
		// testdata/gates/claude_trust.txt — claude code folder-trust dialog.
		{Kind: GateTrust, All: []string{"is this a project you created or one you trust", "yes, i trust this folder"}},
	},
}

// busyMarkers veto both prompt and gate readings: a pane actively working is
// never waiting for input, even if its transcript quotes gate text.
var busyMarkers = []string{
	"esc to interrupt",
	"working (",
	"background terminal running",
}

func hasBusyMarker(lowerLines string) bool {
	for _, pattern := range busyMarkers {
		if strings.Contains(lowerLines, pattern) {
			return true
		}
	}
	return false
}

// DetectGate reports the gate kind visible on a captured pane ("" = none)
// for the given agent backend. The busy-marker veto of IsWaitingForInput
// applies identically.
func DetectGate(agentName, output string) string {
	rules := gateRules[agentName]
	if len(rules) == 0 {
		return ""
	}
	lines := strings.ToLower(getLastLines(output, gateWindowLines))
	if hasBusyMarker(lines) {
		return ""
	}
	for _, rule := range rules {
		matched := true
		for _, sub := range rule.All {
			if !strings.Contains(lines, sub) {
				matched = false
				break
			}
		}
		if matched {
			return rule.Kind
		}
	}
	return ""
}
