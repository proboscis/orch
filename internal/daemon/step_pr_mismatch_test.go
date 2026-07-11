package daemon

// Law tests for the pr-attach refusal diagnostics (run-state-machine.md §11):
// L-PR2 (a refused scrape surfaces as waiting reason pr_branch_mismatch while
// unresolved) and L-N1..L-N3 (the daemon notice: once per (run, refused URL),
// delivered only to a confirmed-idle composer, never a status change of its
// own). The incident replay encodes the 2026-07-11 codex run that opened its
// PR from a self-invented branch and sat waiting, invisible, for 6 minutes.

import (
	"testing"
	"time"

	"github.com/proboscis/orch/internal/model"
)

const testRefusedURL = "https://github.com/o/r/pull/534"

// refusalObs is a captured pane that mentions a foreign-branch PR while the
// composer idles (prompt showing).
func refusalObs(noticeSent bool) runObservation {
	return runObservation{
		Kind:                obsCaptured,
		Output:              "I opened " + testRefusedURL + "\n❯ ",
		Signal:              agentSignal{PromptShowing: true},
		RefusedPRURL:        testRefusedURL,
		RefusedPRHead:       "fix/self-invented-branch",
		RefusedPRNoticeSent: noticeSent,
	}
}

func noticeEffectOf(effects []runEffect) (runEffect, bool) {
	for _, e := range effects {
		if e.Kind == effectSendAgentNotice {
			return e, true
		}
	}
	return runEffect{}, false
}

// L-PR2: the refusal latches on first observation and the confirmed waiting
// verdict carries reason pr_branch_mismatch.
func TestStepLawPRMismatchReason(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true}

	// Tick 1: refusal observed, prompt streak 1 — latch set, no waiting
	// verdict yet (L10a), so no reason either.
	var effects []runEffect
	core, effects = stepRun(view, core, refusalObs(false), now)
	if core.PRMismatchURL != testRefusedURL || core.PRMismatchHead != "fix/self-invented-branch" {
		t.Fatalf("latch = (%q, %q), want the refused URL/head", core.PRMismatchURL, core.PRMismatchHead)
	}
	if status, _, ok := statusReasonEffectOf(effects); ok && status == model.StatusWaiting {
		t.Fatalf("waiting verdict before the streak confirms: %+v", effects)
	}

	// Tick 2: streak confirms — waiting(pr_branch_mismatch).
	core, effects = stepRun(view, core, refusalObs(false), now)
	status, reason, ok := statusReasonEffectOf(effects)
	if !ok || status != model.StatusWaiting || reason != model.StatusReasonPRBranchMismatch {
		t.Fatalf("confirmed verdict = (%v %q %t), want waiting(pr_branch_mismatch)", status, reason, ok)
	}
}

// L10b precedence: a gate reading outranks the mismatch diagnostic — the
// reason stays gate_<kind>.
func TestStepLawPRMismatchYieldsToGateReason(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true, PRMismatchURL: testRefusedURL, PRMismatchHead: "fix/x"}

	gate := runObservation{Kind: obsCaptured, Output: "sign in with chatgpt\npress enter to continue", Signal: agentSignal{Gate: "login"}}
	var effects []runEffect
	core, effects = stepRun(view, core, gate, now)
	core, effects = stepRun(view, core, gate, now)
	if status, reason, ok := statusReasonEffectOf(effects); !ok || status != model.StatusWaiting || reason != "gate_login" {
		t.Fatalf("gate verdict = (%v %q %t), want waiting(gate_login) — the gate outranks pr_branch_mismatch", status, reason, ok)
	}
}

// A verified PR clears the latch (L-PR2 resolution path): the run transitions
// pr_open and later waiting verdicts carry no mismatch reason.
func TestStepLawPRMismatchClearedByVerifiedPR(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true, PRMismatchURL: testRefusedURL, PRMismatchHead: "fix/x"}

	verified := runObservation{
		Kind:          obsCaptured,
		Output:        "opened https://github.com/o/r/pull/606\n❯ ",
		Signal:        agentSignal{PromptShowing: true},
		CapturedPRURL: "https://github.com/o/r/pull/606",
	}
	core, effects := stepRun(view, core, verified, now)
	if status, ok := statusEffectOf(effects); !ok || status != model.StatusPROpen {
		t.Fatalf("verified PR must conclude pr_open, got %+v", effects)
	}
	if core.PRMismatchURL != "" || core.PRMismatchHead != "" {
		t.Fatalf("latch must clear on a verified PR, got (%q, %q)", core.PRMismatchURL, core.PRMismatchHead)
	}

	// Subsequent idle waiting (e.g. after the PR is later closed and the
	// view returns to a non-terminal status) carries no stale reason.
	idle := runObservation{Kind: obsCaptured, Output: "❯ ", Signal: agentSignal{PromptShowing: true}}
	core.PRRecorded = false // simulate a later lifecycle where scrape resumes
	core, _ = stepRun(view, core, idle, now)
	_, effects = stepRun(view, core, idle, now)
	if _, reason, ok := statusReasonEffectOf(effects); ok && reason == model.StatusReasonPRBranchMismatch {
		t.Fatal("cleared latch must not resurrect the mismatch reason")
	}
}

// L-N1 + L-N2: the notice fires exactly when the refusal is current, unsent,
// and the composer is confirmed idle — and never again once the note event
// records the delivery.
func TestStepLawPRMismatchNoticeOnceAndIdleOnly(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true}

	// Busy pane mentioning the refused PR: latch, but no notice (L-N2).
	busy := refusalObs(false)
	busy.Signal = agentSignal{}
	var effects []runEffect
	core, effects = stepRun(view, core, busy, now)
	if _, ok := noticeEffectOf(effects); ok {
		t.Fatal("notice into a busy pane (L-N2 violation)")
	}

	// Idle tick 1: prompt streak 1, unconfirmed — still no notice (L-N2:
	// the same streak that gates the waiting verdict gates the notice).
	core, effects = stepRun(view, core, refusalObs(false), now)
	if _, ok := noticeEffectOf(effects); ok {
		t.Fatal("notice before the idle reading confirms (L-N2 violation)")
	}

	// Idle tick 2: confirmed — exactly one notice, carrying the refusal.
	core, effects = stepRun(view, core, refusalObs(false), now)
	notice, ok := noticeEffectOf(effects)
	if !ok {
		t.Fatalf("confirmed idle + unsent refusal must emit the notice, got %+v", effects)
	}
	if notice.PRURL != testRefusedURL || notice.PRHead != "fix/self-invented-branch" {
		t.Fatalf("notice payload = (%q, %q), want the refusal", notice.PRURL, notice.PRHead)
	}

	// After the note event folds into the observation, no further notice
	// for the same URL (L-N1) — while the reason keeps showing.
	core, effects = stepRun(view, core, refusalObs(true), now)
	if _, ok := noticeEffectOf(effects); ok {
		t.Fatal("second notice for the same (run, URL) (L-N1 violation)")
	}
}

// L-N3: the notice effect is not a status transition — the waiting verdict
// and the notice ride the same tick as separate effects.
func TestStepLawPRMismatchNoticeIsNotAStatusChange(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true}

	var effects []runEffect
	core, _ = stepRun(view, core, refusalObs(false), now)
	core, effects = stepRun(view, core, refusalObs(false), now)

	status, reason, hasStatus := statusReasonEffectOf(effects)
	_, hasNotice := noticeEffectOf(effects)
	if !hasStatus || !hasNotice {
		t.Fatalf("confirmed tick must carry both verdict and notice, got %+v", effects)
	}
	if status != model.StatusWaiting || reason != model.StatusReasonPRBranchMismatch {
		t.Fatalf("verdict = %s(%s), want waiting(pr_branch_mismatch)", status, reason)
	}
}

// Incident replay (2026-07-11, proboscis-ema ISSUE-TRD-162 run 5516c4): the
// agent worked, printed a PR URL from a self-invented branch, and idled. Old
// behavior: waiting with no reason, no feedback, daemon log spam only. New
// behavior: waiting(pr_branch_mismatch) + exactly one corrective notice; the
// fix (a verified run-branch PR) concludes pr_open and clears the diagnostic.
func TestStepIncidentReplayForeignBranchPR(t *testing.T) {
	now := time.Now()
	view := stepTestView(model.StatusRunning, "codex", now.Add(-time.Hour))
	core := runCore{WasAlive: true}

	// Agent working; pane changes tick over runs normally.
	working := runObservation{Kind: obsCaptured, Output: "running tests...", Signal: agentSignal{}}
	core, _ = stepRun(view, core, working, now)

	// Agent announces the foreign-branch PR and idles.
	core, _ = stepRun(view, core, refusalObs(false), now)
	core, effects := stepRun(view, core, refusalObs(false), now)
	status, reason, ok := statusReasonEffectOf(effects)
	if !ok || status != model.StatusWaiting || reason != model.StatusReasonPRBranchMismatch {
		t.Fatalf("idle after foreign PR = (%v %q), want waiting(pr_branch_mismatch)", status, reason)
	}
	if _, ok := noticeEffectOf(effects); !ok {
		t.Fatal("the corrective notice must ride the confirmed idle tick")
	}

	// Notice delivered (note event folds in); the agent resumes and pushes
	// the run branch: the verified PR concludes pr_open, latch cleared.
	core, _ = stepRun(view, core, refusalObs(true), now)
	fixed := runObservation{
		Kind:          obsCaptured,
		Output:        "opened https://github.com/o/r/pull/606 from the run branch\n❯ ",
		Signal:        agentSignal{PromptShowing: true},
		CapturedPRURL: "https://github.com/o/r/pull/606",
	}
	core, effects = stepRun(view, core, fixed, now)
	if status, ok := statusEffectOf(effects); !ok || status != model.StatusPROpen {
		t.Fatalf("verified fix must conclude pr_open, got %+v", effects)
	}
	if core.PRMismatchURL != "" {
		t.Fatalf("latch must be gone after the fix, got %q", core.PRMismatchURL)
	}
}
