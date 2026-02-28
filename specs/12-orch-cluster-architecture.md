# Orch Cluster Architecture (Master + Worker)

Status: Draft (v0)

References:

- [specs/10-remote-execution.md](./10-remote-execution.md)
- [specs/11-master-daemon-event-sourcing.md](./11-master-daemon-event-sourcing.md)

## Why

The current runtime is in a transitional state:

- API identity is moving to `project_id`, but some flows still carry local/path
  assumptions.
- "daemon" behavior still mixes local single-machine semantics with remote
  control-plane semantics.

We need an explicit cluster model where control-plane and execution-plane are
separate concepts.

## Terminology (Authoritative)

1. **orch-master**: control plane and source of truth.
2. **orch-worker**: execution plane for git/worktree/session/agent actions.
3. **orch-client**: CLI/TUI/control-agent process used by humans.
4. **project_id**: cluster-wide logical project identity.

Legacy term mapping:

- `daemon` -> `orch-master` (CLI alias during migration only)

## Goals

1. Make `project_id` the only runtime identity in cluster RPC contracts.
2. Make `orch-master` independent from client local filesystem/env assumptions.
3. Move execution responsibilities to `orch-worker`.
4. Support both distributed and single-host deployment with same semantics.
5. Keep `orch-master` as single source of truth for run/issue state.

## Non-goals

- Multi-master consensus in this phase.
- Removing compatibility aliases in one release.
- Rewriting every command UX at once.

## Cluster Topology

### A) Distributed

```text
orch-client
    |
    | request(project_id, request_id)
    v
orch-master  ------------------------+------------------------+
    |                                |                        |
    | schedule + lease               | schedule + lease       | schedule + lease
    v                                v                        v
orch-worker[a]                   orch-worker[b]          orch-worker[c]
```

### B) Single-host (Zeus mode)

```text
Host: zeus

  orch-master + orch-worker (co-located)
           ^
           |
       orch-client (local or remote)
```

Both modes must use the same protocol and lifecycle semantics.

## Responsibility Split

### orch-master owns

- Project registry (`project_id`, repo metadata, worker placement metadata).
- Run/issue state store (events + projections + idempotency + outbox).
- Scheduling and worker lease assignment.
- API for clients and admin operations.

### orch-worker owns

- Workspace/repo checkout and local worktree operations.
- Session process lifecycle (tmux/zellij/opencode/etc).
- Runtime side effects requested by master.
- Event reporting back to master.

### orch-client owns

- Human interaction and presentation.
- Sending commands by `project_id` only.
- Never deriving server execution path in runtime requests.

## Hard Invariants

1. Runtime requests are accepted only with valid `RequestContext.project_id`.
2. Master runtime handlers MUST NOT resolve identity from path/env fields.
3. Worker is never source of truth for run/issue status; master is.
4. Local and remote mode share identical runtime semantics.
5. `ORCH_PROJECT_ROOT` / `ORCH_ISSUES_ROOT` may exist for compatibility in
   client UX, but not for master runtime routing.

## API Direction

### Client -> Master

- `RequestContext { project_id, request_id, client_id }` on all runtime RPCs.
- No path-coupled routing fields in runtime contracts.

### Worker -> Master

Worker protocol (shape-level direction):

1. `RegisterWorker`
2. `Heartbeat`
3. `LeaseWork`
4. `ReportEvent`
5. `AcknowledgeEffect`

Master schedules; worker executes; worker reports events; master commits state.

## Storage Direction

Master DB (SQLite in current phase, single writer):

- `projects`
- `workers`
- `run_events`
- `run_projection`
- `issue_projection`
- `leases`
- `outbox`
- `idempotency_keys`

Worker local state is operational cache only and recoverable.

## Migration Plan (Cluster Cutover)

### Phase 1: Naming + Contract Freeze

- Introduce `orch-master`/`orch-worker` terms in specs/docs.
- Keep `orch daemon` as compatibility alias to master commands.

### Phase 2: Runtime Identity Strictness

- Complete `RequestContext.project_id` coverage for all runtime RPCs.
- Remove path/env fallback routing from runtime handlers.

### Phase 3: Worker Protocol Introduction

- Add worker registration/heartbeat/lease/event-report path.
- Keep existing local executor as a temporary built-in worker.

### Phase 4: Execution Offload

- Route run execution through worker lease model.
- Master no longer requires local project repo checkout.

### Phase 5: Legacy Daemon Semantics Removal

- Remove project-root-dependent daemon command behavior.
- Deprecate/remove path-coupled runtime fields.
- Keep local single-host mode as "master+worker on one host" profile.

## Acceptance Criteria

1. Master can run on a host with no project checkouts and still orchestrate runs
   on workers.
2. Starting and managing runs requires only `project_id` from client API.
3. Zeus single-host mode works as co-located `orch-master` + `orch-worker`.
4. Existing local UX remains usable via compatibility layer during migration.
5. Runtime correctness derives from master events/projections, not worker-local
   status files.

## Open Questions

1. Worker transport/auth model (`token` vs `mTLS`) and bootstrap flow.
2. Worker capability model (agent/model/tool availability constraints).
3. Queue fairness and placement policy (per-project quotas, priorities).
4. Rollout strategy for command renaming (`daemon` -> `master`).
