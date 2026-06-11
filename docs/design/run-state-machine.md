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
| W1 | `monitor.go updateStatus` (sole monitor-plane writer) | all monitor inferences | terminal check, `CanTransitionStatus`, same-status no-op, auto-resolve issue on `done`, `publishRunEvent` |
| W2 | `socket.go handleStartRun` (legacy JSON path) | `queued` → (`failed` per setup step) → `booting` → `failed`/`running` | store-level only; append errors ignored |
| W3 | `socket.go processStartRunCore` (worker/master core) | same ladder as W2 | same |
| W4 | `socket.go processContinueRunCore` | same ladder | same |
| W5 | `socket.go handleContinueRun` (legacy JSON path) | same ladder | same |
| W6 | `socket.go markRunFeedbackSent` | `waiting/pr_open/rate_limited/unknown` → `running` | `feedbackResumesRun` predicate; fires `onRunFeedback` (PromptStreak reset), `PublishRunEvent` |
| W7 | `socket.go failOpenCodeRunBootstrap` | → `failed` | none |
| W8 | `socket.go appendRunCanceledByUser` (`orch stop`) | → `canceled` (`source=user`) | skip if terminal |
| W9 | `proto_handler.go syncStartRunResultToMasterStore` / `syncContinueRunResultToMasterStore` | `queued` (if record missing) + result status (default `running`) | store-level only |
| W10 | `socket.go handleAppendEvent` (external append API; agent self-report) | arbitrary, caller-supplied source | `CanTransitionStatus` with caller source |

Notes:

- W2–W5 are four near-copies of the same launch ladder (legacy + core ×
  start/continue) — the "same pattern repeated" crystallization signal.
- Worker-hosted runs are dual-written: the worker executes W3/W4/W6 against
  its **local** store (`worker_plane.go executeLeaseEffect`), while the master
  store receives a projection (W9) and is then maintained by the master's
  monitor (W1 via capture leases). There is no synchronization invariant
  between the two stores; the master store is the client-visible SSOT.
- `fireStatusChange` (Slack listeners) is invoked **only** for the
  agent-inference transition inside `processRunOutput` — not for PR
  merged/closed, dead-session verdicts, launch, feedback, or stop.

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
| O8 | launch lifecycle progress (worktree created, session created, …) | launch ladders | — |
| O9 | time | `now` vs `StartedAt` (boot grace), tick cadence, backoff timers | — |
| O10 | agent self-report | `handleAppendEvent` | — |

## 3. Current transition matrix (monitor plane)

States: `queued, booting, running, waiting, rate_limited, pr_open, unknown`
(active) and `done, failed, canceled` (terminal). The monitor only lists
non-terminal runs **except `queued`** (see I6).

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
       local,  never-alive                                         遷移なし (永遠に待つ — I7)
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
   上記の遷移時のみ fireStatusChange (Slack等)

O6 feedback delivered        status ∈ {waiting,pr_open,            → running (+PromptStreak reset)
                             rate_limited,unknown}
O7 user stop                 非terminal                            → canceled (source=user)
O8 launch progress           (launch ladder W2–W5)                 queued → booting → running / failed
```

## 4. Implicit invariants of in-memory monitor state (`RunState`)

| # | Invariant | Status |
|---|-----------|--------|
| I1 | `DeadCheckCount == 0` ⇔ last observation was alive; `>= 3` consecutive dead checks ⇒ verdict allowed | holds, but scattered across two paths |
| I2 | `WasAlive` is monotone (never unset) **within one daemon process** | violated across restarts: RunState is in-memory, so a restart resets boot grace, dead counts, and prompt debounce |
| I3 | `PromptStreak >= 2` required before `waiting` (debounce); reset on feedback | holds (W6 + `noteRunFeedback`) |
| I4 | duplicate status events must not accumulate | enforced only in W1 (same-status no-op); W2–W9 rely on transition legality alone |
| I5 | `PRRecorded` dedupes `pr` artifacts | in-memory only: after a daemon restart, a PR URL still visible in the pane appends one duplicate `pr` artifact |
| I6 | every non-terminal run is eventually observed | **violated for `queued`**: `monitorAll` does not list `queued`, so a run orphaned before `booting` (e.g. daemon crash mid-launch) is never monitored again |
| I7 | a gone session must eventually produce a verdict | **violated locally** for never-alive non-opencode runs: no unknown/failed verdict is ever issued (remote path issues `unknown` after grace — asymmetric) |
| I8 | status-change listeners observe every transition | violated: only the O4 agent-inference path fires `fireStatusChange` |

I2 is the standing argument for deriving monitor state from the event log
(state = fold) in a later phase; I5–I8 are candidate behavior fixes that are
**not** part of the v1 behavior-preserving refactor.

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
| L3 grace | a never-alive run within `neverAliveVerdictGrace` never receives an `unknown`/`failed` verdict, on any observation sequence (complement: after grace, remote concludes `unknown`; local keeps waiting — the §3 asymmetry, pinned) |
| L4 terminality | from a terminal status, `step` emits no effect and no core change for any observation |
| L5 verdict requires evidence | `obsSessionGone` never emits a status directly; it requests git evidence exactly when the dead-check threshold is reached |
| L6 debounce | `waiting` via prompt requires ≥ `waitingPromptStreakThreshold` consecutive prompt observations; any busy capture resets the streak |

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
| W2–W5 | launch ladders (socket.go ×4) | **integrate — v2, first** | Four near-copies of one ladder; interleaves with monitor-plane transitions (booting/running races). Becomes launch-progress observations (O8) decided in `step()`, with the imperative bootstrap steps as effects. |
| W7 | `failOpenCodeRunBootstrap` → failed | **integrate — with W2–W5** | The failure arm of the same ladder. |
| W6 | feedback → running | **integrate — v2** | Already mutates `runCore` (PromptStreak reset, O6). Core state must change only through `step()`. |
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

### D-C1 — restart transparency without new persistence

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

### D-C3 — never-alive verdict asymmetry resolved

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

I6 (queued runs are never monitored) remains a behavior fix tracked by the
coupling-core roadmap Phase C2: `monitorAll` must include `queued` so that
"every non-terminal run is eventually observed" holds.
