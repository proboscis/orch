package daemon

// Law tests for the pure transition core (step.go). Each law is checked by
// enumerating or generating observation inputs and asserting an invariant of
// the emitted effects — no stores, sessions, or networks involved. The laws
// are documented in docs/design/run-state-machine.md §5.

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/model"
)

var stepTestStatuses = []model.Status{
	model.StatusQueued,
	model.StatusBooting,
	model.StatusRunning,
	model.StatusWaiting,
	model.StatusRateLimited,
	model.StatusPROpen,
	model.StatusUnknown,
}

var stepTestTerminalStatuses = []model.Status{
	model.StatusDone,
	model.StatusFailed,
	model.StatusCanceled,
}

var stepTestAgents = []string{"codex", "claude", "opencode"}

func stepTestView(status model.Status, agent string, startedAt time.Time) runView {
	return runView{
		Status:    status,
		Agent:     agent,
		Branch:    "feature/x",
		StartedAt: startedAt,
		IssueID:   "issue-1",
		RunID:     "run-1",
	}
}

// stepTestObservations enumerates one representative observation per kind ×
// meaningful variant.
func stepTestObservations(now time.Time) []runObservation {
	outcomes := []prOutcome{prOutcomeUnknown, prOutcomeOpen, prOutcomeMerged, prOutcomeClosed}
	var obs []runObservation
	for _, o := range outcomes {
		obs = append(obs, runObservation{Kind: obsPRState, PROutcome: o, PRURL: "https://github.com/o/r/pull/1"})
	}
	obs = append(obs, runObservation{Kind: obsSessionAlive})
	for _, remote := range []bool{false, true} {
		obs = append(obs, runObservation{Kind: obsSessionGone, Remote: remote})
		obs = append(obs,
			runObservation{Kind: obsGitEvidence, Remote: remote},
			runObservation{Kind: obsGitEvidence, Remote: remote, Evidence: gitEvidence{
				RepoRootFound:   true,
				BranchPRURL:     "https://github.com/o/r/pull/1",
				BranchPROutcome: prOutcomeMerged,
			}},
			runObservation{Kind: obsGitEvidence, Remote: remote, Evidence: gitEvidence{
				RepoRootFound: true,
				AheadKnown:    true,
				AheadCount:    2,
			}},
		)
	}
	obs = append(obs,
		runObservation{Kind: obsCaptured, Output: "working (esc to interrupt)"},
		runObservation{Kind: obsCaptured, Output: "❯ ", Signal: agentSignal{PromptShowing: true}},
		runObservation{Kind: obsCaptured, Output: "done", Signal: agentSignal{Completed: true}},
		runObservation{Kind: obsCaptured, Output: "exited", Signal: agentSignal{Exited: true}},
		runObservation{Kind: obsCaptured, Output: "busy", Signal: agentSignal{Resolved: true, Status: model.StatusRunning}},
		runObservation{Kind: obsCaptured, Output: "idle", Signal: agentSignal{Resolved: true, Status: model.StatusWaiting}},
	)
	obs = append(obs,
		runObservation{Kind: obsLaunchProgress, Launch: launchReached(stageRunCreated)},
		runObservation{Kind: obsLaunchProgress, Launch: launchReached(stageLaunchReady)},
		runObservation{Kind: obsLaunchProgress, Launch: launchReached(stageWorkspaceOnly)},
		runObservation{Kind: obsLaunchProgress, Launch: launchReached(stageAgentStarted)},
		runObservation{Kind: obsLaunchProgress, Launch: launchFailed("session", fmt.Errorf("session create failed"))},
	)
	return obs
}

func stepTestCores() []runCore {
	var cores []runCore
	for _, wasAlive := range []bool{false, true} {
		for _, dead := range []int{0, 1, 2, 3, 4} {
			for _, streak := range []int{0, 1, 2} {
				cores = append(cores, runCore{
					WasAlive:       wasAlive,
					DeadCheckCount: dead,
					PromptStreak:   streak,
				})
			}
		}
	}
	return cores
}

// foldStatus applies the setStatus effects of one step to the view, the way
// a committed store write would before the next observation.
func foldStatus(view runView, effects []runEffect) runView {
	if status, ok := statusEffectOf(effects); ok {
		view.Status = status
	}
	return view
}

// L4 terminality: from a terminal status, no observation produces any effect.
func TestStepLawTerminality(t *testing.T) {
	now := time.Now()
	for _, status := range stepTestTerminalStatuses {
		for _, agent := range stepTestAgents {
			for _, core := range stepTestCores() {
				for _, obs := range stepTestObservations(now) {
					view := stepTestView(status, agent, now.Add(-time.Hour))
					gotCore, effects := stepRun(view, core, obs, now)
					if len(effects) != 0 {
						t.Fatalf("terminal %s: obs kind %d produced effects %+v", status, obs.Kind, effects)
					}
					if !reflect.DeepEqual(gotCore, core) {
						t.Fatalf("terminal %s: obs kind %d mutated core %+v -> %+v", status, obs.Kind, core, gotCore)
					}
				}
			}
		}
	}
}

// Purity/determinism: identical inputs always produce identical outputs.
func TestStepLawDeterminism(t *testing.T) {
	now := time.Now()
	for _, status := range stepTestStatuses {
		for _, agent := range stepTestAgents {
			for _, core := range stepTestCores() {
				for _, obs := range stepTestObservations(now) {
					view := stepTestView(status, agent, now.Add(-time.Hour))
					core1, effects1 := stepRun(view, core, obs, now)
					core2, effects2 := stepRun(view, core, obs, now)
					if !reflect.DeepEqual(core1, core2) || !reflect.DeepEqual(effects1, effects2) {
						t.Fatalf("non-deterministic step: status=%s agent=%s obs=%d", status, agent, obs.Kind)
					}
				}
			}
		}
	}
}

// isFactObservation distinguishes snapshot observations of world state
// (idempotent: re-observing the same fact must not change the outcome) from
// stream observations sampled once per tick (capture, gone), whose
// repetition legitimately advances debounce/dead counters.
func isFactObservation(obs runObservation) bool {
	switch obs.Kind {
	case obsPRState, obsGitEvidence, obsSessionAlive, obsLaunchProgress:
		return true
	default:
		return false
	}
}

// L1a idempotency (fact observations): once a transition is committed
// (view.Status = target), re-applying the same fact to the resulting core
// emits no status effect with a different target. Together with the
// executor-level same-status no-op this is what makes duplicate status
// events impossible.
func TestStepLawIdempotentConvergence(t *testing.T) {
	now := time.Now()
	for _, status := range stepTestStatuses {
		for _, agent := range stepTestAgents {
			for _, core := range stepTestCores() {
				for _, obs := range stepTestObservations(now) {
					if !isFactObservation(obs) {
						continue
					}
					view := stepTestView(status, agent, now.Add(-time.Hour))
					core1, effects1 := stepRun(view, core, obs, now)
					target1, transitioned := statusEffectOf(effects1)
					if !transitioned {
						continue
					}
					view2 := foldStatus(view, effects1)
					_, effects2 := stepRun(view2, core1, obs, now)
					if target2, ok := statusEffectOf(effects2); ok && target2 != target1 {
						t.Fatalf("divergent re-application: status=%s agent=%s obs=%d: %s then %s",
							status, agent, obs.Kind, target1, target2)
					}
				}
			}
		}
	}
}

// L1b fixed point (stream observations): repeating the same tick-sampled
// observation forever reaches a fixed point — after boundedly many
// applications the step emits no further status effects. Debounce and dead
// counters may advance, but they must converge, never oscillate.
func TestStepLawStreamFixedPoint(t *testing.T) {
	now := time.Now()
	const repetitions = 12 // > deadChecksBeforeFailed and > waitingPromptStreakThreshold

	for _, status := range stepTestStatuses {
		for _, agent := range stepTestAgents {
			for _, core := range stepTestCores() {
				for _, obs := range stepTestObservations(now) {
					if isFactObservation(obs) {
						continue
					}
					view := stepTestView(status, agent, now.Add(-time.Hour))
					c := core
					lastTransition := -1
					for i := 0; i < repetitions; i++ {
						var effects []runEffect
						c, effects = stepRun(view, c, obs, now)
						if _, ok := statusEffectOf(effects); ok {
							lastTransition = i
						}
						view = foldStatus(view, effects)
					}
					// The final applications must be quiet: half the window
					// is far beyond every counter threshold.
					if lastTransition >= repetitions/2 {
						t.Fatalf("no fixed point: status=%s agent=%s obs=%d still transitioning at %d",
							status, agent, obs.Kind, lastTransition)
					}
				}
			}
		}
	}
}

// L2 order-independent convergence: a dead session and a merged/closed PR
// must reach the same terminal status regardless of which channel observes
// first. The PR-state observation and the gone→evidence path are fed in both
// orders; the folded final status must agree.
func TestStepLawOrderIndependentConvergence(t *testing.T) {
	now := time.Now()
	cases := []struct {
		outcome prOutcome
		want    model.Status
	}{
		{prOutcomeMerged, model.StatusDone},
		{prOutcomeClosed, model.StatusCanceled},
	}

	prURL := "https://github.com/o/r/pull/9"
	for _, tc := range cases {
		for _, remote := range []bool{false, true} {
			prObs := runObservation{Kind: obsPRState, PROutcome: tc.outcome, PRURL: prURL}
			evidenceObs := runObservation{Kind: obsGitEvidence, Remote: remote, Evidence: gitEvidence{
				RepoRootFound:   true,
				BranchPRURL:     prURL,
				BranchPROutcome: tc.outcome,
			}}
			goneObs := runObservation{Kind: obsSessionGone, Remote: remote}

			orderings := [][]runObservation{
				{prObs, goneObs, goneObs, goneObs, evidenceObs},
				{goneObs, goneObs, goneObs, evidenceObs, prObs},
				{goneObs, prObs, goneObs, goneObs, evidenceObs},
			}

			var finals []model.Status
			for _, seq := range orderings {
				view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
				core := runCore{WasAlive: true}
				for _, obs := range seq {
					var effects []runEffect
					core, effects = stepRun(view, core, obs, now)
					view = foldStatus(view, effects)
				}
				finals = append(finals, view.Status)
			}
			for _, got := range finals {
				if got != tc.want {
					t.Fatalf("outcome=%s remote=%t: finals=%v, want all %s", tc.outcome, remote, finals, tc.want)
				}
			}
		}
	}
}

// L3 grace: a never-alive run inside the boot grace window never receives an
// unknown/failed verdict, whatever sequence of gone/evidence observations
// arrives on either channel.
func TestStepLawNeverAliveGrace(t *testing.T) {
	now := time.Now()
	rng := rand.New(rand.NewSource(1))

	for trial := 0; trial < 200; trial++ {
		remote := rng.Intn(2) == 0
		agent := stepTestAgents[rng.Intn(len(stepTestAgents))]
		view := stepTestView(model.StatusBooting, agent, now.Add(-time.Minute)) // inside 3m grace
		core := runCore{}                                                       // never alive

		for i := 0; i < 10; i++ {
			var obs runObservation
			if rng.Intn(2) == 0 {
				obs = runObservation{Kind: obsSessionGone, Remote: remote}
			} else {
				obs = runObservation{Kind: obsGitEvidence, Remote: remote, Evidence: gitEvidence{RepoRootFound: true}}
			}
			var effects []runEffect
			core, effects = stepRun(view, core, obs, now)
			if status, ok := statusEffectOf(effects); ok {
				if status == model.StatusUnknown || status == model.StatusFailed {
					t.Fatalf("trial %d: verdict %s within boot grace (remote=%t agent=%s obs=%d)",
						trial, status, remote, agent, obs.Kind)
				}
			}
			view = foldStatus(view, effects)
		}
	}
}

// L3' complement (run-state-machine.md §7 D-C3): after the grace expires,
// BOTH channels conclude unknown for a never-alive run with no git
// evidence — v1's pinned local keep-waiting asymmetry is resolved.
func TestStepNeverAliveVerdictAfterGrace(t *testing.T) {
	now := time.Now()
	startedAt := now.Add(-2 * neverAliveVerdictGrace)

	run := func(remote bool) (model.Status, bool) {
		view := stepTestView(model.StatusBooting, "codex", startedAt)
		core := runCore{}
		for i := 0; i < deadChecksBeforeFailed; i++ {
			var effects []runEffect
			core, effects = stepRun(view, core, runObservation{Kind: obsSessionGone, Remote: remote}, now)
			view = foldStatus(view, effects)
			if effectsContain(effects, effectGatherGitEvidence) {
				core, effects = stepRun(view, core, runObservation{Kind: obsGitEvidence, Remote: remote, Evidence: gitEvidence{RepoRootFound: true}}, now)
				view = foldStatus(view, effects)
			}
		}
		return view.Status, view.Status != model.StatusBooting
	}

	if status, transitioned := run(true); !transitioned || status != model.StatusUnknown {
		t.Fatalf("remote never-alive after grace: status=%s, want unknown", status)
	}
	if status, transitioned := run(false); !transitioned || status != model.StatusUnknown {
		t.Fatalf("local never-alive after grace: status=%s, want unknown", status)
	}
}

// Status-reason payload (run-state-machine.md §5 "Status reasons"): every
// unknown verdict emitted by step() carries a machine-readable reason.
func TestStepUnknownVerdictsCarryReason(t *testing.T) {
	now := time.Now()
	startedAt := now.Add(-2 * neverAliveVerdictGrace)

	unknownReason := func(view runView, core runCore, remote bool) string {
		t.Helper()
		for i := 0; i < deadChecksBeforeFailed+1; i++ {
			var effects []runEffect
			core, effects = stepRun(view, core, runObservation{Kind: obsSessionGone, Remote: remote}, now)
			if reason, ok := unknownReasonOf(effects); ok {
				return reason
			}
			view = foldStatus(view, effects)
			if effectsContain(effects, effectGatherGitEvidence) {
				core, effects = stepRun(view, core, runObservation{Kind: obsGitEvidence, Remote: remote, Evidence: gitEvidence{RepoRootFound: true}}, now)
				if reason, ok := unknownReasonOf(effects); ok {
					return reason
				}
				view = foldStatus(view, effects)
			}
		}
		t.Fatalf("no unknown verdict emitted (remote=%t agent=%s)", remote, view.Agent)
		return ""
	}

	// L3' never-alive arms, both planes.
	if got := unknownReason(stepTestView(model.StatusBooting, "codex", startedAt), runCore{}, true); got != model.StatusReasonNeverAlive {
		t.Fatalf("remote never-alive reason = %q, want %q", got, model.StatusReasonNeverAlive)
	}
	if got := unknownReason(stepTestView(model.StatusBooting, "codex", startedAt), runCore{}, false); got != model.StatusReasonNeverAlive {
		t.Fatalf("local never-alive reason = %q, want %q", got, model.StatusReasonNeverAlive)
	}

	// opencode session-lost arm (was alive, local plane).
	if got := unknownReason(stepTestView(model.StatusRunning, "opencode", startedAt), runCore{WasAlive: true}, false); got != model.StatusReasonSessionLost {
		t.Fatalf("opencode session-lost reason = %q, want %q", got, model.StatusReasonSessionLost)
	}

	// Capture verdict: agent exited, shell prompt showing.
	status, reason := agentVerdict(stepTestView(model.StatusRunning, "codex", startedAt), agentSignal{Exited: true}, false, false)
	if status != model.StatusUnknown || reason != model.StatusReasonAgentExited {
		t.Fatalf("exited verdict = (%s, %q), want (unknown, %q)", status, reason, model.StatusReasonAgentExited)
	}
}

// unknownReasonOf returns the Reason of the first effectSetStatus carrying
// StatusUnknown, if any.
func unknownReasonOf(effects []runEffect) (string, bool) {
	for _, e := range effects {
		if e.Kind == effectSetStatus && e.Status == model.StatusUnknown {
			return e.Reason, true
		}
	}
	return "", false
}

// D-C1 (run-state-machine.md §7): the fold-derivable runCore fields are
// reconstructed from the event log at monitor registration; the ephemeral
// counters start at zero.
func TestInitialRunCoreDerivation(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name      string
		events    []*model.Event
		wantAlive bool
		wantPRRec bool
	}{
		{"empty log", nil, false, false},
		{"queued and booting only", []*model.Event{
			model.NewStatusEvent(model.StatusQueued),
			model.NewStatusEvent(model.StatusBooting),
		}, false, false},
		{"reached running", []*model.Event{
			model.NewStatusEvent(model.StatusQueued),
			model.NewStatusEvent(model.StatusRunning),
		}, true, false},
		{"pr artifact recorded", []*model.Event{
			model.NewStatusEvent(model.StatusRunning),
			model.NewArtifactEvent("pr", map[string]string{"url": "https://github.com/o/r/pull/1"}),
		}, true, true},
		{"failed while never alive", []*model.Event{
			model.NewStatusEvent(model.StatusQueued),
			model.NewStatusEvent(model.StatusFailed),
		}, false, false},
	}
	for _, tc := range cases {
		run := &model.Run{Events: tc.events}
		core := initialRunCore(run, now)
		if core.WasAlive != tc.wantAlive || core.PRRecorded != tc.wantPRRec {
			t.Fatalf("%s: WasAlive=%t PRRecorded=%t, want %t/%t",
				tc.name, core.WasAlive, core.PRRecorded, tc.wantAlive, tc.wantPRRec)
		}
		if core.DeadCheckCount != 0 || core.PromptStreak != 0 || core.OutputHash != "" {
			t.Fatalf("%s: ephemeral counters must start at zero", tc.name)
		}
		if !core.LastOutputAt.Equal(now) {
			t.Fatalf("%s: LastOutputAt = %v, want %v", tc.name, core.LastOutputAt, now)
		}
	}
}

// L7 restart transparency (the I2 fix): after a daemon restart, a run whose
// log shows it was alive folds WasAlive back, so a gone session is judged a
// real death (failed) rather than a never-alive boot (unknown).
func TestInitialRunCoreRestoresLivenessAcrossRestart(t *testing.T) {
	now := time.Now()
	startedAt := now.Add(-2 * neverAliveVerdictGrace)

	run := &model.Run{Events: []*model.Event{
		model.NewStatusEvent(model.StatusQueued),
		model.NewStatusEvent(model.StatusRunning),
	}}
	core := initialRunCore(run, now)

	view := stepTestView(model.StatusRunning, "codex", startedAt)
	var effects []runEffect
	for i := 0; i < deadChecksBeforeFailed; i++ {
		core, effects = stepRun(view, core, runObservation{Kind: obsSessionGone}, now)
		view = foldStatus(view, effects)
	}
	if !effectsContain(effects, effectGatherGitEvidence) {
		t.Fatalf("expected evidence request at the dead-check threshold")
	}
	_, effects = stepRun(view, core, runObservation{Kind: obsGitEvidence, Evidence: gitEvidence{RepoRootFound: true}}, now)
	view = foldStatus(view, effects)
	if view.Status != model.StatusFailed {
		t.Fatalf("was-alive run after restart judged %s, want failed", view.Status)
	}
}

// L5 verdict requires evidence: the dead-session path emits no status
// directly — obsSessionGone may only request git evidence, and only at the
// dead-check threshold.
func TestStepLawVerdictRequiresEvidence(t *testing.T) {
	now := time.Now()
	for _, status := range stepTestStatuses {
		for _, agent := range stepTestAgents {
			for _, wasAlive := range []bool{false, true} {
				for _, remote := range []bool{false, true} {
					for dead := 0; dead < deadChecksBeforeFailed+2; dead++ {
						view := stepTestView(status, agent, now.Add(-time.Hour))
						core := runCore{WasAlive: wasAlive, DeadCheckCount: dead}
						gotCore, effects := stepRun(view, core, runObservation{Kind: obsSessionGone, Remote: remote}, now)

						if _, ok := statusEffectOf(effects); ok {
							t.Fatalf("obsSessionGone emitted a status directly (status=%s dead=%d)", status, dead)
						}
						wantEvidence := gotCore.DeadCheckCount >= deadChecksBeforeFailed
						if effectsContain(effects, effectGatherGitEvidence) != wantEvidence {
							t.Fatalf("evidence request mismatch: dead=%d (after increment %d), want %t",
								dead, gotCore.DeadCheckCount, wantEvidence)
						}
					}
				}
			}
		}
	}
}

// L6 debounce: a waiting verdict via prompt detection requires the prompt to
// persist for waitingPromptStreakThreshold consecutive captures, and any
// non-prompt capture resets the streak.
func TestStepLawPromptDebounce(t *testing.T) {
	now := time.Now()
	prompt := runObservation{Kind: obsCaptured, Output: "❯ ready", Signal: agentSignal{PromptShowing: true}}
	busy := runObservation{Kind: obsCaptured, Output: "working...", Signal: agentSignal{}}

	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true}

	// First prompt observation: no waiting yet (streak 1 < 2). The output
	// change alone keeps the run running.
	var effects []runEffect
	core, effects = stepRun(view, core, prompt, now)
	if status, ok := statusEffectOf(effects); ok && status == model.StatusWaiting {
		t.Fatalf("waiting after a single prompt observation (streak=%d)", core.PromptStreak)
	}
	view = foldStatus(view, effects)

	// Second consecutive prompt: waiting.
	core, effects = stepRun(view, core, prompt, now)
	if status, ok := statusEffectOf(effects); !ok || status != model.StatusWaiting {
		t.Fatalf("expected waiting after %d consecutive prompts, effects=%+v", waitingPromptStreakThreshold, effects)
	}
	view = foldStatus(view, effects)

	// A busy capture resets the streak; the next single prompt must not flip
	// straight back to waiting.
	core, effects = stepRun(view, core, busy, now)
	view = foldStatus(view, effects)
	if core.PromptStreak != 0 {
		t.Fatalf("busy capture did not reset streak: %d", core.PromptStreak)
	}
	core, effects = stepRun(view, core, prompt, now)
	if status, ok := statusEffectOf(effects); ok && status == model.StatusWaiting {
		t.Fatalf("waiting after streak reset + one prompt (streak=%d)", core.PromptStreak)
	}
}

// Random-walk convergence smoke: arbitrary observation sequences never
// produce a transition out of a terminal status and never panic. This is the
// seed of the deterministic simulator described in run-state-machine.md.
func TestStepRandomWalkTerminalAbsorption(t *testing.T) {
	now := time.Now()
	rng := rand.New(rand.NewSource(42))
	allObs := stepTestObservations(now)

	for trial := 0; trial < 500; trial++ {
		agent := stepTestAgents[rng.Intn(len(stepTestAgents))]
		view := stepTestView(stepTestStatuses[rng.Intn(len(stepTestStatuses))], agent, now.Add(-time.Duration(rng.Intn(600))*time.Second))
		core := runCore{}
		terminalAt := -1

		for i := 0; i < 30; i++ {
			obs := allObs[rng.Intn(len(allObs))]
			var effects []runEffect
			core, effects = stepRun(view, core, obs, now)
			view = foldStatus(view, effects)

			if terminalAt >= 0 && len(effects) != 0 {
				t.Fatalf("trial %d: effects after terminal absorption at step %d", trial, i)
			}
			if terminalAt < 0 && view.Status.IsTerminal() {
				terminalAt = i
			}
		}
	}
}

// O8 launch progress: stages map to exactly the ladder statuses, and
// re-affirming the current status is silent — the implicit initial `queued`
// (the fold default) is never duplicated as an event
// (run-state-machine.md §7 D-B1).
func TestStepLaunchProgressStageMapping(t *testing.T) {
	now := time.Now()
	cases := []struct {
		stage launchStage
		want  model.Status
	}{
		{stageRunCreated, model.StatusQueued},
		{stageLaunchReady, model.StatusBooting},
		{stageWorkspaceOnly, model.StatusRunning},
		{stageAgentStarted, model.StatusRunning},
	}
	for _, tc := range cases {
		// waiting stands in for any non-terminal, non-target status: the
		// reuse path relaunches waiting runs through the same ladder.
		view := stepTestView(model.StatusWaiting, "codex", now.Add(-time.Minute))
		obs := runObservation{Kind: obsLaunchProgress, Launch: launchReached(tc.stage)}
		_, effects := stepRun(view, runCore{}, obs, now)
		got, ok := statusEffectOf(effects)
		if !ok || got != tc.want {
			t.Fatalf("stage %d: status effect = %v (present=%t), want %s", tc.stage, got, ok, tc.want)
		}

		view.Status = tc.want
		_, effects = stepRun(view, runCore{}, obs, now)
		if len(effects) != 0 {
			t.Fatalf("stage %d at %s: re-affirmation produced effects %+v", tc.stage, tc.want, effects)
		}
	}
}

// O8 launch failure: the error artifact precedes the failed verdict, the
// verdict carries the machine-readable reason launch_<step>, and an unnamed
// step falls back to launch_bootstrap.
func TestStepLaunchFailureCarriesReason(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusBooting, "codex", now.Add(-time.Minute))

	obs := runObservation{Kind: obsLaunchProgress, Launch: launchFailed("session", fmt.Errorf("tmux exploded"))}
	_, effects := stepRun(view, runCore{}, obs, now)
	if len(effects) != 3 {
		t.Fatalf("expected [recordError, setStatus, log], got %+v", effects)
	}
	if effects[0].Kind != effectRecordError || effects[0].Msg != "tmux exploded" {
		t.Fatalf("first effect = %+v, want error artifact carrying the bootstrap error", effects[0])
	}
	if effects[1].Kind != effectSetStatus || effects[1].Status != model.StatusFailed || effects[1].Reason != "launch_session" {
		t.Fatalf("second effect = %+v, want failed with reason launch_session", effects[1])
	}
	if effects[2].Kind != effectLog {
		t.Fatalf("third effect = %+v, want log", effects[2])
	}

	// No detail → no artifact; no step name → the bootstrap fallback reason.
	_, effects = stepRun(view, runCore{}, runObservation{Kind: obsLaunchProgress, Launch: launchSignal{Failed: true}}, now)
	if len(effects) != 2 {
		t.Fatalf("expected [setStatus, log], got %+v", effects)
	}
	if effects[0].Kind != effectSetStatus || effects[0].Status != model.StatusFailed || effects[0].Reason != "launch_bootstrap" {
		t.Fatalf("first effect = %+v, want failed with reason launch_bootstrap", effects[0])
	}
}
