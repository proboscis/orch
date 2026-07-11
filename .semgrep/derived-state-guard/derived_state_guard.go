//go:build semgrepfixture

// Semgrep test fixture for derived-state-guard rules (WL#2: RunState vs
// event log). Parsed by `semgrep test`, never compiled.
package fixture

// --- run-event-vocabulary-closed -------------------------------------------

const (
	// ok: run-event-vocabulary-closed
	EventTypeStatus EventType = "status"
	// ok: run-event-vocabulary-closed
	EventTypeArtifact EventType = "artifact"
	// ruleid: run-event-vocabulary-closed
	EventTypeMonitorCounter EventType = "monitor_counter"
)

// ruleid: run-event-vocabulary-closed
var snapshotEventType model.EventType = "run_core_snapshot"

func badMintEventTypeLiteral() {
	// ruleid: run-event-vocabulary-closed
	_ = model.EventType("dead_check_snapshot")
}

func badNewEventWithNovelLiteral(st Store, ref Ref) error {
	// ruleid: run-event-vocabulary-closed
	return st.AppendEvent(ref, model.NewEvent("counter", "dead_checks", nil))
}

func okNewEventInVocabulary() {
	// ok: run-event-vocabulary-closed
	_ = model.NewEvent("note", "observation", nil)
}

func okDynamicConversionIsTheW10LawBoundary(req AppendRequest) {
	// ok: run-event-vocabulary-closed
	_ = model.NewEvent(model.EventType(req.EventType), req.EventName, req.EventAttrs)
}

// --- no-monitor-counter-in-event --------------------------------------------

func badPersistDeadCheckCounter(st Store, ref Ref, core runCore) error {
	// ruleid: no-monitor-counter-in-event
	return st.AppendEvent(ref, model.NewArtifactEvent("monitor_state", map[string]string{"dead_checks": strconv.Itoa(core.DeadCheckCount)}))
}

func badPersistWasAliveMirror(core runCore) {
	// ruleid: no-monitor-counter-in-event
	_ = model.NewArtifactEvent("was_alive", map[string]string{"v": strconv.FormatBool(core.WasAlive)})
}

func badCounterSmuggledIntoReason(core runCore) {
	// ruleid: no-monitor-counter-in-event
	_ = model.NewStatusEventWithReason(model.StatusUnknown, fmt.Sprintf("streak_%d", core.ReadingStreak))
}

func badRefusalLatchPersistedAsEvent(st Store, ref Ref, core runCore) error {
	// The refusal latch is ephemeral diagnostic state; the once-only notice
	// bookkeeping is the daemon_notice note event built from the EFFECT
	// payload (observation-supplied), never from the core latch.
	// ruleid: no-monitor-counter-in-event
	return st.AppendEvent(ref, model.NewArtifactEvent("refused_pr", map[string]string{"url": core.PRMismatchURL}))
}

func okArtifactWithoutMonitorState(prURL string) {
	// ok: no-monitor-counter-in-event
	_ = model.NewArtifactEvent("pr", map[string]string{"url": prURL})
}

func okDiagnosticLoggingOfCounters(d *Daemon, core runCore) {
	// ok: no-monitor-counter-in-event
	d.logger.Printf("dead checks: %d", core.DeadCheckCount)
}

// --- run-core-hydration-surface ----------------------------------------------

func badSnapshotReadBack(state *RunState, snap persistedSnapshot) {
	// ruleid: run-core-hydration-surface
	state.WasAlive = snap.WasAlive
	// ruleid: run-core-hydration-surface
	state.DeadCheckCount = snap.DeadCheckCount
}

func badIncrementOutsidePolicy(state *RunState) {
	// ruleid: run-core-hydration-surface
	state.DeadCheckCount++
}

func badRefusalLatchOutsidePolicy(state *RunState, url, head string) {
	// The pr-attach refusal latch (L-PR2) is policy state: only stepRun may
	// set or clear it. A shell writing it is the same snapshot shape.
	// ruleid: run-core-hydration-surface
	state.PRMismatchURL = url
	// ruleid: run-core-hydration-surface
	state.PRMismatchHead = head
}

func badWholesaleCoreReplacement(state *RunState, loaded runCore) {
	// ruleid: run-core-hydration-surface
	state.runCore = loaded
}

func badHydrateCoreFromNonFoldSource(snap persistedSnapshot) runCore {
	// ruleid: run-core-hydration-surface
	return runCore{WasAlive: snap.WasAlive, DeadCheckCount: snap.DeadCheckCount}
}

func okZeroCoreForLaunchPlane(view runView, sig launchSignal) {
	// ok: run-core-hydration-surface
	_, _ = stepRun(view, runCore{}, runObservation{Kind: obsLaunchProgress, Launch: sig}, now())
}

func okReadingCoreFields(state *RunState) bool {
	// ok: run-core-hydration-surface
	return state.WasAlive && state.DeadCheckCount == 0
}

func okSchedulingStateIsShellOwned(state *RunState) {
	// Scheduling fields (when to observe) are mechanism, not policy — the
	// shell may write them freely (daemon.go RunState doc comment).
	// ok: run-core-hydration-surface
	state.LastCheckAt = now()
	// ok: run-core-hydration-surface
	state.CaptureFailureCount++
}
