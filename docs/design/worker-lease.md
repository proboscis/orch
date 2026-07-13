# Worker Lease — Survey and Law Candidates

Status: E1/E2 verified; maintained as the worker-lease law record
Date: 2026-06-12; updated 2026-07-13 (LL7)

Mirror of `run-state-machine.md` §1–4 for the second coupling core: worker
lease ownership, heartbeats, and their coupling to the run state machine.
Sources: code as of `feat/coupling-core-phase-b`, commits 7bfbcf88
(heartbeats during effects, fail leases fast) and 88ef7dc5 / PR #457
(run snapshots as mutation SSOT).

## 1. Lease write surface

All lease state lives in `SocketServer.workerLeases` (map, RWMutex) and
`SocketServer.workers` (map, RWMutex). Writers:

| # | Site | Writes | Lock |
|---|------|--------|------|
| LW1 | `worker_plane.go:411-448 acquireWorkerLease` | new lease (LeaseID, WorkerID, LeasedAt; DispatchCount=0) | workerLeasesMu |
| LW2 | `worker_plane.go:553-588 leaseWorkForWorker` | DispatchCount++, DispatchedAt, ExpiresAt=now+60s, PayloadJSON | workerLeasesMu |
| LW3 | `worker_plane.go:883-907 acknowledgeWorkerLease` | Completed, CompletedAt, Success, Error, ResultJSON | workerLeasesMu |
| LW4 | `worker_plane.go:240-256 heartbeatWorker` | worker.LastHeartbeat=now, Active=true | workersMu |
| LW5 | `worker_plane.go:236 unregisterWorker` | delete from workers map | workersMu |

Readers that decide policy: `waitForWorkerLeaseCompletion`
(`worker_plane.go:831-881`; poll 100ms, deadline, and since 7bfbcf88 a
per-iteration `workerIsActive` check that fails the wait early), and
`selectActiveWorkerForEffect` (worker choice at acquire time).

Entry points: every host-routed RPC goes through `withWorkerLease`
(`worker_plane.go:909-921`, acquire + wait, 10min timeout) —
start_run, continue_run, stop_run, capture_session, send_message,
get_diff(-stats), get_branch_state, run_worktree (`proto_handler.go` and
`run_worktree.go`), plus `captureRunOutputViaWorker`
(`proto_handler.go:819-823`, 15s capture timeout per `monitor.go:42`).

## 2. Ownership / lifecycle

- **Master owns the lease record**: creates (LW1), dispatches (LW2),
  observes completion (LW3 via worker ack), and times out. The worker owns
  only the *execution*; it reports back via `handleProtoAcknowledgeEffect`.
- **Master restart**: `workerLeases` and `workers` are reset in the
  constructor — leases have no persistence (see §5). In-flight RPC waits
  die with the process; workers re-register/heartbeat and the next
  dispatch starts clean.
- **Worker death**: heartbeat goes silent → `workerIsActive` false after
  30s → `waitForWorkerLeaseCompletion` fails fast with
  "worker lost while executing lease" (`worker_plane.go:871-876`,
  7bfbcf88) instead of burning the full 10min timeout.
- **Worker graceful shutdown**: UnregisterWorker RPC → removed from the
  workers map → same observable outcome as heartbeat expiry.

## 3. Heartbeat semantics

| Constant | Value | Site |
|----------|-------|------|
| workerHeartbeatTTL | 30s | worker_plane.go:18 |
| workerLeaseTTL (dispatch) | 60s | worker_plane.go:19 |
| lease wait timeout | 10min | withWorkerLease |
| remoteCaptureLeaseTimeout | 15s | monitor.go:42 |

Since 7bfbcf88 the worker sends heartbeats from a dedicated goroutine, so
a long-running effect (e.g. a hanging zellij call) no longer silences the
heartbeat and masquerades as worker death; conversely the master checks
liveness on every wait iteration, so a genuinely dead worker is detected
in ≤30s rather than 10min.

## 4. Coupling to the run state machine

`step()` never sees leases directly — by design. Lease failures reach the
run state machine only as *observations*:

```
lease failure (capture)        lease failure (start/continue/stop)
        │                                    │
        ▼                                    ▼
 obsSessionGone et al. ──► step() ──►  RPC error to the caller; launch
 (dead-check ladder,                   ladder writes failed (W2–W5) or
  git-evidence verdict)                the caller retries
```

This is the correct mechanism/policy split and must be preserved by E2/E3:
**a lease is infrastructure; only its observable consequences are policy.**

Worktree facts obey the same boundary. A run worktree belongs to its execution
host, not to the master that stores the run record:

```
ObserveSingleRunWorktree(r) = stat(worker(r.TargetHost), r.WorktreePath)
CleanWorktree(r)            = remove(worker(r.TargetHost), repo(r), r.WorktreePath)
worker unavailable or observation error => explicit error
                                           != WorktreeExists(false)
                                           != skipped("worktree already absent")
ListRuns.WorktreeExists(r) = unpopulated
```

The last inequality is the fail-clearly part of LL7: absence is a worker-local
observation, never a master-side inference from a path in another filesystem
namespace. Live inspection is deliberately confined to single-run reads and
mutations. List paths do not issue one synchronous lease per run: doing so
amplifies latency, saturates the worker lease plane under polling clients, and
makes one unavailable historical worker fail the whole list. Any future list
enrichment must use a batched per-host operation rather than per-run leases.

## 5. Store-of-record

- Lease records: **memory only**, by design (a lease is a short-lived
  coordination token, not domain state). Master restart forgets them; the
  bounded-timeout + heartbeat layers make this safe.
- Run mutations during delegation: the master sends a `RunSnapshot`
  (`run_snapshot.go`, PR #457) inside the lease payload; the worker
  operates on the snapshot instead of reading its own local store. The
  master store remains the client-visible SSOT, maintained via W9
  projections + W1 monitoring (see run-state-machine.md §1 notes on
  dual-write).

## 6. Invariant candidates and tripwire observations

- Copy-vs-pointer discipline: `acquireWorkerLease` returns a *copy* under
  the lock because `leaseWorkForWorker` mutates the stored pointer
  (comment at worker_plane.go:441). This is a documented manual-sync
  point — tripwire-adjacent; E2 should either law it or remove the
  aliasing.
- `WorkerRegistration.Active` is written by LW4 but liveness decisions go
  through `workerIsActive` (computed from LastHeartbeat) — the stored flag
  is a near-mirror of a derivable value. Candidate for removal (derive,
  don't mirror).
- No invariant currently asserts **single active holder per run**: nothing
  prevents two concurrent leases targeting the same run (e.g. a capture
  lease racing a stop lease). Whether that is benign must be decided by
  law, not by luck.

## 7. Law candidates (DRAFT — verify in E2)

| Law | Statement |
|-----|-----------|
| LL1 liveness detection | a dead worker is observed within workerHeartbeatTTL; no lease wait outlives a dead worker by more than one poll interval (7bfbcf88's contract) |
| LL2 dispatch monotonicity | DispatchCount is monotone; re-dispatch happens only after ExpiresAt; (DispatchedAt, ExpiresAt] intervals of one lease never overlap |
| LL3 completion finality | a completed lease never changes again (Completed is terminal; acks after completion are no-ops — currently unguarded, needs a check) |
| LL4 restart amnesia is safe | master restart ⇒ all leases forgotten; every caller observes failure within its timeout; no run transition is *decided* by lease state alone (only via observations, §4) |
| LL5 snapshot sufficiency | a worker executes any effect using only the lease payload (RunSnapshot); it never reads its local store for master-owned runs (PR #457's contract) |
| LL6 start target confinement | every `start_run` resolves a `TargetWorkerID` before lease acquisition; empty/`local` targets resolve to the master's host worker, named targets resolve through `config.targets`, and neither may fall back to another worker |
| LL7 worktree host confinement | single-run worktree inspect/remove executes on the run's target worker; an unavailable worker or failed stat/git operation is returned with host/cause and is never converted to `exists=false` or an absent skip; list paths leave worktree existence unpopulated pending a batched per-host operation |

## Verification and maintenance

LL1–LL6 are covered by `worker_plane_law_test.go` and the focused worker-plane
tests named in `coupling-core-roadmap.md`. LL7 is covered at both sides of the
boundary: `TestExecuteLeaseEffectRunWorktreeInspectsAndRemovesRegisteredWorktree`
executes against a real registered worktree, while
`TestRemoteGetRunReportsWorkerObservedWorktreeExists`,
`TestRemoteCleanRunWorktreeUsesExecutionHostWorker`, and
`TestRemoteGetRunWorktreeObservationFailureIsExplicit` prove master routing and
fail-clear propagation. Lease-map mutation remains confined by the
`worker-lease-mutation-surface` semgrep rule.
