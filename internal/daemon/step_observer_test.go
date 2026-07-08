package daemon

// Law tests for observer attestation on death verdicts
// (run-state-machine.md §10): L11a streak continuity, L11b attestation,
// L11c unverified fallback, L7' verdict-strength monotonicity, and the I9
// invariant, checked over the pure transition core.

import (
	"testing"
	"time"

	"github.com/proboscis/orch/internal/model"
)

func goneFrom(observer, class string, remote bool) runObservation {
	return runObservation{Kind: obsSessionGone, Remote: remote, Observer: observer, GoneClass: class}
}

// runGoneToVerdict drives gone observations from one observer until the
// evidence request fires, then feeds empty git evidence and returns the
// resulting view.
func runGoneToVerdict(t *testing.T, view runView, core runCore, obs runObservation) (runView, runCore) {
	t.Helper()
	var effects []runEffect
	for i := 0; i < deadChecksBeforeFailed+1; i++ {
		core, effects = stepRun(view, core, obs, time.Now())
		view = foldStatus(view, effects)
		if effectsContain(effects, effectGatherGitEvidence) {
			core, effects = stepRun(view, core, runObservation{Kind: obsGitEvidence, Remote: obs.Remote, Evidence: gitEvidence{RepoRootFound: true}}, time.Now())
			view = foldStatus(view, effects)
			return view, core
		}
	}
	t.Fatalf("no evidence request after %d gone observations", deadChecksBeforeFailed+1)
	return view, core
}

// L11a: DeadCheckCount accumulates only while the observation's ObserverID
// equals DeadCheckObserver; a gone observation from a different observer
// restarts the streak at 1 and re-owns it.
func TestStepLawObserverStreakContinuity(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true, AliveObserver: "obs-a"}

	core, _ = stepRun(view, core, goneFrom("obs-a", goneClassSessionAbsent, true), now)
	core, _ = stepRun(view, core, goneFrom("obs-a", goneClassSessionAbsent, true), now)
	if core.DeadCheckCount != 2 || core.DeadCheckObserver != "obs-a" {
		t.Fatalf("streak = (%q, %d), want (obs-a, 2)", core.DeadCheckObserver, core.DeadCheckCount)
	}

	// A different observer's gone report is evidence only within its own
	// generation: the streak restarts at 1 under the new owner.
	core, _ = stepRun(view, core, goneFrom("obs-b", goneClassSessionAbsent, true), now)
	if core.DeadCheckCount != 1 || core.DeadCheckObserver != "obs-b" {
		t.Fatalf("streak after observer change = (%q, %d), want (obs-b, 1)", core.DeadCheckObserver, core.DeadCheckCount)
	}
}

// §10.5 counterexample 1 — incident replay: a restarted worker with a
// poisoned TMUX socket reports not-found with a fresh ObserverID while
// AliveObserver points at the dead old instance. The verdict is
// unknown(observer_unverified), never failed; the first successful capture
// re-attests and the run resumes normal inference. No terminal status is
// ever written.
func TestStepObserverIncidentReplay(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true, AliveObserver: "worker:mac:old"}

	view, core = runGoneToVerdict(t, view, core, goneFrom("worker:mac:new", goneClassSessionAbsent, true))
	if view.Status != model.StatusUnknown || view.StatusReason != model.StatusReasonObserverUnverified {
		t.Fatalf("poisoned-observer verdict = %s(%s), want unknown(observer_unverified)", view.Status, view.StatusReason)
	}
	if view.Status.IsTerminal() {
		t.Fatal("unattested verdict must not be terminal")
	}

	// Operator fixes the env: the next successful capture re-attests the
	// channel and normal inference resumes.
	var effects []runEffect
	core, effects = stepRun(view, core, runObservation{Kind: obsSessionAlive, Observer: "worker:mac:new"}, now)
	view = foldStatus(view, effects)
	if core.AliveObserver != "worker:mac:new" || core.DeadCheckCount != 0 {
		t.Fatalf("re-attestation failed: AliveObserver=%q DeadCheckCount=%d", core.AliveObserver, core.DeadCheckCount)
	}
	core, effects = stepRun(view, core, runObservation{Kind: obsCaptured, Output: "agent working again", Signal: agentSignal{}}, now)
	view = foldStatus(view, effects)
	if view.Status != model.StatusRunning {
		t.Fatalf("recovered run = %s, want running", view.Status)
	}
}

// §10.5 counterexample 3: the tmux server exits because the last session
// closed — the same long-lived observer reports server_absent. Still
// attested: the class is diagnostic, not the predicate. Verdict failed as
// today.
func TestStepObserverServerAbsentUnderSteadyObserverFails(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true, AliveObserver: "worker:mac:w1"}

	view, _ = runGoneToVerdict(t, view, core, goneFrom("worker:mac:w1", goneClassServerAbsent, true))
	if view.Status != model.StatusFailed {
		t.Fatalf("attested server_absent verdict = %s, want failed", view.Status)
	}
}

// §10.5 counterexample 6 — mixed-version fleet: a legacy channel reports
// gone without observer context (Observer ""). It can never attest, so the
// verdict is at most unknown.
func TestStepObserverLegacyChannelNeverFails(t *testing.T) {
	now := time.Now()
	for _, remote := range []bool{false, true} {
		view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
		core := runCore{WasAlive: true, AliveObserver: "local:n1"}
		view, _ = runGoneToVerdict(t, view, core, goneFrom("", goneClassUnclassified, remote))
		if view.Status != model.StatusUnknown || view.StatusReason != model.StatusReasonObserverUnverified {
			t.Fatalf("legacy-channel verdict (remote=%t) = %s(%s), want unknown(observer_unverified)", remote, view.Status, view.StatusReason)
		}
	}
}

// I9 property sweep: across every (alive-observer, gone-observer, plane,
// agent) combination, a failed verdict is only ever reached when the gone
// observer is the non-empty channel that last saw the run alive.
func TestStepLawI9NoFailedWithoutAttestation(t *testing.T) {
	now := time.Now()
	observers := []string{"", "obs-a", "obs-b"}
	for _, agentName := range stepTestAgents {
		for _, remote := range []bool{false, true} {
			for _, aliveObs := range observers {
				for _, goneObs := range observers {
					view := stepTestView(model.StatusRunning, agentName, now.Add(-time.Hour))
					core := runCore{WasAlive: true, AliveObserver: aliveObs}
					view, _ = runGoneToVerdict(t, view, core, goneFrom(goneObs, goneClassSessionAbsent, remote))

					attested := goneObs != "" && goneObs == aliveObs
					if view.Status == model.StatusFailed && !attested {
						t.Fatalf("I9 violation: failed written by unattested channel (agent=%s remote=%t alive=%q gone=%q)",
							agentName, remote, aliveObs, goneObs)
					}
					if attested && agentName != "opencode" && view.Status != model.StatusFailed {
						t.Fatalf("attested death did not conclude failed (agent=%s remote=%t): got %s", agentName, remote, view.Status)
					}
				}
			}
		}
	}
}
