# Run State Machine — Transition Inventory and `step()` Design

Status: implemented (v1: monitor plane)
Date: 2026-06-11

This document is the prerequisite survey and design record for consolidating
run-status transition logic into a single pure transition function:

```
step(view, core, observation, now) → (core', effects)
```

It enumerates (1) every site that writes a run status event, (2) the
observation taxonomy, (3) the current transition matrix, (4) the implicit
invariants carried by in-memory monitor state, and (5) the v1 scope and design
decisions. The matrix below is the authoritative reference for the behavior
`step()` encodes; changes to transition policy must update both.

## 1. Status write surface (as of main @ 88ef7dc5)

Run status is already event-sourced at the store level: a run's `Status` is
the fold of `status` events in its `.md` file (`FileStore.loadRun` →
`run.DeriveState()`), and `FileStore.AppendEvent` enforces the only global
guard (`CanTransitionStatus`: terminal states can only be exited by
`source=user`). Everything else — debouncing, dead-check thresholds, grace
windows, inference — lives in the writers:

| # | Site | Transitions written | Guards |
|---|------|---------------------|--------|
| W1 | `monitor.go updateStatus` (monitor-plane executor over `commitRunStatus`) | all monitor inferences | `commitRunStatus` guards; auto-resolve issue on `done`, `publishRunEvent`, `fireStatusChange` after commit |
| W2–W5 | launch ladders (`handleStartRun` / `processStartRunCore` / `processContinueRunCore` / `handleContinueRun`) | **no direct writes since D-B1 (2026-06-13)**: milestones/failures are O8 observations; `stepLaunchProgress` decides, `reportLaunchProgress` executes via `commitRunStatus` | `commitRunStatus` guards (policy-level same-status no-op on top) |
| W6 | `socket.go markRunFeedbackSent` | `waiting/pr_open/rate_limited/unknown` → `running` | `feedbackResumesRun` predicate; fires `onRunFeedback` (PromptStreak reset), `fireStatusChange`, `PublishRunEvent` after append |
| W7 | `socket.go failOpenCodeRunBootstrap` | **no direct writes since D-B1**: routes `launchFailed("opencode_bootstrap")` through O8 | same as W2–W5 |
| W8 | `socket.go appendRunCanceledByUser` (`orch stop`) | → `canceled` (`source=user`) | skip if terminal |
| W9 | `proto_handler.go syncStartRunResultToMasterStore` / `syncContinueRunResultToMasterStore` | `queued` (if record missing) + result status (default `running`) | store-level only |
| W10 | `socket.go handleAppendEvent` (external append API; agent self-report) | arbitrary, caller-supplied source | `CanTransitionStatus` with caller source |

Notes:

- Status events are **constructed in exactly one place**: `commitRunStatus`
  (`status_commit.go`), which owns the terminal check, `CanTransitionStatus`,
  the same-status no-op, and the append. Its two sanctioned executors are
  W1 (`updateStatus`, monitor plane) and `reportLaunchProgress`
  (launch plane, usable in the worker process where no `Daemon` exists).
  The remaining annotated writers (W6, W8, W9, W10, `ResolveRun`) are the
  frozen legacy / law-boundary set.
- W2–W5 remain four near-copies of the same *imperative bootstrap* (legacy +
  core × start/continue), but since D-B1 they carry no transition policy:
  every status decision flows through `stepLaunchProgress`. Collapsing the
  four control flows themselves is the remaining v2 work.
- Worker-hosted runs are dual-written: the worker executes W3/W4/W6 against
  its **local** store (`worker_plane.go executeLeaseEffect`), while the master
  store receives a projection (W9) and is then maintained by the master's
  monitor (W1 via capture leases). There is no synchronization invariant
  between the two stores; the master store is the client-visible SSOT.
- `fireStatusChange` (Slack listeners) is invoked after committed W1
  monitor-plane transitions in `updateStatus`, so O1/O3/O4/O5 transitions
  share the same listener fanout. W6 feedback resume bridges into the same
  fanout after its legacy append until O6 is integrated into `step()` v2.

## 2. Observation taxonomy

Facts the daemon can notice about a run, with their sources:

| Obs | Fact | Source | Cost |
|-----|------|--------|------|
| O1 | PR outcome (`open/merged/closed/unknown`) + URL | gh cache via `PRUrl` or branch lookup | cached |
| O2 | session alive | local mux `IsAlive` / successful lease capture | cheap / lease RTT |
| O3 | session gone (one dead check) | local `IsAlive=false` / lease `session_not_found` | same |
| O4 | captured output | local `CapturePane` / `capture_session` lease | cheap / lease RTT (paced 15s) |
| O4a | — derived: output changed | content hash vs `OutputHash` (excludes status-bar lines) | pure |
| O4b | — derived: prompt showing (mux agents) | `IsWaitingForInput` string heuristics | pure |
| O4c | — derived: agent verdict (exited/completed/api-limited/failed) | mux string heuristics; opencode session-status HTTP API (busy/idle/retry/gone) | pure / HTTP |
| O4d | — derived: PR URL in output | regex | pure |
| O5 | git evidence (PR info, ahead count, uncommitted changes) | gh cache + git, gathered only after a dead verdict is near | subprocess |
| O6 | user feedback delivered (`orch send` without `--no-enter`) | send paths | — |
| O7 | user stop | stop path | — |
| O8 | launch lifecycle progress (`launchSignal`: stage milestone or failed step) | launch ladders via `reportLaunchProgress` | — |
| O9 | time | `now` vs `StartedAt` (boot grace), tick cadence, backoff timers | — |
| O10 | agent self-report | `handleAppendEvent` | — |

## 3. Current transition matrix (monitor plane)

States: `queued, booting, running, waiting, rate_limited, pr_open, unknown`
(active) and `done, failed, canceled` (terminal). The monitor lists every
non-terminal run, including `queued` (see I6).

Per tick, observations are evaluated in this order; the first terminal
transition ends the tick:

```
観測                         条件                                  遷移/効果
──────────────────────────────────────────────────────────────────────────────
O1 PR merged                 branch or PRUrl set                   → done
O1 PR closed                 〃                                    → canceled (+pr_closed artifact)
O1 PR open/unknown           〃                                    遷移なし (URL発見時はartifact記録のみ)

O3 session gone              count < 3                             遷移なし (カウント++)
O3 session gone, count ≥ 3   → O5 git evidence を収集して判定:
   O5: PR(branch) found                                            → done/canceled/pr_open (outcomeに従う; URL未記録なら記録)
   O5: PRUrl set, lookup ok                                        → done/canceled/pr_open
   O5: PRUrl set, lookup fail                                      → pr_open (PR存在は既知)
   O5: commits ahead > 0 or uncommitted                            → waiting
   O5: no signal, agent=opencode, was-alive                        → done   (opencodeは完了でexitする)
   O5: no signal, agent=opencode, never-alive, grace超過           → failed
   O5: no signal, agent=opencode, never-alive, grace内             遷移なし
   O5: no signal, agent≠opencode:
       local,  was-alive                                           → failed
       local,  never-alive, grace内                                 遷移なし
       local,  never-alive, grace超過                               → unknown (L3', §7 D-C3)
       remote, was-alive                                           → failed
       remote, never-alive, grace内                                 遷移なし
       remote, never-alive, grace超過                               → unknown

O2 session alive                                                   カウンタreset, WasAlive=true
O4 captured + PR URL in output   未記録                            → pr_open (+pr artifact)
O4 captured + agent verdict:     (mux: 優先順位順)
   exited                                                          → unknown
   completed                                                       → done
   api-limited                                                     → rate_limited
   failed                                                          → failed
   prompt streak ≥ 2                                               → waiting
   output changed                                                  → running
   (opencode: busy→running, idle→waiting, retry→rate_limited,
    gone→unknown; booting/queued→running)
   status append 成功後に updateStatus が fireStatusChange (Slack等)

O6 feedback delivered        status ∈ {waiting,pr_open,            → running (+PromptStreak reset)
                             rate_limited,unknown}
O7 user stop                 非terminal                            → canceled (source=user)
O8 launch progress           stageRunCreated                       → queued
   (W2–W5/W7 →               stageLaunchReady                      → booting
    stepLaunchProgress)      stageWorkspaceOnly / stageAgentStarted → running
                             bootstrap step failed                 → failed
                               (reason launch_<step>; error artifact first)
   terminal view は L4 で吸収(store guard が拒否していた旧挙動と同値)。
   同値 status への再到達は無発行(L1a — 暗黙の初期 queued は二重化しない)。
```

## 4. Implicit invariants of in-memory monitor state (`RunState`)

| # | Invariant | Status |
|---|-----------|--------|
| I1 | `DeadCheckCount == 0` ⇔ last observation was alive; `>= 3` consecutive dead checks ⇒ verdict allowed | holds, but scattered across two paths |
| I2 | `WasAlive` is monotone (never unset) | holds across restarts since D-C1 (§7, 2026-06-12): `WasAlive`/`PRRecorded` are re-derived from the event log at registration; the ephemeral counters reset by design and re-converge (L7) |
| I3 | `PromptStreak >= 2` required before `waiting` (debounce); reset on feedback | holds (W6 + `noteRunFeedback`) |
| I4 | duplicate status events must not accumulate | enforced only in W1 (same-status no-op); W2–W9 rely on transition legality alone |
| I5 | `PRRecorded` dedupes `pr` artifacts | holds across restarts since D-C1 (§7, 2026-06-12): derived from existing `pr` artifacts at registration |
| I6 | every non-terminal run is eventually observed | holds since `monitorAll` lists `queued` as well as the other non-terminal states, so a run orphaned before `booting` still reaches the monitor plane |
| I7 | a gone session must eventually produce a verdict | holds since L3' (§7 D-C3, 2026-06-12): both planes conclude `unknown` after the never-alive grace |
| I8 | status-change listeners observe every committed W1 transition | holds since D-C4 (§7, 2026-06-12): `updateStatus` fires once after a successful status append; same-status no-ops and append failures fire nothing |

I2, I5 (via D-C1), I7 (via D-C3), I6 (`inv-monitor-queued-orphans`) and
I8 (D-C4, `inv-fire-status-change-all`) were resolved on 2026-06-12. I4's
broader guarantee arrives with the Phase B write-surface consolidation.
W6 feedback resume also fires the listener after its committed append; the
remaining launch/stop legacy writers stay in the v2 disposition table until
they are integrated into `step()`.

## 5. `step()` v1 — scope and design decisions

### Scope

**In**: the monitor plane — every transition decided from observations
O1–O5 (all of `monitorRun` / `monitorRemoteRun` / `processRunOutput` /
`inferStatusFromGitState` decision logic). This is where state, debounce,
grace, and inference interact, and where the historical bugs concentrated
(duplicate events, missed inference paths, double monitoring).

**Out (v1)**, each a single already-guarded site, candidates for v2:
launch ladders (W2–W5; imperative bootstrap, would need launch-progress
observations), feedback (W6), stop (W8), master projection (W9), external
append (W10).

### Decision/execution split

```
                    gather (impure)                 step (pure)              execute (impure)
  ┌──────────────┐  PR cache, mux, leases,  ┌───────────────────────┐  effects  ┌──────────────┐
  │ monitorRun / │ ───── observation ─────► │ step(view, core, obs, │ ────────► │ updateStatus │
  │ remote shell │                          │      now)             │           │ AppendEvent  │
  └──────────────┘ ◄─── effectGatherGit ─── │  = transition matrix  │           │ fire/publish │
        ▲             (request more obs)    └───────────────────────┘           └──────────────┘
        └── capture pacing, backoff, lease scheduling stay in the shell (mechanism)
```

- `runView` — read-only run fields the policy needs (status, agent, branch,
  PR URL, StartedAt).
- `runCore` — the semantic counters (`WasAlive`, `DeadCheckCount`,
  `PromptStreak`, `OutputHash`, `LastOutput*`, `PRRecorded`), embedded in
  `RunState` so existing field accesses keep working. Capture backoff and
  lease pacing fields remain shell-owned: *when* to observe is mechanism,
  *what an observation means* is policy.
- Expensive git/PR evidence is requested by `step` via an effect
  (`effectGatherGitEvidence`) and fed back as a new observation — the shell
  never decides whether evidence is needed.
- Effect execution is transactional per observation: the shell commits
  `core'` only if all effects applied; on failure the old core is kept and
  the next tick retries. (Previously a failed `pr` artifact write retried via
  an equivalent in-memory rollback; a failed status append could leak
  hash/streak updates — the commit rule makes retry uniform.)
- `updateStatus` remains the single executor for status effects, keeping the
  store-level guard, same-status no-op, auto-resolve, and event publication
  as defense in depth beneath the matrix.

### Laws (encoded as property tests in `step_test.go`)

Observations split into two classes with different repetition semantics — a
distinction the law tests themselves forced:

- **fact observations** (PR state, git evidence, session alive): snapshots of
  world state. Re-observing the same fact must be idempotent.
- **stream observations** (capture, session gone): per-tick samples. Their
  repetition legitimately advances debounce/dead counters — the law is
  bounded convergence, not idempotency.

| Law | Statement |
|-----|-----------|
| L1a idempotency (facts) | re-applying a fact observation after its transition committed never yields a different target |
| L1b fixed point (streams) | repeating one stream observation reaches a quiet fixed point after boundedly many steps (no oscillation) |
| L2 order-independent convergence | {session gone ×3 + git evidence, PR merged/closed} in any interleaving converge to the same terminal status |
| L3' grace | a never-alive run within `neverAliveVerdictGrace` never receives an `unknown`/`failed` verdict, on any observation sequence; after the grace, any plane concludes `unknown` on the standing evidence (revised from v1's pinned asymmetry by §7 D-C3) |
| L4 terminality | from a terminal status, `step` emits no effect and no core change for any observation |
| L5 verdict requires evidence | `obsSessionGone` never emits a status directly; it requests git evidence exactly when the dead-check threshold is reached |
| L6 debounce | `waiting` via prompt requires ≥ `waitingPromptStreakThreshold` consecutive prompt observations; any busy capture resets the streak |
| L8 listener commit point | every committed W1 status transition fires exactly one status-change listener event after `AppendEvent` succeeds; duplicate-status no-ops and failed appends fire none |
| L9 launch failure reason | a launch-failure verdict (O8) always carries the machine-readable reason `launch_<step>`, preceded by an error artifact recording the bootstrap error; launch milestones on a terminal or already-reached status emit nothing |

### Status reasons (verdict payload)

The status vocabulary is a closed set; *why* an `unknown` verdict was reached
travels as a machine-readable `reason` attribute on the status event
(`model.AttrStatusReason` — the k8s `phase`+`reason` pattern). Every
`unknown` verdict emitted by `step()` carries one:

| reason | emitted by | operator response |
|--------|-----------|-------------------|
| `never_alive` | L3' grace expiry, either plane | infrastructure problem (binary/auth/mux env); fix the host before retrying |
| `session_lost` | opencode session not found after dead checks | backend lost observability; backend-specific triage |
| `agent_exited` | capture verdict: process exited, shell prompt showing | check transcript/worktree; retry plausible |
| `launch_<step>` | O8 bootstrap failure (`failed` verdict); `<step>` ∈ worktree, prompt, agent_command, multiplexer, opencode_server, session, opencode_bootstrap, bootstrap | the named bootstrap step broke; the paired error artifact carries the detail |

`orch ps` renders the reason inline (`unknown(never_alive)`); the event log
carries it as `reason=…`; `StatusChangeEvent.Reason` exposes it to listeners.

L1/L4 fix the 5,830-duplicate-event class structurally; L2 is the law the
PR #458 inference fixes were converging toward; restart transparency (I2)
remains an open law until monitor state is derived from the event log
(resolved by decision D-C1 in §7).

## 6. v2 disposition of the remaining writers (decided 2026-06-12)

Phase A of the coupling-core roadmap closed the write surface mechanically
(semgrep rule `run-status-write-surface`; every existing writer carries a
frozen `nosemgrep` annotation). Each frozen writer now has an explicit
disposition — integrate into `step()` or quarantine with a recorded
rationale. *Undecided* is no longer a legal state for a status writer.

| # | Site | Disposition | Rationale |
|---|------|-------------|-----------|
| W2–W5 | launch ladders (socket.go ×4) | **integrated — D-B1 (2026-06-13)** | Transition policy moved to `stepLaunchProgress` (O8); ladders report milestones/failures via `reportLaunchProgress`, commits go through `commitRunStatus`. The four imperative control flows remain to be collapsed in v2. |
| W7 | `failOpenCodeRunBootstrap` → failed | **integrated — D-B1 (2026-06-13)** | Routes `launchFailed("opencode_bootstrap")` through the same O8 path. |
| W6 | feedback → running | **integrate — v2** | Already mutates `runCore` (PromptStreak reset, O6). Core state must change only through `step()`. Until then, the legacy writer emits `fireStatusChange` after its append to preserve listener coverage. |
| W8 | `appendRunCanceledByUser` (`orch stop`) | **integrate — v2, after the ladder** | O7 (user stop) exists in the observation taxonomy; terminality (L4) should observe it. Single guarded site until then. |
| W9 | master projections (proto_handler.go) | **quarantine — law boundary** | Replicates transitions already decided on the worker plane; carries no local policy. Guards: `CanTransitionStatus` + fail-fast appends (Phase A2). Law: a projection is status-preserving — it must never add inference in flight. |
| W10 | external append API (`handleAppendEvent`) | **quarantine — law boundary** | User/agent escape hatch with caller-supplied source. Guard: `CanTransitionStatus` with caller source. `orch repair` (cli/repair.go) is its sanctioned client and stays. |
| — | TUI `Monitor.StopRun` (client plane) | **removed — B3** | Now calls the daemon `StopRun` verb (host-aware session kill + daemon-side canceled append). |
| — | TUI `Monitor.ResolveRun` (client plane) | **pending — B3 issue** | Daemon `ResolveRun` verb gains run-done semantics; see the equation below. |

B3 ResolveRun equation (spec for the pending issue — the daemon verb, not
the orchapi lookup of the same name):

```
ResolveRun(issue, run) ≡  append(run, done, source=user)   if ¬terminal(run)
                          ; SetIssueStatus(issue, resolved)
```

Today `handleProtoResolveRun` implements only the second conjunct; the TUI
implements both client-side. After the issue lands, exactly one
implementation exists (daemon), and the TUI client calls it.

## 7. Open-law decisions (decided 2026-06-12)

### D-C1 — restart transparency without new persistence (implemented 2026-06-12)

`runCore` fields fall into two classes, and the restart story differs:

- **Derivable (fold of the event log)** — must be re-derived at monitor
  registration, never mirrored:
  - `WasAlive := ∃ status event ∈ {running, waiting, rate_limited, done}`
  - `PRRecorded := ∃ artifact event of type pr` (also resolves I5)
  - boot grace: derivable from the persisted `StartedAt`
- **Ephemeral by law (bounded re-convergence)** — deliberately reset on
  restart; L1b guarantees they re-converge within bounded ticks, and the
  reset direction is safe (verdicts/debounce are *delayed*, never wrong):
  `DeadCheckCount`, `PromptStreak`, `OutputHash`, `LastOutput*`.

Rejected alternatives: (a) full observation replay (observations are not
events; would require recording per-tick captures), (b) persisting counter
changes as events (OutputHash churn would spam the log), (c) periodic
snapshots (a second store of record — exactly the mirror this design
forbids).

| Law | Statement |
|-----|-----------|
| L7 restart transparency | for any observation sequence and any restart point, the sequence of *transition targets* is identical with and without the restart; only their timing may differ, by at most `max(deadCheckThreshold, waitingPromptStreakThreshold)` ticks |

### D-C3 — never-alive verdict asymmetry resolved (implemented 2026-06-12)

L3's pinned complement ("after grace, remote concludes `unknown`; local
keeps waiting") is revised: **local and remote both conclude `unknown`
after `neverAliveVerdictGrace`**. Rationale: I7 (a gone session must
eventually produce a verdict) is a liveness requirement; the local
keep-waiting behavior violates it for never-alive non-opencode runs, and
the asymmetry was pinned in v1 only for behavior preservation. L3 reads
after the change:

| Law | Statement |
|-----|-----------|
| L3' grace (revised) | a never-alive run within `neverAliveVerdictGrace` never receives an `unknown`/`failed` verdict; after the grace, any plane concludes `unknown` on the standing evidence |

I6 is resolved by listing `queued` runs in `monitorAll`: a run orphaned before
`booting` enters the same monitor plane and follows L3' (no verdict within
`neverAliveVerdictGrace`; `unknown` after grace on standing dead evidence).

### D-C4 — listener dispatch moved to the commit point (implemented 2026-06-12)

`fireStatusChange` is no longer a separate `step()` effect emitted only by
O4 agent inference. W1 (`Daemon.updateStatus`) now dispatches listeners
after the status event append succeeds, using the store-derived `from`
status and the requested `to` status. This makes listener delivery follow
the event log: if the append fails, no listener fires; if the transition is a
same-status no-op, no listener fires; otherwise exactly one listener event is
emitted for the committed W1 transition. The event carries the verdict
`reason` (model.AttrStatusReason) alongside `from`/`to`, so listeners (Slack…)
can render `unknown(never_alive)` without reading the run record.

W6 feedback resume is still a legacy writer until O6 enters `step()` v2, so
it bridges into the same listener fanout immediately after its append. This
keeps `orch send` resume notifications consistent with W1 without changing
the transition policy matrix.

### D-B1 — launch ladders integrated as O8 observations (implemented 2026-06-13)

The four launch ladders (W2–W5) and their failure arm (W7) no longer write
status events. Each milestone or failure is reported as an O8 observation
(`launchSignal`) to `reportLaunchProgress`, which feeds it through the pure
policy `stepLaunchProgress` and executes the effects. The imperative
bootstrap (worktree, prompt file, multiplexer, server, session) stays where
it was — only the transition *decisions* moved, exactly the
mechanism/policy split of D2 in §5.

Decisions taken:

- **Single constructor.** `Daemon.updateStatus` no longer owns the append:
  its guard/append core was extracted to `commitRunStatus`
  (`status_commit.go`), the one place that constructs status events. The
  launch executor calls the same function, so the worker process (which has
  a `SocketServer` but no `Daemon`) commits through the identical guards.
- **Stage vocabulary, not statuses, in the observation.** The ladders report
  `stageRunCreated / stageLaunchReady / stageWorkspaceOnly /
  stageAgentStarted` or `launchFailed(step, err)`; the stage→status mapping
  is policy. `stageWorkspaceOnly` is the `--tmux=false` arm: workspace
  prepared, no agent launched, run handed to the monitor plane as `running`.
- **Terminal absorption replaces guard reliance.** A launch observation on a
  terminal view is absorbed by the stepRun L4 guard. Previously those
  appends were silently *rejected* by the store guard (daemon source on a
  terminal run); the fold outcome is identical, but the no-write is now a
  policy decision instead of a discarded error. All ladders operate on
  freshly created runs (continue creates a new run linked by
  `continued_from`), so this arm only fires when a user cancels mid-boot.
- **The implicit initial `queued` is no longer materialized.** A fresh run's
  fold already yields `queued` (the `GetStatus` default); the policy-level
  same-status no-op therefore skips the event the ladders used to append
  right after `CreateRun`. One less redundant event, no fold change.
- **Failure verdicts gained machine-readable reasons** (`launch_<step>`,
  L9) and keep the error-artifact-then-verdict order of the old ladders.
- **Listener scope unchanged.** `reportLaunchProgress` does not fire
  `fireStatusChange` / `publishRunEvent`: launch transitions were never
  listener-visible, and widening I8's scope is a separate decision from
  removing the write surface. The asymmetry is recorded in §1 notes.

Whitelist meter: 47 → 10 annotations (socket.go 40 → 3; the remaining three
are W6, W8 and `appendRunResolvedByUser`, all with §6 dispositions).

## 8. Vocabulary drift at serialization boundaries (decided 2026-07-07)

**Rule: vocabulary drift at serialization boundaries degrades to skip+log —
never panics, never invents a status.**

The status vocabulary can drift ahead of the proto map (newer binary wrote,
older binary reads, or vice versa; there is no CLI/daemon version handshake).
The conversion layer (`internal/daemon/proto_convert.go` —
`modelStatusToProto` and friends) returns `(value, error)` for an unmapped
status; it never panics. Callers at serialization boundaries (event-bus
publish in `Daemon.publishRunEvent`, `SocketServer.publishFeedbackResume`)
handle the error by logging an ERROR naming the run ref and the offending
status, then skipping that run's notification for that tick. No status event
is written, no synthetic status (e.g. `unknown`) is substituted, and the
daemon stays alive — one drifted run must not stop monitoring for all runs.

This does not weaken fail-fast elsewhere: recover wrappers still re-panic
(`logAndRepanic`), the transition policy (`step()`) and the status write
surface (`commitRunStatus`) are untouched, and discarding conversion errors
remains banned by the `fail-fast-no-discard-status-parse-error` semgrep rule.

## 9. Interactive gates — parked-at-gate detection (designed 2026-07-07)

Status: **design — accepted pending human review; no implementation yet.**
All matrix rows, reason-table rows, and core fields in this section are
deltas to fold into §2/§3/§4/§5 when the implementation issue lands; the
tables above still describe the code on main.

Trigger — live incident 2026-07-07: two codex runs sat ~2 hours at the
codex login screen ("Sign in with ChatGPT") while `orch ps` reported
`running` / ALIVE yes and `orch wait` never fired. Same class: codex
workspace-trust prompt ("Do you trust the contents of this directory?"),
claude folder-trust prompt, account-credit dialogs.

### 9.1 The gap

A pre-agent interactive gate renders a full-screen dialog that matches
neither the busy markers nor the prompt patterns of `IsWaitingForInput`
(`internal/agent/manager.go`), and its pane content is stable, so
`stepCaptured` derives no verdict at all:

```
tick   capture pane              prompt reading      stepCaptured outcome
──────────────────────────────────────────────────────────────────────────
t0     "Sign in with ChatGPT…"   false (no pattern)  hash new → (already running)
t1     same                      false               hash same, no prompt → no effect
t2…tN  same                      false               no effect — forever
──────────────────────────────────────────────────────────────────────────
ps: running / ALIVE yes          orch wait: never fires          2h stall
```

The run is indistinguishable from productive work in every client. This is
a *false negative* of the waiting detection: the agent is waiting for human
input, just not at its normal composer prompt.

### 9.2 Observation definition — O4e gate reading (Q1)

Choice space and decision:

| option | verdict | rationale |
|--------|---------|-----------|
| new top-level obsKind (`obsGate`) | rejected | same source, cost, and pacing as O4 capture; a separate kind would need its own gathering loop and duplicate the capture pacing mechanism for no policy gain |
| fold gate strings into the existing promptPatterns | rejected | loses the gate *kind* (no reason on the event), and the remedies differ (`orch send` clears a prompt, only `orch attach` clears a gate); the false-positive budget (9.5) also demands stronger per-pattern evidence than the generic prompt heuristics carry |
| **O4e derived reading + kind-tagged input-request streak** | **adopted** | a gate is another *derived reading of a captured pane* (peer of O4a–O4d); the debounce is the existing prompt-streak mechanism generalized over reading kinds |

Definition. `agentSignal` gains one field:

- `Gate string` — the gate kind detected on this capture (`""` = none),
  produced by a new gather-side method `AgentManager.DetectGate(output)`.
  The busy-marker veto of `IsWaitingForInput` applies to gate detection
  identically (a pane showing "esc to interrupt" is never a gate).

`runCore` generalizes the prompt streak into a kind-tagged input-request
streak: `{PromptStreak int}` → `{ReadingKind string; ReadingStreak int}`
with `ReadingKind ∈ {"", "prompt", "gate:<kind>"}`. Consecutive captures
with the same reading advance the streak; any different reading (including
"" = busy/none) resets it. Both fields are **ephemeral by law** (§7 D-C1
list amended): reset on restart, bounded re-convergence, delay-only.

So the answer to "new observation kind or generalization?" is precisely:
*at the taxonomy level a new derived reading inside O4; at the debounce
level a generalization of the L6 prompt streak* — L6 becomes the
`reading = prompt` special case of L10a below.

### 9.3 Status mapping (Q2)

| option | verdict | rationale |
|--------|---------|-----------|
| new status `gated` | rejected | the status vocabulary is a closed set with real drift cost (§8): every client, the proto map, and the W9 projection must learn it; it buys nothing a reason does not |
| keep `running`, surface in ps TOPIC | rejected | TOPIC is issue-plane, not run-plane; `orch wait` and Slack listeners key off status transitions, and the incident's core complaint is that neither fired |
| `unknown` + reason | rejected | the state is precisely known — this is the opposite of unknown |
| **`waiting` + reason `gate_<kind>`** | **adopted** | it *is* waiting for human input; `orch wait` fires, listeners notify, ps renders `waiting(gate_login)` inline — the k8s phase+reason pattern already adopted in §5 (`launch_<step>` family precedent) |

Reason vocabulary delta for the §5 table:

| reason | emitted by | operator response |
|--------|-----------|-------------------|
| `gate_<kind>` (`gate_login`, `gate_trust`, `gate_credit`, …) | O4e gate reading confirmed by L10a | `orch attach` and complete the gate interactively; `orch send` will NOT clear it — the run re-asserts `waiting(gate_<kind>)` while the gate persists |

**D-G1 — same-status no-op compares (status, reason).** Today
`commitRunStatus` no-ops on equal *status*. A run already `waiting`
(reason `""`, normal prompt) that then hits a gate would keep a stale
reason forever under status-only comparison. Decision: the no-op predicate
becomes the pair `(status, reason)`; a confirmed reading change re-appends
and fires the listener once (L8 unchanged: fire iff append committed).
Oscillation is bounded by the streak: flipping the reason requires a full
threshold of consecutive opposite readings (L10a), which means the pane
content itself changed — i.e. someone interacted.

Recorded asymmetry with agent-prompt waiting: O6 feedback (`orch send`)
resumes `waiting → running` and resets the streak exactly as today, but a
standing gate re-confirms after one threshold and the run returns to
`waiting(gate_<kind>)`. This is self-correcting and intentional — it is
the signal that send did not help. (Note: sending Enter into a trust
prompt could *accept* it; whether send should refuse gated runs is a
policy question deliberately left out of this core — it belongs to the
send path, not the transition matrix.)

### 9.4 Where the patterns live (Q3)

| option | verdict | rationale |
|--------|---------|-----------|
| runtime-loadable config (patterns as shipped data files) | rejected (v1) | an untested pattern violates the false-positive budget: config files cannot carry the mandatory pane fixture that proves a pattern against a real screen; also adds a distribution/merge mechanism. Revisit only if gate churn across agent versions becomes frequent enough to outpace releases |
| per-backend code heuristics (another `IsWaitingForInput`-style function) | rejected | contribution requires understanding heuristic precedence; rows cannot be mechanically audited for the conjunction arity the FP budget demands |
| **compiled-in declarative table, per backend** (`internal/agent/gates.go`) | **adopted** | contribution = append one row + one fixture; a meta-test audits every row mechanically |

Row shape (design-level): `{Kind string; All []string}` — `All` is a
conjunctive set of **≥ 2** lowercase substrings that must co-occur within
the last 40 lines of the pane (same window as `IsWaitingForInput`).
Mandatory per row, enforced by a table-driven meta-test:

- ≥ 2 conjunctive substrings (no single-string rows);
- ≥ 1 positive fixture: a **real captured pane** checked into testdata —
  patterns must never be written from memory of what a screen "probably
  says";
- the fixture asserts the full reading precedence end-to-end (the gate
  wins over exited/completed/api-limited/failed on that pane), not just
  the substring match.

Backends contribute rows without touching any writer surface: `DetectGate`
is gather-side observation vocabulary; every transition still flows
through `stepRun` → `commitRunStatus`. opencode contributes no rows — it
is observed via its server API, and its login problems surface as O8
bootstrap failures (`launch_opencode_server` / `launch_opencode_bootstrap`).

### 9.5 False-positive budget and laws (Q4)

Cost asymmetry: a misfire flips `running → waiting` wrongly (spurious
wait/notify, and automated feedback flows may write into a busy agent); a
miss reproduces the incident (hours of invisible stall). The evidence
stack for a gate verdict is therefore:

```
busy veto  ∧  conjunctive row (≥2 co-occurring substrings, last 40 lines)
           ∧  same-kind streak ≥ waitingPromptStreakThreshold
```

Output-hash stability was considered as an additional gate (a real gate is
a static screen) and **rejected as a requirement**: gates that animate
(e.g. a spinner while polling for browser sign-in) would never confirm,
and that false negative *is* the incident class this section exists to
kill. It remains available as an optional per-row strengthening flag if a
specific row proves noisy in practice.

| Law | Statement |
|-----|-----------|
| L10a gate debounce (generalizes L6) | a `waiting` verdict with reason `gate_<kind>` requires ≥ `waitingPromptStreakThreshold` consecutive captures whose input-request reading is the same `gate:<kind>`; any busy capture or different reading resets the streak. L6 is the `reading = prompt` special case |
| L10b reading precedence | busy veto > gate > {exited, completed, api-limited, failed} > prompt > output-changed. A curated conjunctive row outranks the loose generic heuristics; each row's fixture locks its intended winner. (This deliberately lets a future `gate_credit` row override `IsAPILimited` on account-credit dialogs — a credit dialog needs a human, not a rate-limit wait) |
| L10c reason fidelity | the `waiting` reason always reflects the latest *confirmed* reading; a confirmed reading change re-appends under D-G1 and fires exactly one listener event (L8) |

Counterexamples (must become fixtures/property tests at implementation):

1. **Incident replay**: codex login pane, stable, no busy marker →
   `waiting(gate_login)` within ~2 capture ticks (≈30–45 s at remote
   pacing). The old behavior (no verdict, 2 h stall) is the bug.
2. **Busy transcript quoting gate text**: an agent working on gate
   detection prints "Sign in with ChatGPT" into its transcript while
   "esc to interrupt" is visible → busy veto, no gate, streak reset.
3. **Idle agent whose last output quotes gate text** at its normal
   composer prompt: conjunctive rows must require screen-only co-strings;
   if a row nevertheless misfires, the status is still `waiting`
   (correct — the agent *is* idle), only the reason is wrong. Bounded
   residual harm, recorded.
4. **Alternating readings** (prompt ↔ gate): the status flips at most once
   per threshold of ticks and only tracks confirmed readings (L10a);
   reasons never flap tick-by-tick (D-G1 + streak).
5. **Gate completed by the user**: output changes, reading changes →
   streak reset → `running` on the next changed capture. No stale
   `waiting(gate_*)` persists.

### 9.6 Follow-up (do not implement in this issue)

One implementation issue, **frontier + human** (touches `step.go`,
`stepCaptured` precedence, `commitRunStatus` no-op predicate, and the
`AgentManager` interface — all core surfaces): `agentSignal.Gate`,
`DetectGate` + gate table + fixtures + meta-test, kind-tagged streak in
`runCore`, D-G1 pair no-op, L10 property tests in `step_test.go`, §2/§3/
§4/§5 fold-in. Real gate screens must be captured for fixtures *before*
the pattern rows are written.

## 10. Observation-channel health — death verdicts require attestation (designed 2026-07-07)

Status: **design — accepted pending human review; no implementation yet.**
Deltas to fold into §2/§3/§4/§5 at implementation time.

Trigger — live incident 2026-07-07 22:53: two ALIVE runs (agents actively
working, sessions intact on the default tmux server) were marked `failed`
after "remote session not found on worker (3/3)". The freshly restarted
worker process had inherited `TMUX=<agent-deck socket>` (pre-#483 binary
rebuilds env in the managed spawn path), so its has-session/capture looked
at a **different tmux server** than the one hosting the sessions:

```
default tmux server            agent-deck tmux server
┌────────────────────┐         ┌────────────────────┐
│ orch-xxx  (alive)  │         │ (agent-deck's own  │
│ orch-yyy  (alive)  │         │  sessions)         │
└────────────────────┘         └─────────▲──────────┘
          ▲                              │ TMUX inherited
          │ nobody looks here            │
    old worker (gone)              new worker ──"not found"×3──► master ──► failed ✗
```

The daemon treated three consistent not-found responses from a
**misconfigured observer** as evidence of death. Two aggravating facts:

- `isRemoteSessionGone` (`internal/daemon/monitor.go`) classifies by
  English substring matching (including "no server running") — fragile
  across locales, and it conflates *observer misconfiguration* with
  *session death*.
- Git corroboration is structurally blind for worker-hosted runs: the
  master's `gatherGitEvidence` cannot see the worker-side worktree, so
  `HasUncommittedChanges`/ahead-count return nothing even while the agent
  is mid-commit.

### 10.1 Principle

**A death verdict requires positive evidence from an attested observation
channel. N consecutive not-founds from a channel that has never seen this
run alive is testimony, not evidence.**

L3' is the run-scoped special case (never alive *anywhere* ⇒ at most
`unknown`); this section generalizes it to per-(run, observer-instance)
attestation. The unifying reading: `failed` claims "the work stopped";
`unknown` claims "we cannot see". A channel that never saw the session can
only ever support the second claim.

### 10.2 Observation definition — O3 refined

`obsSessionGone` (and the `obsGitEvidence` follow-up it triggers) gains
structured fields; `obsSessionAlive` gains the first:

- **ObserverID** — identity of the observing channel *instance*:
  `worker:<worker_id>:<instance_nonce>` for lease-routed observation
  (nonce freshly generated per worker process start, carried **in the
  lease result payload**, not looked up from the registry — the registry
  preserves `RegisteredAt` across re-registration and a lease answer may
  race a re-register), or `local:<daemon_instance_nonce>` for local mux
  observation. No such generation concept exists today; this introduces
  it.
- **GoneClass ∈ {session_absent, server_absent, unclassified}** — decided
  **where the facts are local** (the worker for remote runs, the daemon
  shell for local runs) and shipped as structured data. `session_absent`:
  the mux server responded and the target session is missing.
  `server_absent`: no mux server on the socket this observer controls.
  `unclassified`: legacy/unknown responses. GoneClass is *diagnostic
  payload* (logs, error artifacts, operator hints) — the verification
  predicate is attestation (10.3), not the class. This retires
  `isRemoteSessionGone`'s substring matching (L11d).

`runCore` gains two fields, both **ephemeral by law** (§7 D-C1 amended;
reset direction analyzed in L7' below):

- `AliveObserver string` — ObserverID of the most recent successful
  alive/capture observation of this run ("" = none since restart).
- `DeadCheckObserver string` — ObserverID owning the current dead-check
  streak.

### 10.3 Transition law

| Law | Statement |
|-----|-----------|
| L11a streak continuity | `DeadCheckCount` accumulates only while the observation's ObserverID equals `DeadCheckObserver`; a gone observation from a different observer restarts the streak at 1 (and re-owns it). A dead-check streak is evidence only within a single observer generation |
| L11b attestation | the no-evidence fallback death verdicts (`failed`; opencode's `unknown(session_lost)`) require the concluding observation's ObserverID == `AliveObserver` ≠ "" — *the channel pronouncing death must be the channel that last saw this run alive*. Evidence-ladder verdicts (done/canceled/pr_open/waiting from PR/git facts) are channel-independent and unaffected |
| L11c unverified fallback | the same standing evidence from an unattested channel concludes at most `unknown(observer_unverified)` once the dead-check threshold has passed (never-alive runs additionally keep the L3' grace). Never `failed`. Recovery is automatic: `unknown` is non-terminal, and any successful capture re-attests the channel (sets `AliveObserver`) and resumes normal inference (O4 output-changed → running, O6 feedback → running) |
| L11d classification locality | gone-class and observer identity are decided at the observing side and travel as structured payload; master-side natural-language matching on error strings is retired. Legacy responses without observer context are `unclassified` ⇒ unattested ⇒ at most `unknown` (safe mixed-version degradation; the version handshake surfaces the stale worker) |
| L7' verdict-strength monotonicity (amends L7) | a restart of the master or of an observer may *soften or delay* a would-be `failed` into `unknown(observer_unverified)` (until the channel re-attests); it must never strengthen, terminalize, or invent a verdict. L7's target-equality clause is relaxed exactly this far and no further |

Reason vocabulary delta for the §5 table:

| reason | emitted by | operator response |
|--------|-----------|-------------------|
| `observer_unverified` | L11c: dead-check threshold reached through an unattested channel | the observer cannot see the session — check the worker/daemon mux environment (TMUX socket, #483 class) and worker process; the run self-recovers on the next successful capture |

Invariant delta for the §4 table:

| # | Invariant |
|---|-----------|
| I9 | no `failed` is ever written on the sole testimony of a channel that never observed this run alive |

### 10.4 Choice space of Addendum 2, closed

| candidate | verdict | rationale |
|-----------|---------|-----------|
| (a) reset DeadCheckCount when the observer generation changes | adopted (L11a) — but **insufficient alone**: in the incident all three not-founds came from the *same new* worker instance; a reset at the generation boundary changes nothing there | correct streak hygiene, wrong lever for the incident |
| (b) corroborating evidence before `failed` | adopted in two parts: the PR/git evidence ladder already corroborates and stays first (channel-independent); worker-side git evidence for worker-hosted runs is a **separate follow-up issue** (the master is structurally blind to the worker's worktree — `get_diff_stats`/`get_branch_state` capabilities already exist to close this). The "session-created-by-worker generation match" variant is subsumed by attestation: a creator observes its own session immediately, so it attests itself | |
| (c) observer changed + not found ⇒ `unknown`, not `failed` | adopted in generalized form: the predicate is not "changed" but "**unattested for this run**" (L11b/L11c) | "changed" misses the incident (the new observer's streak was internally consistent) |
| channel-level attestation ("observer saw ≥ 1 session of any kind") | **rejected** — two killing counterexamples: (i) the poisoned socket can contain *foreign* sessions (agent-deck's own), so ListSessions non-emptiness attests nothing; (ii) a poisoned worker that *launches a new run* creates and sees that session on the wrong server, attesting itself channel-wide while still blind to every pre-existing run. Attestation must be per-(run, observer-instance) | this is the cheaper design that almost worked; recorded so it is not re-proposed |

### 10.5 Counterexamples

1. **Incident replay**: restarted worker, poisoned TMUX → its not-founds
   carry a fresh ObserverID with `AliveObserver` pointing at the dead old
   instance (or "" after a master restart) → unattested → after 3 checks
   `unknown(observer_unverified)`, never `failed`. Operator fixes the env
   / worker restarts clean → first successful capture re-attests →
   `running`. No terminal status was ever written.
2. **Legitimate death under a steady observer**: the worker that has been
   capturing the run all along reports not-found ×3 → ObserverID ==
   AliveObserver → attested → evidence ladder → `failed` (or the ladder's
   done/waiting). Behavior unchanged from today.
3. **tmux server exits because the last session closed** (was-alive run,
   no work product): same long-lived observer reports `server_absent` →
   still attested (class is diagnostic, not the predicate) → `failed` as
   today. GoneClass only changes the log line.
4. **Worker restart races a genuine run death** (single-run host): the new
   instance never saw the run alive → `unknown(observer_unverified)`
   instead of `failed`. Accepted softening under L7' — a false `failed`
   (the incident) is strictly worse than a delayed `unknown` that a human
   or `orch repair` resolves.
5. **Master restart mid-streak**: `AliveObserver`/`DeadCheckObserver`/
   `DeadCheckCount` reset → verdicts delayed until the channel re-attests;
   for a genuinely dead session the channel never re-attests and the run
   concludes `unknown(observer_unverified)` rather than `failed` —
   accepted under L7' (today's fold-derived `WasAlive` would have allowed
   `failed` on hearsay; that is exactly the pattern this section retires
   for fallback verdicts).
6. **Mixed-version fleet**: an old worker reports plain error strings →
   `unclassified`, no ObserverID → unattested → at most `unknown`. Safe
   degradation; upgrade restores `failed` capability.

### 10.6 Follow-ups (do not implement in this issue)

- Implementation issue, **frontier + human** (touches `step.go`,
  `monitor.go`, `worker_plane.go`, proto lease payloads — core surfaces):
  worker instance nonce + observer context in lease results, local-plane
  observer identity, `runCore` fields, L11 arms in `stepSessionGone`/
  `stepGitEvidence`, retirement of `isRemoteSessionGone`, L11/L7'/I9
  property tests, §2/§3/§4/§5 fold-in.
- Separate issue (delegable once specced): worker-side git corroboration
  for worker-hosted dead verdicts via existing `get_diff_stats`/
  `get_branch_state` capabilities (closes the corroboration blindness
  noted in 10.0; benefits the attested path too).
- Cross-reference: this partially delivers the O12 (worker presence)
  backlog item of `docs/design/observation-coverage.md` by surfacing
  observer identity to `step()` as observation payload.
