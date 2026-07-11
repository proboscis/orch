package agent

// Meta-test for the gate-rule table (run-state-machine.md §9.4): every rule
// is audited mechanically so contributing a gate stays "append one row + one
// fixture". The daemon-side fixture test (internal/daemon) additionally locks
// each fixture's winner across the full reading precedence.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gateFixtures returns fixture file paths for (agent, kind):
// testdata/gates/<agent>_<kind>*.txt.
func gateFixtures(t *testing.T, agentName, kind string) []string {
	t.Helper()
	pattern := filepath.Join("testdata", "gates", agentName+"_"+kind+"*.txt")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	return matches
}

func TestGateRuleTable(t *testing.T) {
	for agentName, rules := range gateRules {
		for _, rule := range rules {
			t.Run(agentName+"/"+rule.Kind, func(t *testing.T) {
				if len(rule.All) < 2 {
					t.Fatalf("rule %s/%s has %d substrings; the false-positive budget requires >= 2 conjunctive substrings", agentName, rule.Kind, len(rule.All))
				}
				if rule.Kind == GateLogin && rule.AutoAck {
					t.Fatalf("rule %s/%s: login gates must NEVER be AutoAck — credentials belong to humans (L-G1)", agentName, rule.Kind)
				}
				for _, sub := range rule.All {
					if sub != strings.ToLower(sub) {
						t.Fatalf("rule %s/%s substring %q is not lowercase; matching lowercases the pane first", agentName, rule.Kind, sub)
					}
					if strings.TrimSpace(sub) == "" {
						t.Fatalf("rule %s/%s has an empty substring", agentName, rule.Kind)
					}
				}

				fixtures := gateFixtures(t, agentName, rule.Kind)
				if len(fixtures) == 0 {
					t.Fatalf("rule %s/%s has no fixture; capture the real screen into testdata/gates/%s_%s.txt — never write rules from memory", agentName, rule.Kind, agentName, rule.Kind)
				}
				for _, fixture := range fixtures {
					pane, err := os.ReadFile(fixture)
					if err != nil {
						t.Fatalf("read fixture %s: %v", fixture, err)
					}
					if got := DetectGate(agentName, string(pane)); got != rule.Kind {
						t.Errorf("DetectGate(%s, %s) = %q, want %q", agentName, fixture, got, rule.Kind)
					}
					// Busy veto: the identical screen with an in-progress
					// marker visible is never a gate (an agent quoting gate
					// text in its transcript must not match).
					busy := string(pane) + "\nesc to interrupt"
					if got := DetectGate(agentName, busy); got != "" {
						t.Errorf("DetectGate(%s, %s + busy marker) = %q, want busy veto", agentName, fixture, got)
					}
				}
			})
		}
	}
}

// TestGateFixturesAllOwned rejects orphan fixtures: every file in
// testdata/gates must correspond to a rule in the table.
func TestGateFixturesAllOwned(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "gates", "*.txt"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no gate fixtures found; the table meta-test depends on them")
	}
	for _, file := range files {
		base := strings.TrimSuffix(filepath.Base(file), ".txt")
		owned := false
		for agentName, rules := range gateRules {
			for _, rule := range rules {
				if strings.HasPrefix(base, agentName+"_"+rule.Kind) {
					owned = true
				}
			}
		}
		if !owned {
			t.Errorf("fixture %s matches no gate rule; name it <agent>_<kind>*.txt for an existing rule", file)
		}
	}
}

// TestDetectGateNoRules: backends without rules (opencode, custom, unknown)
// never detect gates.
func TestDetectGateNoRules(t *testing.T) {
	pane := "sign in with chatgpt\npress enter to continue"
	for _, agentName := range []string{"opencode", "custom", "", "unknown-backend"} {
		if got := DetectGate(agentName, pane); got != "" {
			t.Errorf("DetectGate(%q) = %q, want \"\"", agentName, got)
		}
	}
}
