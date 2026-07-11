package daemon

// This file is the pure decision core of the run monitor: the entire
// observation → status-transition policy lives in stepRun and nowhere else.
// Shells (monitorRun / monitorRemoteRun) only gather observations, execute
// the returned effects, and own scheduling concerns (capture pacing, lease
// backoff). The transition matrix encoded here is documented in
// docs/design/run-state-machine.md — keep both in sync.
//
// stepRun is a pure function: no I/O, no logging, no clock reads (time is an
// input). That is what makes the laws in step_test.go checkable by feeding
// observation sequences without sessions, stores, or networks.

import (
	"fmt"
	"time"

	"github.com/proboscis/orch/internal/model"
)

// runView is the read-only projection of a run that transition policy may
// depend on. It is derived from the store fold (run.DeriveState), never from
// in-memory monitor bookkeeping.
type runView struct {
	Status model.Status
	// StatusReason is the machine-readable reason on the run's current
	// status event ("" = none). The pair (Status, StatusReason) is the
	// no-op identity for status writes (§9.3 D-G1): a confirmed reading
	// change re-appends even when the status itself is unchanged.
	StatusReason string
	Agent        string
	Branch       string
	PRUrl        string
	StartedAt    time.Time
	IssueID      model.IssueID
	RunID        model.RunID
}

func runViewOf(run *model.Run) runView {
	return runView{
		Status:       run.Status,
		StatusReason: run.StatusReason(),
		Agent:        run.Agent,
		Branch:       run.Branch,
		PRUrl:        run.PRUrl,
		StartedAt:    run.StartedAt,
		IssueID:      run.IssueID,
		RunID:        run.RunID,
	}
}

// runCore holds the semantic monitor counters: the part of RunState that the
// transition policy reads and writes. Scheduling state (capture backoff,
// lease pacing) stays on RunState itself — when to observe is mechanism,
// what an observation means is policy.
type runCore struct {
	LastOutput   string
	LastOutputAt time.Time
	OutputHash   string
	// ReadingKind/ReadingStreak are the kind-tagged input-request streak
	// (run-state-machine.md §9.2): ReadingKind ∈ {"", "prompt",
	// "gate:<kind>"}; consecutive captures with the same reading advance the
	// streak, any different reading (including "" = busy/none) resets it.
	// Both are ephemeral by law (§7 D-C1): reset on restart, delay-only.
	ReadingKind    string
	ReadingStreak  int
	PRRecorded     bool
	WasAlive       bool
	DeadCheckCount int
	// AliveObserver is the ObserverID of the most recent successful
	// alive/capture observation ("" = none since restart); DeadCheckObserver
	// owns the current dead-check streak (run-state-machine.md §10.2). Both
	// are ephemeral by law (§7 D-C1): a restart may soften or delay a
	// would-be failed into unknown(observer_unverified) — never strengthen
	// (L7').
	AliveObserver     string
	DeadCheckObserver string
	// PRMismatchURL/PRMismatchHead latch the most recent pr-attach refusal
	// (L-PR2): set from a captured refusal observation, replaced by a newer
	// refusal, cleared when a verified PR records (PRRecorded). Ephemeral by
	// law (§7 D-C1): after a restart the pane recapture re-observes the URL
	// while it is still visible; once it scrolls away the diagnostic reason
	// quietly expires — a delayed/omitted reason, never a changed verdict.
	PRMismatchURL  string
	PRMismatchHead string
}

// initialRunCore derives the fold-derivable runCore fields from the run's
// event log (run-state-machine.md §7 D-C1 / L7). WasAlive folds from any
// alive-implying status event, PRRecorded from an existing pr artifact, so
// neither survives only in daemon memory (the I2/I5 fix). The remaining
// counters are ephemeral by law: they reset to zero and re-converge within
// bounded ticks (L1b), delaying verdicts/debounce but never changing them.
func initialRunCore(run *model.Run, now time.Time) runCore {
	core := runCore{LastOutputAt: now}
	for _, e := range run.Events {
		if e == nil {
			continue
		}
		switch e.Type {
		case model.EventTypeStatus:
			switch model.Status(e.Name) {
			case model.StatusRunning, model.StatusWaiting, model.StatusRateLimited, model.StatusDone:
				core.WasAlive = true
			}
		case model.EventTypeArtifact:
			if e.Name == "pr" {
				core.PRRecorded = true
			}
		}
	}
	return core
}

type obsKind int

const (
	// obsPRState reports the PR outcome for the run's branch/PR URL from the
	// PR cache. DiscoveredPRURL carries a URL found via branch lookup when
	// the run record has no PR URL yet.
	obsPRState obsKind = iota
	// obsSessionAlive reports one successful liveness observation (local
	// IsAlive or successful worker-lease capture).
	obsSessionAlive
	// obsSessionGone reports one failed liveness observation. Remote marks
	// which channel observed it: the no-evidence verdicts differ (see
	// docs/design/run-state-machine.md §3).
	obsSessionGone
	// obsCaptured carries a captured session output plus the agent-specific
	// signal precomputed by the gatherer.
	obsCaptured
	// obsGitEvidence is the response to effectGatherGitEvidence: the raw
	// PR/git facts needed for a dead-session verdict.
	obsGitEvidence
	// obsLaunchProgress reports a launch-ladder milestone or bootstrap
	// failure (O8). The bootstrap shells (socket.go start/continue handlers)
	// stay imperative, but every status transition they used to write
	// directly is decided here instead (run-state-machine.md §7 D-B1).
	obsLaunchProgress
)

// agentSignal is the agent-specific reading of a captured output, gathered
// impurely (opencode consults its server API) so stepRun stays pure.
type agentSignal struct {
	// Resolved means Status carries a fully resolved verdict ("" = none) and
	// the mux heuristics below are not used. Set for opencode, whose manager
	// resolves busy/idle/retry/gone itself.
	Resolved bool
	Status   model.Status

	// Mux-agent string heuristics on the captured pane.
	Exited        bool
	Completed     bool
	APILimited    bool
	Failed        bool
	PromptShowing bool
	// Gate is the interactive-gate kind detected on this capture ("" =
	// none), produced by AgentManager.DetectGate (O4e). The busy-marker veto
	// is applied gather-side, identically to PromptShowing. GateAutoAck
	// mirrors the gate table's AutoAck declaration for this kind (§9.6):
	// policy metadata gathered alongside the reading so stepRun never
	// consults the table itself.
	Gate        string
	GateAutoAck bool
}

// gitEvidence is the raw fact set for a dead-session verdict, gathered by
// the shell on request (effectGatherGitEvidence). The decision ladder over
// these facts lives in stepRun.
type gitEvidence struct {
	BranchPRURL     string    // PR URL found via branch lookup ("" = none)
	BranchPROutcome prOutcome // outcome of that PR (valid when BranchPRURL != "")
	URLPRFound      bool      // run.PRUrl lookup succeeded
	URLPROutcome    prOutcome // outcome of that PR (valid when URLPRFound)
	RepoRootFound   bool
	AheadKnown      bool // ahead count query succeeded
	AheadCount      int
	HasUncommitted  bool
}

// launchStage is the launch-ladder milestone vocabulary (O8). The stages are
// facts about how far the bootstrap got; mapping a stage to a run status is
// transition policy and lives in stepLaunchProgress, not at the call sites.
type launchStage int

const (
	// stageRunCreated: the run record was created (or re-targeted by a
	// continue) and the launch was accepted.
	stageRunCreated launchStage = iota
	// stageLaunchReady: the agent launch command is fully resolved and the
	// imperative bootstrap (multiplexer, server, session) is about to run.
	stageLaunchReady
	// stageWorkspaceOnly: the workspace (run record, worktree, branch,
	// prompt file) was prepared and, by request (NoSession), no multiplexer
	// session or agent was launched. The run still counts as running: the
	// monitor plane owns it from here.
	stageWorkspaceOnly
	// stageAgentStarted: the session exists and the initial prompt was
	// handed to the agent.
	stageAgentStarted
)

// launchSignal is one O8 observation: either a milestone reached or a
// bootstrap failure. Step is the machine-readable name of the failed
// bootstrap step; it becomes the status reason `launch_<step>`. Err is the
// human-readable detail, recorded as an error artifact.
type launchSignal struct {
	Stage  launchStage
	Failed bool
	Step   string
	Err    string
}

// launchReached builds the milestone form of a launch observation.
func launchReached(stage launchStage) launchSignal {
	return launchSignal{Stage: stage}
}

// launchFailed builds the failure form of a launch observation.
func launchFailed(step string, err error) launchSignal {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return launchSignal{Failed: true, Step: step, Err: msg}
}

type runObservation struct {
	Kind obsKind

	// obsPRState
	PROutcome       prOutcome
	PRURL           string
	DiscoveredPRURL string

	// obsSessionGone / obsGitEvidence
	Remote bool

	// obsSessionAlive / obsSessionGone: identity of the observing channel
	// instance (worker:<id>:<nonce> / local:<nonce>; "" = legacy channel
	// without observer context, which can never attest — §10.2).
	Observer string
	// obsSessionGone: observation-side gone classification
	// (session_absent / server_absent / unclassified). Diagnostic payload
	// only — the verification predicate is attestation, not the class.
	GoneClass string

	// obsCaptured
	Output string
	Signal agentSignal
	// CapturedPRURL is a PR URL scraped from the pane that the gatherer has
	// already verified against the run: the PR's headRefName equals the
	// run's non-empty branch artifact (pr-attach law, run-state-machine.md
	// §11). stepRun must never adopt a PR URL from raw Output.
	CapturedPRURL string
	// RefusedPRURL/RefusedPRHead carry a scraped PR URL the gatherer REFUSED
	// under the pr-attach law: the URL's verified head branch differs from
	// the run branch (L-PR2, §11). Lookup failures are not refusals — only a
	// positively-verified mismatch reaches the core. RefusedPRNoticeSent
	// reports whether the run's event log already records a daemon notice
	// for exactly this URL (L-N1); the gatherer derives it from note events
	// so the once-only guarantee survives restarts without a persisted
	// counter.
	RefusedPRURL        string
	RefusedPRHead       string
	RefusedPRNoticeSent bool
	// GateAckSent reports whether the run's event log already records a
	// daemon gate acknowledgement for the CURRENTLY detected gate kind
	// (L-G1 once-only fold; gatherer-derived like RefusedPRNoticeSent).
	GateAckSent bool

	// obsGitEvidence
	Evidence gitEvidence

	// obsLaunchProgress
	Launch launchSignal
}

type effectKind int

const (
	// effectSetStatus appends a status event (executed by updateStatus,
	// which keeps the store-level guard and same-status no-op as defense in
	// depth beneath this matrix).
	effectSetStatus effectKind = iota
	// effectRecordPR appends a "pr" artifact. If the append fails, the
	// executor vetoes the PRRecorded core flag so the next tick retries.
	effectRecordPR
	// effectRecordPRClosed appends a "pr_closed" artifact.
	effectRecordPRClosed
	// effectGatherGitEvidence asks the shell to gather gitEvidence and feed
	// it back as an obsGitEvidence observation. The policy decides when
	// evidence is needed; the shell never does.
	effectGatherGitEvidence
	// effectRecordError appends an error artifact (Msg carries the detail).
	// Emitted before the failed-status effect so the artifact order matches
	// the historical ladder behavior.
	effectRecordError
	// effectLog / effectDebugLog emit an operator log line.
	effectLog
	effectDebugLog
	// effectSendAgentNotice delivers a daemon-authored corrective message to
	// the run's agent session (L-N1..L-N3, run-state-machine.md §11): today
	// the only notice is the pr-attach refusal. The executor sends first,
	// then appends the note event that makes the notice fold-visible; a
	// failed send leaves no note, so the next confirmed-idle capture
	// retries. The effect never carries runCore values beyond the refusal
	// payload the observation itself supplied.
	effectSendAgentNotice
)

type runEffect struct {
	Kind     effectKind
	Status   model.Status // effectSetStatus
	PRURL    string       // effectRecordPR / effectRecordPRClosed / effectSendAgentNotice
	PRHead   string       // effectSendAgentNotice: verified head of the refused PR
	GateKind string       // effectSendAgentNotice: gate to acknowledge (L-G1); "" = PR-mismatch notice
	Output   string       // effectSetStatus listener payload context
	Msg      string       // effectLog / effectDebugLog
	Reason   string       // effectSetStatus: machine-readable verdict reason (model.AttrStatusReason)
}

func logEffect(format string, args ...interface{}) runEffect {
	return runEffect{Kind: effectLog, Msg: fmt.Sprintf(format, args...)}
}

func debugEffect(format string, args ...interface{}) runEffect {
	return runEffect{Kind: effectDebugLog, Msg: fmt.Sprintf(format, args...)}
}

func setStatusEffect(status model.Status) runEffect {
	return runEffect{Kind: effectSetStatus, Status: status}
}

// setStatusReasonEffect is setStatusEffect with a machine-readable reason
// attached to the resulting status event (model.AttrStatusReason).
func setStatusReasonEffect(status model.Status, reason string) runEffect {
	return runEffect{Kind: effectSetStatus, Status: status, Reason: reason}
}

// setStatusEffectWithOutput is setStatusEffect carrying the captured output
// as the listener notification payload.
func setStatusEffectWithOutput(status model.Status, output string) runEffect {
	return runEffect{Kind: effectSetStatus, Status: status, Output: output}
}

// stepRun is the single transition function for the monitor plane:
//
//	stepRun(view, core, obs, now) → (core', effects)
//
// All status decisions driven by observations O1–O5 go through here. It must
// stay pure — see the laws in step_test.go.
func stepRun(view runView, core runCore, obs runObservation, now time.Time) (runCore, []runEffect) {
	// Terminal states are exited only by user action (CanTransitionStatus);
	// no observation may produce effects on them. Shells and updateStatus
	// also guard this — encoding it here makes it a law of the pure core
	// (step_test.go L4) instead of a property of call-site discipline.
	if view.Status.IsTerminal() {
		return core, nil
	}

	switch obs.Kind {
	case obsPRState:
		return stepPRState(view, core, obs)
	case obsSessionAlive:
		core.WasAlive = true
		core.DeadCheckCount = 0
		core.DeadCheckObserver = ""
		// A successful observation attests the channel for this run (L11b):
		// only the channel that last saw the run alive may pronounce the
		// no-evidence death verdicts.
		core.AliveObserver = obs.Observer
		return core, nil
	case obsSessionGone:
		return stepSessionGone(view, core, obs)
	case obsCaptured:
		return stepCaptured(view, core, obs, now)
	case obsGitEvidence:
		return stepGitEvidence(view, core, obs, now)
	case obsLaunchProgress:
		return stepLaunchProgress(view, core, obs)
	default:
		return core, nil
	}
}

// stepLaunchProgress decides the launch-plane transitions (O8):
//
//	stageRunCreated    → queued
//	stageLaunchReady   → booting
//	stageWorkspaceOnly → running
//	stageAgentStarted  → running
//	failure            → failed (reason launch_<step>, error artifact first)
//
// The policy depends only on view.Status and the signal — never on the
// monitor core, which the launch shells do not hold. Terminal views never
// reach here (L4 guard in stepRun): a (re)launch against a terminal run
// leaves the fold untouched, matching the store-level guard that already
// rejected those appends. Re-affirming the current status emits nothing
// (L1a), so the implicit initial `queued` (the fold default) is never
// duplicated as an event.
func stepLaunchProgress(view runView, core runCore, obs runObservation) (runCore, []runEffect) {
	sig := obs.Launch

	if sig.Failed {
		step := sig.Step
		if step == "" {
			step = "bootstrap"
		}
		var effects []runEffect
		if sig.Err != "" {
			effects = append(effects, runEffect{Kind: effectRecordError, Msg: sig.Err})
		}
		effects = append(effects,
			setStatusReasonEffect(model.StatusFailed, launchFailureReason(step)),
			logEffect("%s#%s: launch failed at %s: %s", view.IssueID, view.RunID, step, sig.Err),
		)
		return core, effects
	}

	var target model.Status
	switch sig.Stage {
	case stageRunCreated:
		target = model.StatusQueued
	case stageLaunchReady:
		target = model.StatusBooting
	case stageWorkspaceOnly, stageAgentStarted:
		target = model.StatusRunning
	default:
		return core, nil
	}
	if target == view.Status {
		return core, nil
	}
	return core, []runEffect{setStatusEffect(target)}
}

// launchFailureReason is the machine-readable reason family for launch
// failures: `launch_<step>` (run-state-machine.md §5 status reasons).
func launchFailureReason(step string) string {
	return "launch_" + step
}

func stepPRState(view runView, core runCore, obs runObservation) (runCore, []runEffect) {
	var effects []runEffect

	// A PR discovered via branch lookup is recorded even when its outcome
	// forces no transition (open PRs feed the ps PR column).
	if obs.DiscoveredPRURL != "" && view.PRUrl == "" {
		effects = append(effects, runEffect{Kind: effectRecordPR, PRURL: obs.DiscoveredPRURL})
	}

	switch obs.PROutcome {
	case prOutcomeMerged:
		if obs.PRURL != "" {
			effects = append(effects, logEffect("%s#%s: detected merged PR (%s), transitioning to done", view.IssueID, view.RunID, obs.PRURL))
		} else {
			effects = append(effects, logEffect("%s#%s: detected merged PR, transitioning to done", view.IssueID, view.RunID))
		}
		effects = append(effects, setStatusEffect(model.StatusDone))
	case prOutcomeClosed:
		if obs.PRURL != "" {
			effects = append(effects, logEffect("%s#%s: detected closed PR (%s), transitioning to canceled", view.IssueID, view.RunID, obs.PRURL))
		} else {
			effects = append(effects, logEffect("%s#%s: detected closed PR, transitioning to canceled", view.IssueID, view.RunID))
		}
		effects = append(effects, runEffect{Kind: effectRecordPRClosed, PRURL: obs.PRURL})
		effects = append(effects, setStatusEffect(model.StatusCanceled))
	}

	return core, effects
}

func stepSessionGone(view runView, core runCore, obs runObservation) (runCore, []runEffect) {
	// L11a streak continuity: a dead-check streak is evidence only within a
	// single observer generation. A gone observation from a different
	// observer restarts the streak at 1 and re-owns it.
	if obs.Observer != core.DeadCheckObserver {
		core.DeadCheckObserver = obs.Observer
		core.DeadCheckCount = 1
	} else {
		core.DeadCheckCount++
	}

	if obs.Remote {
		if core.DeadCheckCount < deadChecksBeforeFailed {
			return core, []runEffect{logEffect("%s#%s: remote session not found on worker (check %d/%d, observer=%s, class=%s)", view.IssueID, view.RunID, core.DeadCheckCount, deadChecksBeforeFailed, obs.Observer, obs.GoneClass)}
		}
		// The session is gone on the execution host. A gone session must
		// never mask completed work: ask for PR/git evidence before any
		// unknown/failed verdict.
		return core, []runEffect{{Kind: effectGatherGitEvidence}}
	}

	if !core.WasAlive {
		var effects []runEffect
		if core.DeadCheckCount >= deadChecksBeforeFailed {
			effects = append(effects, runEffect{Kind: effectGatherGitEvidence})
			return core, effects
		}
		// Cadence-limited "still waiting for the session to appear" logging
		// happens in stepGitEvidence once evidence attempts begin; below the
		// threshold every check logs.
		effects = append(effects, logEffect("%s#%s: agent not alive yet (never confirmed alive), waiting", view.IssueID, view.RunID))
		return core, effects
	}

	if core.DeadCheckCount < deadChecksBeforeFailed {
		return core, []runEffect{logEffect("%s#%s: agent not alive (%d/%d checks), waiting", view.IssueID, view.RunID, core.DeadCheckCount, deadChecksBeforeFailed)}
	}
	return core, []runEffect{{Kind: effectGatherGitEvidence}}
}

// stepGitEvidence is the dead-session verdict: the decision ladder formerly
// inside inferStatusFromGitState plus the per-channel fallbacks from the
// local/remote monitor paths.
func stepGitEvidence(view runView, core runCore, obs runObservation, now time.Time) (runCore, []runEffect) {
	inferred, effects := gitVerdict(view, core, obs.Evidence, now)

	if inferred != "" {
		if inferred != view.Status {
			if obs.Remote {
				effects = append(effects, logEffect("%s#%s: remote session gone, inferred status from git state: %s", view.IssueID, view.RunID, inferred))
			} else if core.WasAlive {
				effects = append(effects, logEffect("%s#%s: agent session gone, inferred status from git state: %s", view.IssueID, view.RunID, inferred))
			} else {
				effects = append(effects, logEffect("%s#%s: agent never confirmed alive, but inferred status from git state: %s", view.IssueID, view.RunID, inferred))
			}
		}
		effects = append(effects, setStatusEffect(inferred))
		return core, effects
	}

	// No evidence-based verdict. The fallback differs by channel and
	// liveness history (run-state-machine.md §3), and every no-evidence
	// death verdict additionally requires attestation (L11b): the channel
	// pronouncing death must be the channel that last saw this run alive.
	// N consecutive not-founds from a channel that never saw this run alive
	// is testimony, not evidence (§10.1, I9).
	attested := core.DeadCheckObserver != "" && core.DeadCheckObserver == core.AliveObserver

	if obs.Remote {
		if !core.WasAlive {
			if withinNeverAliveGraceAt(view.StartedAt, now) {
				effects = append(effects, debugEffect("%s#%s: remote session not observable yet (within boot grace), waiting", view.IssueID, view.RunID))
				return core, effects
			}
			if view.Status != model.StatusUnknown {
				effects = append(effects, logEffect("%s#%s: remote agent never confirmed alive after %d checks, marking unknown", view.IssueID, view.RunID, core.DeadCheckCount))
			}
			effects = append(effects, setStatusReasonEffect(model.StatusUnknown, model.StatusReasonNeverAlive))
			return core, effects
		}
		if !attested {
			return core, appendUnattestedVerdict(view, core, effects)
		}
		effects = append(effects, logEffect("%s#%s: remote agent confirmed dead after %d checks, marking failed", view.IssueID, view.RunID, core.DeadCheckCount))
		effects = append(effects, setStatusEffect(model.StatusFailed))
		return core, effects
	}

	if !core.WasAlive {
		// L3' (run-state-machine.md §7 D-C3): a never-alive run gets no
		// verdict within the boot grace; past it, the local plane concludes
		// unknown exactly like the remote plane, so I7 (a gone session must
		// eventually produce a verdict) holds locally too.
		if withinNeverAliveGraceAt(view.StartedAt, now) {
			if core.DeadCheckCount <= deadChecksBeforeFailed || core.DeadCheckCount%neverAliveLogEvery == 0 {
				effects = append(effects, logEffect("%s#%s: agent not alive yet (never confirmed alive), waiting", view.IssueID, view.RunID))
			} else {
				effects = append(effects, debugEffect("%s#%s: agent not alive yet (never confirmed alive), waiting", view.IssueID, view.RunID))
			}
			return core, effects
		}
		if view.Status != model.StatusUnknown {
			effects = append(effects, logEffect("%s#%s: agent never confirmed alive after %d checks, marking unknown", view.IssueID, view.RunID, core.DeadCheckCount))
		}
		effects = append(effects, setStatusReasonEffect(model.StatusUnknown, model.StatusReasonNeverAlive))
		return core, effects
	}

	if !attested {
		return core, appendUnattestedVerdict(view, core, effects)
	}
	if view.Agent == "opencode" {
		effects = append(effects, logEffect("%s#%s: opencode session not found after %d checks, marking unknown", view.IssueID, view.RunID, core.DeadCheckCount))
		effects = append(effects, setStatusReasonEffect(model.StatusUnknown, model.StatusReasonSessionLost))
		return core, effects
	}
	effects = append(effects, logEffect("%s#%s: agent confirmed dead after %d checks, marking failed", view.IssueID, view.RunID, core.DeadCheckCount))
	effects = append(effects, setStatusEffect(model.StatusFailed))
	return core, effects
}

// appendUnattestedVerdict is the L11c arm: the dead-check threshold passed
// through a channel that never observed this run alive, so the standing
// evidence supports at most unknown(observer_unverified) — never failed.
// Recovery is automatic: unknown is non-terminal, and any successful capture
// re-attests the channel and resumes normal inference.
func appendUnattestedVerdict(view runView, core runCore, effects []runEffect) []runEffect {
	if view.Status != model.StatusUnknown || view.StatusReason != model.StatusReasonObserverUnverified {
		effects = append(effects, logEffect("%s#%s: session gone after %d checks, but observer %q never saw this run alive (last alive observer %q) — marking unknown, not failed",
			view.IssueID, view.RunID, core.DeadCheckCount, core.DeadCheckObserver, core.AliveObserver))
	}
	return append(effects, setStatusReasonEffect(model.StatusUnknown, model.StatusReasonObserverUnverified))
}

// gitVerdict evaluates the evidence ladder. It returns "" when the evidence
// supports no verdict; the caller applies the per-channel fallback.
func gitVerdict(view runView, core runCore, ev gitEvidence, now time.Time) (model.Status, []runEffect) {
	var effects []runEffect

	if view.Branch == "" {
		effects = append(effects, debugEffect("%s#%s: infer: skipping - branch=%q", view.IssueID, view.RunID, view.Branch))
		return "", effects
	}

	if ev.BranchPRURL != "" {
		effects = append(effects, debugEffect("%s#%s: infer: found PR %s (outcome=%s)", view.IssueID, view.RunID, ev.BranchPRURL, ev.BranchPROutcome))
		if view.PRUrl == "" {
			effects = append(effects, runEffect{Kind: effectRecordPR, PRURL: ev.BranchPRURL})
		}
		if status := statusFromPROutcome(ev.BranchPROutcome); status != "" {
			return status, effects
		}
	}

	// Branch lookup gave no verdict but the run already has a PR URL: check
	// by URL (handles deleted/rebased local branches and worker-hosted runs
	// whose repo is not visible from this host).
	if view.PRUrl != "" {
		if ev.URLPRFound {
			if status := statusFromPROutcome(ev.URLPROutcome); status != "" {
				return status, effects
			}
		}
		// A PR is known to exist even though lookups failed: preserve
		// pr_open rather than concluding unknown/failed.
		effects = append(effects, debugEffect("%s#%s: infer: preserving pr_open status (PR URL exists but lookup failed)", view.IssueID, view.RunID))
		return model.StatusPROpen, effects
	}

	if !ev.RepoRootFound {
		effects = append(effects, debugEffect("%s#%s: infer: cannot find repo root", view.IssueID, view.RunID))
		return "", effects
	}
	if !ev.AheadKnown {
		effects = append(effects, debugEffect("%s#%s: infer: cannot get ahead count", view.IssueID, view.RunID))
		return "", effects
	}

	effects = append(effects, debugEffect("%s#%s: infer: commits ahead=%d, uncommitted=%v", view.IssueID, view.RunID, ev.AheadCount, ev.HasUncommitted))
	if ev.AheadCount > 0 || ev.HasUncommitted {
		return model.StatusWaiting, effects
	}

	// No positive signals (no PR, no commits, clean worktree). The verdict
	// depends on agent lifecycle: opencode exits when finished, so a gone
	// session with no work distinguishes done/failed. Interactive agents
	// (codex, claude) idle with their session open, so no verdict here.
	if view.Agent != "opencode" {
		return "", effects
	}
	if !core.WasAlive {
		if withinNeverAliveGraceAt(view.StartedAt, now) {
			effects = append(effects, debugEffect("%s#%s: infer: within boot grace, no verdict yet", view.IssueID, view.RunID))
			return "", effects
		}
		return model.StatusFailed, effects
	}
	return model.StatusDone, effects
}

func stepCaptured(view runView, core runCore, obs runObservation, now time.Time) (runCore, []runEffect) {
	var effects []runEffect

	contentHash := hashContent(obs.Output)
	outputChanged := contentHash != core.OutputHash

	reading := inputReading(obs.Signal)
	kind, streak, confirmed := recordInputReadingStreak(core.ReadingKind, core.ReadingStreak, reading)
	core.ReadingKind = kind
	core.ReadingStreak = streak
	confirmedReading := ""
	if confirmed {
		confirmedReading = reading
	}

	if outputChanged {
		core.OutputHash = contentHash
		core.LastOutput = obs.Output
		core.LastOutputAt = now
	}

	hashPreview := contentHash
	if len(hashPreview) > 8 {
		hashPreview = hashPreview[:8]
	}
	effects = append(effects, debugEffect("%s#%s: pane hash=%s changed=%t reading=%q confirmed=%q streak=%d",
		view.IssueID, view.RunID, hashPreview, outputChanged, reading, confirmedReading, core.ReadingStreak))

	if prURL := obs.CapturedPRURL; prURL != "" && !core.PRRecorded {
		core.PRRecorded = true
		// A verified PR resolves any outstanding pr-attach refusal (L-PR2):
		// the agent's work is now tracked, the diagnostic latch is done.
		core.PRMismatchURL = ""
		core.PRMismatchHead = ""
		effects = append(effects,
			logEffect("%s#%s: PR created: %s", view.IssueID, view.RunID, prURL),
			runEffect{Kind: effectRecordPR, PRURL: prURL},
			setStatusEffectWithOutput(model.StatusPROpen, obs.Output),
		)
		return core, effects
	}

	// Latch a pr-attach refusal (L-PR2). A newer refused URL replaces the
	// latch; the latch clears only through a verified PR (above). Recorded
	// runs need no diagnostic: their PR column is already truthful.
	if obs.RefusedPRURL != "" && !core.PRRecorded {
		core.PRMismatchURL = obs.RefusedPRURL
		core.PRMismatchHead = obs.RefusedPRHead
	}

	newStatus, reason := agentVerdict(view, obs.Signal, outputChanged, confirmedReading)
	// A waiting verdict with no stronger reason surfaces the outstanding
	// refusal as pr_branch_mismatch (L-PR2): the agent believes it opened a
	// PR, orch shows why that PR is not being tracked. Gate reasons and the
	// resolved/agent-specific verdicts stay untouched.
	if newStatus == model.StatusWaiting && reason == "" && core.PRMismatchURL != "" && !core.PRRecorded {
		reason = model.StatusReasonPRBranchMismatch
	}
	if newStatus != "" && (newStatus != view.Status || reason != view.StatusReason) {
		effects = append(effects,
			logEffect("%s#%s: status change %s -> %s", view.IssueID, view.RunID, view.Status, statusWithReason(newStatus, reason)),
			runEffect{Kind: effectSetStatus, Status: newStatus, Output: obs.Output, Reason: reason},
		)
	}

	// Gate auto-ack (L-G1, §9.6): a confirmed AutoAck-gate reading is
	// acknowledged by the daemon exactly once per (run, gate kind) — the
	// gate_ack note event is the fold-visible ledger. The waiting(gate_)
	// verdict above still fires on the same tick, so the event log keeps an
	// honest trail (waiting(gate_trust) -> ack -> running via the send
	// path's feedback-resume). A gate that REAPPEARS after its one ack
	// stays waiting(gate_<kind>) for a human: ack loops are structurally
	// impossible. Non-AutoAck gates (login) never reach this block.
	if obs.Signal.Gate != "" && obs.Signal.GateAutoAck && !obs.GateAckSent &&
		confirmedReading == "gate:"+obs.Signal.Gate {
		effects = append(effects, runEffect{
			Kind:     effectSendAgentNotice,
			GateKind: obs.Signal.Gate,
		})
	}

	// Daemon notice for the refused PR (L-N1..L-N3): at most one notice per
	// (run, refused URL) — the gatherer folds sent notices from note events
	// (L-N1) — delivered only to a confirmed-idle composer (the same L10a
	// streak that gates the waiting verdict; never type into a working pane,
	// L-N2), and never as a status change of its own (L-N3; the send path's
	// feedback-resume semantics apply, exactly as for user feedback).
	if core.PRMismatchURL != "" && !core.PRRecorded &&
		obs.RefusedPRURL == core.PRMismatchURL && !obs.RefusedPRNoticeSent &&
		confirmedReading == "prompt" {
		effects = append(effects, runEffect{
			Kind:   effectSendAgentNotice,
			PRURL:  core.PRMismatchURL,
			PRHead: core.PRMismatchHead,
		})
	}

	return core, effects
}

// inputReading classifies a capture's input-request reading (§9.2): a gate
// outranks the generic prompt heuristics (L10b), busy/none is "".
func inputReading(sig agentSignal) string {
	if sig.Gate != "" {
		return "gate:" + sig.Gate
	}
	if sig.PromptShowing {
		return "prompt"
	}
	return ""
}

// gateStatusReason is the machine-readable reason family for confirmed gate
// readings: `gate_<kind>` (run-state-machine.md §5 status reasons).
func gateStatusReason(kind string) string {
	return "gate_" + kind
}

// statusWithReason renders a status plus its non-empty reason for log lines.
func statusWithReason(status model.Status, reason string) string {
	if reason == "" {
		return string(status)
	}
	return fmt.Sprintf("%s(%s)", status, reason)
}

// agentVerdict applies the agent-specific reading of a captured output. For
// resolved signals (opencode) the gatherer already produced the verdict; for
// mux agents the reading precedence is L10b:
//
//	busy veto > gate > {exited, completed, api-limited, failed} > prompt
//	          > output-changed
//
// A gate reading present on this capture masks every lower reading even
// before its streak confirms (a curated conjunctive row outranks the loose
// generic heuristics); the waiting verdict itself only fires once confirmed
// (L10a). The second return value is the machine-readable reason for the
// verdict (model.AttrStatusReason); empty for self-explanatory statuses.
func agentVerdict(view runView, sig agentSignal, outputChanged bool, confirmedReading string) (model.Status, string) {
	if sig.Resolved {
		return sig.Status, ""
	}
	if sig.Gate != "" {
		if confirmedReading == "gate:"+sig.Gate {
			return model.StatusWaiting, gateStatusReason(sig.Gate)
		}
		return "", ""
	}
	switch {
	case sig.Exited:
		return model.StatusUnknown, model.StatusReasonAgentExited
	case sig.Completed:
		return model.StatusDone, ""
	case sig.APILimited:
		return model.StatusRateLimited, ""
	case sig.Failed:
		return model.StatusFailed, ""
	case confirmedReading == "prompt":
		return model.StatusWaiting, ""
	case outputChanged:
		return model.StatusRunning, ""
	default:
		return "", ""
	}
}

// withinNeverAliveGraceAt is the pure form of withinNeverAliveGrace: a run
// never observed alive gets neverAliveVerdictGrace from StartedAt before
// dead checks may conclude unknown/failed.
func withinNeverAliveGraceAt(startedAt, now time.Time) bool {
	return !startedAt.IsZero() && now.Sub(startedAt) < neverAliveVerdictGrace
}

// effectsContain reports whether any effect has the given kind.
func effectsContain(effects []runEffect, kind effectKind) bool {
	for _, e := range effects {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// statusEffectOf reports whether the effects include a status transition
// (used by shells to end the tick like the historical control flow did).
func statusEffectOf(effects []runEffect) (model.Status, bool) {
	for _, e := range effects {
		if e.Kind == effectSetStatus {
			return e.Status, true
		}
	}
	return "", false
}
