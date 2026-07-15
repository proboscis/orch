# ADR-0002: Idempotent worker availability (auto-start on demand)

- Status: accepted (2026-07-09)
- Deciders: maintainer + implementation session
- Scope: worker plane, run dispatch, local & cluster modes

## Context

The daemon auto-starts on the first orch command (`internal/cli/root.go`,
`ensureDaemon`), but the worker — the process that actually launches agent
sessions — never does. `orch worker start` is *already idempotent*
(`worker.StartManaged` reuses a live managed worker, guards split-brain,
recovers stale PID files — `internal/worker/managed.go`), yet nothing invokes
it on demand. The result: on a fresh machine or after any reboot, `orch run`
fails with `no active workers available` (`internal/daemon/worker_plane.go`)
before a run record even exists, and the user must know to run
`orch worker start` by hand. This is a cluster-design remnant applied to the
local single-machine case, where daemon, worker, and checkout are all on the
same host and there is nothing left to choose.

Structural facts that shape the mechanism:

- At lease time the master knows both its own host identity
  (`defaultWorkerID()` = `host-<hostname>`) and the effect's resolved target
  (`preferredWorkerPreferenceForPayload`). "This run should execute on my own
  host" is decidable inside the master.
- The import direction is `internal/worker → internal/daemon`; the daemon
  cannot call `worker.StartManaged` without an import cycle.
- The daemon always binds its unix socket, even when additionally listening on
  TCP (`internal/daemon/socket.go`), so a colocated `orch worker start` (no
  remote) always reaches the very master that needs the worker.
- The daemon already spawns detached copies of its own binary
  (`daemon.StartInBackground` execs `orch daemon run`); exec-self is a
  precedented pattern in this codebase.

## Decision

Worker availability becomes a reconciled precondition instead of a manual
setup step, at two layers:

### 1. Master-side reconciler (owns the invariant)

**Invariant: a master can always execute a run that targets its own host.**

When lease acquisition fails with "no active workers" and the effect's target
is the master's own host — i.e. the resolved target worker ID equals
`defaultWorkerID()`, which includes the untargeted/local case — the master:

1. ensures a colocated managed worker by exec-ing its own binary:
   `<self> worker start --worker-id host-<self>` (detached CLI call, exit
   status observed). The process boundary avoids the import cycle and reuses
   *all* of `StartManaged`'s idempotency, split-brain guard, stale-PID
   recovery, and state/pid/log file conventions — the auto-started worker is
   indistinguishable from a hand-started one and remains manageable via
   `orch worker status` / `orch worker stop`;
2. waits (bounded) for the worker to register — `worker start` itself blocks
   until the master reports the worker Active, so a zero exit is the signal;
3. retries lease acquisition once. A single-flight guard ensures concurrent
   lease failures trigger at most one ensure.

If the target is a *different* host, behavior is unchanged: fail fast with the
instructive error (orch cannot spawn processes on foreign hosts).

This lives in the daemon because the daemon owns lease acquisition: every
entry path (`run`, `restart-from`, `continue`, monitor-initiated dispatch, any
future caller) is covered at one point, per-path client checks are not.

### 2. Client-side ensure for remote masters

When a run-dispatching CLI command (`run`, `restart-from`) talks to a remote
master (`ORCH_REMOTE` / `--remote` / `client.yaml remote.default`), the CLI
first ensures the local managed worker registered to that master
(`worker.StartManaged`, idempotent, exactly what `orch worker start` does).

Deliberate, accepted consequence: the client host becomes an eligible executor
for that master's untargeted runs (lease fallback is non-strict). The
maintainer chose this trade-off explicitly: one fewer manual step on every
operator machine outweighs the surprise of a laptop occasionally picking up an
untargeted run. Operators who want strict topology use the off-switch.

### 3. Off-switch

`ORCH_WORKER_AUTOSTART=0` disables the mechanism at whichever layer reads it —
on the master process it disables the reconciler, on the client it disables
the pre-dispatch ensure. Default: enabled.

### 4. Worker identity is host-local and profile-independent

**Invariant: one host has at most one live `orch worker run` process for a
given `worker-id`.** The master connection string is configuration of that
worker, not part of its identity: `zeus:7777`, `127.0.0.1:7777`, and the
local socket may all denote the same master, so no per-connection-string
record (the managed state files are keyed by worker-id + remote) can decide
whether a second process is a duplicate.

`worker start` and `worker stop` therefore reconcile by `worker-id` against
the live process table (`reconcileWorkerID` in `internal/worker/reconcile.go`)
instead of trusting profile state: every local process whose command line
claims the worker id — an exact `orch … worker run` subcommand match, with an
absent `--worker-id` flag resolving to the host default id — is a claimant.
The process-table check is required because an older binary generation, a
changed `--remote` spelling, or deleted state/PID metadata leaves a live
supervisor outside every current profile. `worker start` keeps only the
requested profile's known-live PID, stops every other claimant, and launches
only when no managed process remains. `worker stop` stops all claimants of
the id, including state-less ones; `worker stop --all` additionally sweeps
every remaining local `orch worker run` process. Lifecycle transitions for
one worker id serialize on a host-local file lock keyed by the id alone
(`acquireManagedWorkerIdentityLock`), so concurrent start/stop calls — even
with different `--remote` spellings — cannot interleave their
reconcile-and-launch sequences.

Master-side duplicate eviction is not the enforcement layer of this decision.
The worker protocol carries only `worker_id` on register, heartbeat, lease,
acknowledgement, and unregister calls; once two processes claim the id, the
master cannot tell which request belongs to which process, so it cannot
selectively shut down the older one without introducing a new
connection-instance identity across the whole lease protocol. Host-local
process ownership is already the managed lifecycle boundary and also reaps
older binaries that would not understand a new protocol response. The master
only surfaces the symptom: it logs a warning when a worker id re-registers
while the previous registration is still heartbeat-fresh.

## Resulting UX

| Mode | Before | After |
|---|---|---|
| local single machine | `orch worker start` required by hand, again after every reboot | zero manual steps: `orch run` self-heals |
| cluster, run targets master's host | manual `orch worker start` on the master | master self-heals |
| cluster, run targets other host | manual `orch worker start` on that host | unchanged (physical necessity), instructive error |
| cluster, client machine | manual `ORCH_REMOTE=… orch worker start` | auto on first `orch run` (per maintainer decision) |

## Consequences

- The auto-spawned worker inherits the master's (or client's) environment.
  A master auto-started from the user's shell carries the user's PATH — the
  normal local case. A systemd/nohup master with a minimal PATH may spawn a
  worker that cannot find or execute agent CLIs. At worker startup, orch probes
  every known adapter and logs a stable availability map plus the inherited
  PATH. A failed run identifies the evaluating worker and reports the exact
  probe command, exit status or lookup failure, and PATH, so the deciding
  worker environment is immediately diagnosable. Explicit `orch worker start`
  from an environment with the intended PATH remains the operator override.
- "worker not running" disappears as a user-visible error class on
  single-machine setups; docs and the embedded tutorial shrink accordingly.
- The reconciler runs strictly inside the lease-failure path: zero cost when
  workers are healthy, no background polling, no new periodic loop.
- Changing the configured master for a `worker-id` is a replacement, not a
  second concurrent worker: `worker start` with the new `--remote` stops the
  old supervisor before launching, so two processes never race for the same
  lease identity after a master restart. Running workers for two masters on
  one host requires two distinct worker ids.

### Environment normalization decision (2026-07-13)

The worker does **not** synthesize a login-shell environment or rewrite PATH at
spawn. It continues to inherit the environment of the process that launches
it.

Running a login shell is not a portable normalization rule: shell selection and
startup-file semantics differ across platforms, startup files can have
interactive side effects, and a service manager may intentionally constrain
PATH. Rewriting the environment would also make the startup probe describe a
different execution context from the one that launches agents. The invariant
is therefore: **probe and agent launch observe the same inherited environment**.
Operators configure PATH explicitly in their service/automation launcher, or
restart the managed worker from the intended shell environment; orch exposes
the mismatch rather than silently changing it.

## Alternatives considered

- **CLI-side ensure only** — rejected: `run` is not the only entry path
  (`restart-from`, `continue`, monitor); per-path imperative checks inevitably
  miss one. The layer that owns lease acquisition owns the invariant.
- **Park runs `queued` until a worker registers** — rejected for now: today no
  run record exists before lease success, and the run state machine +
  worker-lease draft laws (docs/design/worker-lease.md) would need a rework.
  Noted as possible future work for multi-host queueing.
- **Extract `StartManaged` into a shared package the daemon can import** —
  rejected: heavier refactor for no behavioral gain; exec-self is precedented
  (`daemon.StartInBackground`) and keeps one implementation of the managed
  worker lifecycle.

## Verification

- daemon unit tests (spawn seam injected): lease failure + self-host target →
  ensure invoked → registration → lease retry succeeds; other-host target →
  no spawn, error unchanged; `ORCH_WORKER_AUTOSTART=0` → no spawn;
  single-flight under concurrent failures.
- CLI: remote dispatch path calls the ensure before `StartRun` (seam test).
- e2e (scripted): fresh env with no worker → `orch run` completes the golden
  path with zero manual worker commands.
