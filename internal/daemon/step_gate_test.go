package daemon

// Law tests for interactive-gate detection (run-state-machine.md §9): the
// L10a debounce, the L10b reading precedence, the L10c reason fidelity under
// the D-G1 (status, reason) pair identity, and the fixture-locked end-to-end
// precedence over the real captured gate screens in
// internal/agent/testdata/gates.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
)

func gateObs(kind, output string) runObservation {
	return runObservation{Kind: obsCaptured, Output: output, Signal: agentSignal{Gate: kind}}
}

// statusReasonEffectOf returns (status, reason) of the first effectSetStatus.
func statusReasonEffectOf(effects []runEffect) (model.Status, string, bool) {
	for _, e := range effects {
		if e.Kind == effectSetStatus {
			return e.Status, e.Reason, true
		}
	}
	return "", "", false
}

// L10a gate debounce: a waiting(gate_<kind>) verdict requires
// waitingPromptStreakThreshold consecutive captures with the same gate
// reading; any busy capture or different reading resets the streak.
func TestStepLawGateDebounce(t *testing.T) {
	now := time.Now()
	login := gateObs("login", "sign in with chatgpt\npress enter to continue")
	busy := runObservation{Kind: obsCaptured, Output: "working...", Signal: agentSignal{}}
	prompt := runObservation{Kind: obsCaptured, Output: "❯ ready", Signal: agentSignal{PromptShowing: true}}

	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true}

	// Single gate observation: reading recorded, no verdict, and no lower
	// verdict either (the gate masks output-changed).
	var effects []runEffect
	core, effects = stepRun(view, core, login, now)
	if _, ok := statusEffectOf(effects); ok {
		t.Fatalf("status effect after a single gate observation: %+v", effects)
	}
	if core.ReadingKind != "gate:login" || core.ReadingStreak != 1 {
		t.Fatalf("core reading = (%q, %d), want (gate:login, 1)", core.ReadingKind, core.ReadingStreak)
	}

	// Second consecutive gate capture: waiting(gate_login).
	core, effects = stepRun(view, core, login, now)
	if status, reason, ok := statusReasonEffectOf(effects); !ok || status != model.StatusWaiting || reason != "gate_login" {
		t.Fatalf("confirmed gate verdict = %+v, want waiting(gate_login)", effects)
	}
	view = foldStatus(view, effects)

	// A busy capture resets the streak and flips the run back to running
	// (output changed); a single later gate capture must not re-confirm.
	core, effects = stepRun(view, core, busy, now)
	view = foldStatus(view, effects)
	if core.ReadingKind != "" || core.ReadingStreak != 0 {
		t.Fatalf("busy capture did not reset the reading: (%q, %d)", core.ReadingKind, core.ReadingStreak)
	}
	core, effects = stepRun(view, core, login, now)
	if _, ok := statusEffectOf(effects); ok {
		t.Fatalf("verdict after streak reset + one gate capture: %+v", effects)
	}

	// Alternating readings never confirm: each kind change restarts at 1.
	core = runCore{WasAlive: true}
	view = stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	seq := []runObservation{prompt, login, prompt, login, prompt, login}
	for i, obs := range seq {
		core, effects = stepRun(view, core, obs, now)
		if status, _, ok := statusReasonEffectOf(effects); ok && status == model.StatusWaiting {
			t.Fatalf("alternating readings confirmed waiting at tick %d (streak=%d)", i, core.ReadingStreak)
		}
		view = foldStatus(view, effects)
	}
}

// L10b reading precedence: a gate reading present on a capture masks the
// generic heuristics (exited/completed/api-limited/failed/prompt/output-
// changed) while unconfirmed, and outranks them once confirmed.
func TestStepLawGatePrecedence(t *testing.T) {
	now := time.Now()
	lowerSignals := []agentSignal{
		{Exited: true},
		{Completed: true},
		{APILimited: true},
		{Failed: true},
		{PromptShowing: true},
		{}, // output-changed only
	}
	for _, lower := range lowerSignals {
		sig := lower
		sig.Gate = "trust"
		obs := runObservation{Kind: obsCaptured, Output: "do you trust the contents of this directory?\npress enter to continue", Signal: sig}

		view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
		core := runCore{WasAlive: true}

		var effects []runEffect
		core, effects = stepRun(view, core, obs, now)
		if _, ok := statusEffectOf(effects); ok {
			t.Fatalf("lower signal %+v produced a verdict beneath an unconfirmed gate: %+v", lower, effects)
		}
		_, effects = stepRun(view, core, obs, now)
		status, reason, ok := statusReasonEffectOf(effects)
		if !ok || status != model.StatusWaiting || reason != "gate_trust" {
			t.Fatalf("confirmed gate lost to lower signal %+v: %+v", lower, effects)
		}
	}
}

// L10c reason fidelity + D-G1: the waiting reason tracks the latest
// confirmed reading — a reading change re-appends even when the status
// itself stays waiting, in both directions (prompt → gate, gate → prompt).
func TestStepLawGateReasonFidelity(t *testing.T) {
	now := time.Now()
	login := gateObs("login", "sign in with chatgpt\npress enter to continue")
	prompt := runObservation{Kind: obsCaptured, Output: "❯ ready", Signal: agentSignal{PromptShowing: true}}

	// Run already waiting at its normal prompt (reason ""), then hits a gate.
	view := stepTestView(model.StatusWaiting, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true, ReadingKind: "prompt", ReadingStreak: 2}

	var effects []runEffect
	core, effects = stepRun(view, core, login, now)
	if _, ok := statusEffectOf(effects); ok {
		t.Fatalf("unconfirmed gate re-appended waiting: %+v", effects)
	}
	core, effects = stepRun(view, core, login, now)
	status, reason, ok := statusReasonEffectOf(effects)
	if !ok || status != model.StatusWaiting || reason != "gate_login" {
		t.Fatalf("waiting(\"\") -> waiting(gate_login) re-append missing (D-G1): %+v", effects)
	}
	view = foldStatus(view, effects)

	// Same confirmed reading again: no re-append (pair no-op).
	core, effects = stepRun(view, core, login, now)
	if _, ok := statusEffectOf(effects); ok {
		t.Fatalf("re-affirming waiting(gate_login) appended again: %+v", effects)
	}

	// Gate cleared to the normal composer prompt: the changed output flips
	// the run to running first (§9.5 counterexample 5 — no stale gate
	// reason persists), then the confirmed prompt lands waiting("").
	core, effects = stepRun(view, core, prompt, now)
	if status, reason, ok := statusReasonEffectOf(effects); !ok || status != model.StatusRunning || reason != "" {
		t.Fatalf("cleared gate (changed output) did not resume running: %+v", effects)
	}
	view = foldStatus(view, effects)
	_, effects = stepRun(view, core, prompt, now)
	status, reason, ok = statusReasonEffectOf(effects)
	if !ok || status != model.StatusWaiting || reason != "" {
		t.Fatalf("confirmed prompt after gate did not land waiting(\"\") (L10c): %+v", effects)
	}
}

// §9.5 counterexample 5: a gate completed by the user changes the output;
// the next changed capture flips the run back to running with no stale
// waiting(gate_*).
func TestStepGateClearedResumesRunning(t *testing.T) {
	now := time.Now()
	login := gateObs("login", "sign in with chatgpt\npress enter to continue")
	working := runObservation{Kind: obsCaptured, Output: "fetching model catalog...", Signal: agentSignal{}}

	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true}

	var effects []runEffect
	core, effects = stepRun(view, core, login, now)
	core, effects = stepRun(view, core, login, now)
	view = foldStatus(view, effects)
	if view.Status != model.StatusWaiting || view.StatusReason != "gate_login" {
		t.Fatalf("setup: expected waiting(gate_login), got %s(%s)", view.Status, view.StatusReason)
	}

	_, effects = stepRun(view, core, working, now)
	status, reason, ok := statusReasonEffectOf(effects)
	if !ok || status != model.StatusRunning || reason != "" {
		t.Fatalf("cleared gate did not resume running: %+v", effects)
	}
}

// TestGateFixturePrecedence locks every gate fixture's intended winner
// end-to-end (§9.4): the signal is gathered from the REAL captured pane
// exactly as gatherAgentSignal does for mux agents, then run through stepRun
// — the gate must win the full reading precedence on that pane, not just
// match its substrings. This is also the §9.5 incident replay: a stable gate
// pane confirms within waitingPromptStreakThreshold captures.
func TestGateFixturePrecedence(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("..", "agent", "testdata", "gates", "*.txt"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no gate fixtures found")
	}
	now := time.Now()
	for _, fixture := range fixtures {
		base := strings.TrimSuffix(filepath.Base(fixture), ".txt")
		agentName, rest, found := strings.Cut(base, "_")
		if !found {
			t.Fatalf("fixture %s: name must be <agent>_<kind>*.txt", fixture)
		}
		t.Run(base, func(t *testing.T) {
			paneBytes, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("read %s: %v", fixture, err)
			}
			pane := string(paneBytes)

			sig := agentSignal{
				Exited:        agent.IsAgentExited(pane),
				Completed:     agent.IsCompleted(pane),
				APILimited:    agent.IsAPILimited(pane),
				Failed:        agent.IsFailed(pane),
				PromptShowing: agent.IsWaitingForInput(pane),
				Gate:          agent.DetectGate(agentName, pane),
			}
			if sig.Gate == "" {
				t.Fatalf("fixture does not detect any gate for agent %s", agentName)
			}
			if !strings.HasPrefix(rest, sig.Gate) {
				t.Fatalf("fixture named %s detected gate %q", base, sig.Gate)
			}

			view := stepTestView(model.StatusRunning, agentName, now.Add(-time.Hour))
			core := runCore{WasAlive: true}
			obs := runObservation{Kind: obsCaptured, Output: pane, Signal: sig}

			var effects []runEffect
			core, effects = stepRun(view, core, obs, now)
			if _, ok := statusEffectOf(effects); ok {
				t.Fatalf("verdict on first capture (must debounce): %+v", effects)
			}
			_, effects = stepRun(view, core, obs, now)
			status, reason, ok := statusReasonEffectOf(effects)
			if !ok || status != model.StatusWaiting || reason != gateStatusReason(sig.Gate) {
				t.Fatalf("gate did not win the reading precedence on the real pane: %+v", effects)
			}
		})
	}
}

// L-G1 (§9.6): a confirmed AutoAck-gate reading is acknowledged by the daemon
// exactly once per (run, gate kind); login gates are never acknowledged; a
// gate that reappears after its one ack waits for a human.
func TestStepLawGateAutoAck(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))

	trust := runObservation{
		Kind:   obsCaptured,
		Output: "do you trust the contents of this directory\npress enter to continue",
		Signal: agentSignal{Gate: "trust", GateAutoAck: true},
	}

	// Tick 1: unconfirmed — no verdict, no ack (the same L10a streak gates both).
	core := runCore{WasAlive: true}
	var effects []runEffect
	core, effects = stepRun(view, core, trust, now)
	for _, e := range effects {
		if e.Kind == effectSendAgentNotice {
			t.Fatal("ack before the gate reading confirms (L-G1/L10a violation)")
		}
	}

	// Tick 2: confirmed — waiting(gate_trust) AND exactly one ack effect.
	core, effects = stepRun(view, core, trust, now)
	if status, reason, ok := statusReasonEffectOf(effects); !ok || status != model.StatusWaiting || reason != "gate_trust" {
		t.Fatalf("confirmed trust gate = (%v %q %t), want waiting(gate_trust)", status, reason, ok)
	}
	acks := 0
	for _, e := range effects {
		if e.Kind == effectSendAgentNotice {
			acks++
			if e.GateKind != "trust" {
				t.Fatalf("ack effect GateKind = %q, want trust", e.GateKind)
			}
		}
	}
	if acks != 1 {
		t.Fatalf("confirmed AutoAck gate must emit exactly one ack effect, got %d", acks)
	}

	// After the gate_ack note event folds in, the reappearing gate stays
	// waiting for a human — no second ack, ever (loop-impossibility).
	acked := trust
	acked.GateAckSent = true
	core, effects = stepRun(view, core, acked, now)
	for _, e := range effects {
		if e.Kind == effectSendAgentNotice {
			t.Fatal("second ack for the same (run, gate kind) (L-G1 violation)")
		}
	}

	// Login gates: never acknowledged, regardless of confirmation.
	login := runObservation{
		Kind:   obsCaptured,
		Output: "sign in with chatgpt\npress enter to continue",
		Signal: agentSignal{Gate: "login", GateAutoAck: false},
	}
	core = runCore{WasAlive: true}
	core, _ = stepRun(view, core, login, now)
	core, effects = stepRun(view, core, login, now)
	for _, e := range effects {
		if e.Kind == effectSendAgentNotice {
			t.Fatal("login gate acknowledged — credentials belong to humans (L-G1 violation)")
		}
	}
	if status, reason, ok := statusReasonEffectOf(effects); !ok || status != model.StatusWaiting || reason != "gate_login" {
		t.Fatalf("login gate must still conclude waiting(gate_login), got (%v %q %t)", status, reason, ok)
	}
}
