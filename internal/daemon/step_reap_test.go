package daemon

import (
	"testing"
	"time"

	"github.com/proboscis/orch/internal/model"
)

// reapTestView builds a view whose current session generation is recorded as
// reaped (the LS3 latch): agent_session generation == session_reaped
// generation.
func reapTestView(status model.Status, agent string, startedAt time.Time, agentGen, reapedGen int) runView {
	view := stepTestView(status, agent, startedAt)
	view.AgentSessionGeneration = agentGen
	view.ReapedGeneration = reapedGen
	return view
}

// L-S3 (ADR-0005 LS3, run-state-machine.md §12): gone observations on a
// reaped session generation advance no dead-check streak, request no
// evidence, and emit no verdict — for any agent, any non-terminal status,
// local and remote channels, attested or not, repeated past every threshold.
func TestStepLawReapAbsorption(t *testing.T) {
	now := time.Now()
	for _, agent := range stepTestAgents {
		for _, remote := range []bool{false, true} {
			view := reapTestView(model.StatusWaiting, agent, now.Add(-time.Hour), 1, 1)
			// Attested observer: the exact channel that would otherwise be
			// allowed to reach a failed verdict (L11b) — the strongest case.
			core := runCore{WasAlive: true, AliveObserver: "obs-a"}
			obs := runObservation{Kind: obsSessionGone, Remote: remote, Observer: "obs-a", GoneClass: "session_absent"}

			for i := 0; i < deadChecksBeforeFailed*2; i++ {
				var effects []runEffect
				core, effects = stepRun(view, core, obs, now)
				if core.DeadCheckCount != 0 {
					t.Fatalf("agent=%s remote=%v: dead-check streak advanced to %d on a reaped generation", agent, remote, core.DeadCheckCount)
				}
				if effectsContain(effects, effectGatherGitEvidence) {
					t.Fatalf("agent=%s remote=%v: evidence requested for a reaped generation", agent, remote)
				}
				if status, ok := statusEffectOf(effects); ok {
					t.Fatalf("agent=%s remote=%v: verdict %q emitted for a reaped generation", agent, remote, status)
				}
			}
		}
	}
}

// A revive records a higher-generation agent_session artifact (ADR-0005 R5),
// dissolving the LS3 latch: gone observations resume normal dead-check
// accumulation and reach the evidence request at the threshold.
func TestStepLawReviveDissolvesReapLatch(t *testing.T) {
	now := time.Now()
	view := reapTestView(model.StatusRunning, "claude", now.Add(-time.Hour), 2, 1)
	core := runCore{WasAlive: true, AliveObserver: "obs-a"}
	obs := runObservation{Kind: obsSessionGone, Remote: false, Observer: "obs-a", GoneClass: "session_absent"}

	sawEvidenceRequest := false
	for i := 0; i < deadChecksBeforeFailed; i++ {
		var effects []runEffect
		core, effects = stepRun(view, core, obs, now)
		if effectsContain(effects, effectGatherGitEvidence) {
			sawEvidenceRequest = true
		}
	}
	if core.DeadCheckCount != deadChecksBeforeFailed {
		t.Fatalf("revived run's dead-check streak = %d, want %d (latch must dissolve)", core.DeadCheckCount, deadChecksBeforeFailed)
	}
	if !sawEvidenceRequest {
		t.Fatalf("revived run never reached the evidence request after %d gone checks", deadChecksBeforeFailed)
	}
}

// Defensive arm: a reap note with no recorded agent_session identity
// (AgentSessionGeneration == 0) still absorbs. The reaper is forbidden from
// producing this state (R4 kills only identity-recorded runs), but if it
// appears the safe direction is delaying verdicts, never inventing them (L7'
// strength monotonicity).
func TestStepLawReapAbsorptionWithoutIdentity(t *testing.T) {
	now := time.Now()
	view := reapTestView(model.StatusRunning, "codex", now.Add(-time.Hour), 0, 1)
	core := runCore{WasAlive: true, AliveObserver: "obs-a"}
	obs := runObservation{Kind: obsSessionGone, Remote: false, Observer: "obs-a", GoneClass: "session_absent"}

	for i := 0; i < deadChecksBeforeFailed+1; i++ {
		var effects []runEffect
		core, effects = stepRun(view, core, obs, now)
		if core.DeadCheckCount != 0 || effectsContain(effects, effectGatherGitEvidence) {
			t.Fatalf("identity-less reaped run was not absorbed (count=%d)", core.DeadCheckCount)
		}
	}
}

// The latch itself: boundary semantics of sessionReaped.
func TestSessionReapedLatchBoundaries(t *testing.T) {
	cases := []struct {
		agentGen, reapedGen int
		want                bool
	}{
		{0, 0, false},  // no identity, no reap
		{1, 0, false},  // live identity, never reaped
		{1, 1, true},   // current generation reaped
		{2, 1, false},  // revived past the reap
		{1, 2, true},   // reap note ahead (reaper raced a fold) — absorb
		{0, 1, true},   // defensive arm
	}
	for _, c := range cases {
		view := runView{AgentSessionGeneration: c.agentGen, ReapedGeneration: c.reapedGen}
		if got := sessionReaped(view); got != c.want {
			t.Errorf("sessionReaped(agent=%d, reaped=%d) = %v, want %v", c.agentGen, c.reapedGen, got, c.want)
		}
	}
}
