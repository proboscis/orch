package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Claude Code activity spinner renders no stable keyword ("Architecting…",
// "Pondering…", …) and some UI variants show no "esc to interrupt" hint
// anywhere on the pane, while the composer prompt patterns below it still
// match. Without the spinner-line veto such a pane concludes waiting while
// the agent is demonstrably mid-task (issue claude-run-state-detection,
// re-confirmed live 2026-07-11).
func TestClaudeSpinnerPaneIsNotWaiting(t *testing.T) {
	pane, err := os.ReadFile(filepath.Join("testdata", "busy", "claude_spinner_xhigh.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	output := string(pane)

	if !strings.Contains(output, "… (") {
		t.Fatal("fixture lost its spinner line; re-capture a real working pane")
	}
	if IsWaitingForInput(output) {
		t.Fatal("working pane with active spinner must not read as waiting")
	}
	if kind := DetectGate(string(AgentClaude), output); kind != "" {
		t.Fatalf("working pane must not read as a gate, got %q", kind)
	}
}

// Control: the same pane without its spinner line is an idle composer and
// must keep reading as waiting — the veto must not over-suppress.
func TestClaudeIdlePaneStillWaiting(t *testing.T) {
	pane, err := os.ReadFile(filepath.Join("testdata", "busy", "claude_spinner_xhigh.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var kept []string
	for _, line := range strings.Split(string(pane), "\n") {
		if isSpinnerLine(strings.ToLower(line)) {
			continue
		}
		kept = append(kept, line)
	}
	idle := strings.Join(kept, "\n")
	if !IsWaitingForInput(idle) {
		t.Fatal("idle composer pane (spinner removed) must still read as waiting")
	}
}

func TestIsSpinnerLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		// Real frames observed live (glyph rotates per frame).
		{"· Architecting… (2m 54s · ↓ 7.8k tokens · thinking with xhigh effort)", true},
		{"✽ Working… (52m)", true},
		{"  ✻ Pondering… (3s)", true},
		// Bullet or transcript lines must not match: no "… (" marker.
		{"· plain bullet item", false},
		{"✽ decorative glyph without duration", false},
		// "… (" without a spinner glyph prefix is prose, not a spinner.
		{"the run stalled… (see logs)", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isSpinnerLine(tc.line); got != tc.want {
			t.Errorf("isSpinnerLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
