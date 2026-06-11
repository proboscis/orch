package daemon

// Deterministic observation-script replay over the pure transition core
// (coupling-core roadmap Phase D1). A scenario is a scripted sequence of
// (clock advance, observation) ticks; the harness folds committed status
// effects back into the view the way updateStatus would, so a scenario
// asserts the exact sequence of transition *targets* a run would commit.
// Past incident classes are pinned here as replayable scripts.

import (
	"reflect"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

// simStep is one scripted tick: advance the simulated clock, then feed one
// observation to stepRun.
type simStep struct {
	advance time.Duration
	obs     runObservation
}

type simResult struct {
	targets []model.Status // committed status *changes*, in order
	final   model.Status
	core    runCore
}

// replayScript drives stepRun over a scripted observation sequence with a
// deterministic clock. Same-status effects are not recorded as targets,
// mirroring the same-status no-op in the updateStatus executor (W1).
func replayScript(view runView, core runCore, t0 time.Time, script []simStep) simResult {
	now := t0
	var targets []model.Status
	for _, s := range script {
		now = now.Add(s.advance)
		var effects []runEffect
		core, effects = stepRun(view, core, s.obs, now)
		if status, ok := statusEffectOf(effects); ok && status != view.Status {
			targets = append(targets, status)
		}
		view = foldStatus(view, effects)
	}
	return simResult{targets: targets, final: view.Status, core: core}
}

func simCaptured(output string, sig agentSignal) runObservation {
	return runObservation{Kind: obsCaptured, Output: output, Signal: sig}
}

func simAssert(t *testing.T, res simResult, wantTargets []model.Status, wantFinal model.Status) {
	t.Helper()
	if !reflect.DeepEqual(res.targets, wantTargets) {
		t.Fatalf("transition targets = %v, want %v", res.targets, wantTargets)
	}
	if res.final != wantFinal {
		t.Fatalf("final status = %s, want %s", res.final, wantFinal)
	}
}

var simT0 = time.Unix(1765000000, 0)

// Incident class: duplicate terminal events (the 5,830-duplicate-event run).
// Re-observing the merged-PR fact must commit exactly one transition; after
// done, terminality (L4) absorbs everything.
func TestSimMergedPRCommitsDoneExactlyOnce(t *testing.T) {
	view := stepTestView(model.StatusRunning, "codex", simT0.Add(-time.Hour))
	merged := runObservation{Kind: obsPRState, PROutcome: prOutcomeMerged, PRURL: "https://github.com/o/r/pull/1"}
	var script []simStep
	for i := 0; i < 5; i++ {
		script = append(script, simStep{advance: 15 * time.Second, obs: merged})
	}
	res := replayScript(view, runCore{WasAlive: true}, simT0, script)
	simAssert(t, res, []model.Status{model.StatusDone}, model.StatusDone)
}

// L6 debounce: waiting requires two consecutive prompt observations; a busy
// capture flips the run back to running and resets the streak, so a single
// later prompt does not re-enter waiting.
func TestSimPromptDebounceAndStreakReset(t *testing.T) {
	view := stepTestView(model.StatusRunning, "codex", simT0.Add(-time.Hour))
	prompt := simCaptured("❯ ", agentSignal{PromptShowing: true})
	busy := simCaptured("working (esc to interrupt)", agentSignal{})
	script := []simStep{
		{15 * time.Second, prompt}, // streak 1 — no transition yet
		{15 * time.Second, prompt}, // streak 2 — waiting
		{15 * time.Second, busy},   // output changed — running, streak reset
		{15 * time.Second, prompt}, // streak 1 again — still running
	}
	res := replayScript(view, runCore{WasAlive: true}, simT0, script)
	simAssert(t, res,
		[]model.Status{model.StatusWaiting, model.StatusRunning},
		model.StatusRunning)
	if res.core.PromptStreak != 1 {
		t.Fatalf("PromptStreak = %d, want 1", res.core.PromptStreak)
	}
}

// Agent completion commits done once and is then terminally absorbed, even
// if the completion banner keeps being captured.
func TestSimCompletionVerdictTerminallyAbsorbed(t *testing.T) {
	view := stepTestView(model.StatusRunning, "codex", simT0.Add(-time.Hour))
	completed := simCaptured("task complete", agentSignal{Completed: true})
	script := []simStep{
		{15 * time.Second, runObservation{Kind: obsSessionAlive}},
		{15 * time.Second, completed},
		{15 * time.Second, completed},
		{15 * time.Second, completed},
	}
	res := replayScript(view, runCore{}, simT0, script)
	simAssert(t, res, []model.Status{model.StatusDone}, model.StatusDone)
}

// API-limited verdict enters rate_limited; the next busy capture (changed
// output) resumes running.
func TestSimRateLimitedThenResumes(t *testing.T) {
	view := stepTestView(model.StatusRunning, "codex", simT0.Add(-time.Hour))
	limited := simCaptured("usage limit reached", agentSignal{APILimited: true})
	busy := simCaptured("working (esc to interrupt)", agentSignal{})
	script := []simStep{
		{15 * time.Second, limited},
		{15 * time.Second, busy},
	}
	res := replayScript(view, runCore{WasAlive: true}, simT0, script)
	simAssert(t, res,
		[]model.Status{model.StatusRateLimited, model.StatusRunning},
		model.StatusRunning)
}

// L3 grace for a never-alive remote run: dead checks plus empty git evidence
// produce no verdict inside neverAliveVerdictGrace; once the grace has
// passed, the same evidence concludes unknown.
func TestSimNeverAliveRemoteGraceThenUnknown(t *testing.T) {
	view := stepTestView(model.StatusBooting, "codex", simT0)
	gone := runObservation{Kind: obsSessionGone, Remote: true}
	evidence := runObservation{Kind: obsGitEvidence, Remote: true}
	script := []simStep{
		{15 * time.Second, gone},
		{15 * time.Second, gone},
		{15 * time.Second, gone},     // threshold reached — evidence requested
		{15 * time.Second, evidence}, // within 3min grace: no verdict
		{3 * time.Hour, gone},        // long past grace
		{15 * time.Second, evidence}, // same evidence now concludes unknown
	}
	res := replayScript(view, runCore{}, simT0, script)
	simAssert(t, res, []model.Status{model.StatusUnknown}, model.StatusUnknown)
}
