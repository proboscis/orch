# Master Daemon + Event-Sourced Runtime — Implementation Plan

Reference: [specs/11-master-daemon-event-sourcing.md](./11-master-daemon-event-sourcing.md)

## Delivery Principles

1. Keep daemon as single source of truth for run/issue state.
2. Migrate in safe slices with dual-read compatibility where needed.
3. Preserve current CLI behavior until replacement path is proven.
4. Prefer additive schema/API changes before removals.

## Phase 0: Foundation (Project Registry + Runtime Schema Bootstrap)

**Goal**: Establish project config registry in XDG config and bootstrap event-store
tables in SQLite without changing all command behavior.

### Changes

| Area | Work |
|------|------|
| Project config | Add loader/writer for `XDG_CONFIG_HOME/orch/projects/<project_id>.yaml` |
| Admin flow | Update daemon project registration path to write YAML registry entries |
| Startup | Load project registry YAML entries into daemon context on start |
| Runtime DB | Create event/projection/outbox/idempotency tables in daemon DB migration path |

### Acceptance

1. `daemon repo register` creates/updates YAML project config.
2. Daemon restart keeps mapping via YAML project files.
3. SQLite contains required baseline tables for event-sourcing.

---

## Phase 1: Request Context (`project_id`, `request_id`) Introduction

**Goal**: Introduce explicit identity and idempotency context in protobuf APIs.

### Changes

| Area | Work |
|------|------|
| Proto | Add `RequestContext` and wire into runtime requests |
| Client | Populate `project_id` and generated `request_id` |
| Daemon | Validate context and reject missing/unknown `project_id` in new path |
| Compatibility | Keep old fields as fallback during migration window |

### Acceptance

1. Runtime requests include context in all new call paths.
2. Daemon emits clear errors for missing/invalid `project_id`.

---

## Phase 2: Handler Routing by `project_id`

**Goal**: Runtime handlers resolve project/workspace/store from project registry.

### Changes

| Area | Work |
|------|------|
| Resolution | Replace per-request path resolution in runtime handlers with `project_id` lookup |
| Store init | Resolve issues backend/path from project YAML + daemon-side config |
| Fallback | Remove remote dependency on `ORCH_PROJECT_ROOT` for runtime RPC resolution |

### Acceptance

1. Remote run/issue commands work with project mapping only.
2. No runtime remote RPC requires client-provided server path values.

---

## Phase 3: Event Appends + Projection Writes for Run Lifecycle

**Goal**: Start using event store and projections for run lifecycle transitions.

### Changes

| Area | Work |
|------|------|
| Event append | Add event append helper with stream versioning |
| Projection updates | Maintain `run_state` in same transaction where possible |
| Outbox | Queue side effects (`create_worktree`, `start_session`) in outbox |
| Idempotency | Persist/consult request idempotency keys for command retries |

### Acceptance

1. New run lifecycle emits ordered events.
2. `run_state` stays consistent with emitted events.
3. Retries with same `request_id` are deduplicated.

---

## Phase 4: Query Path Migration to Projections

**Goal**: Move `ps/show/list` reads to projection tables while preserving output contract.

### Changes

| Area | Work |
|------|------|
| Query path | Read `run_state` and related projection tables for query commands |
| Backfill | Rebuild projection from events when needed |
| Monitoring | Ensure daemon monitor updates feed events/projections |

### Acceptance

1. Query commands do not depend on scanning legacy run files for current state.
2. Projection rebuild command can recover query state from events.

---

## Phase 5: Deprecation and Cleanup

**Goal**: Remove path-coupled runtime request behavior.

### Changes

| Area | Work |
|------|------|
| Proto cleanup | Deprecate/remove runtime payload dependence on legacy path fields |
| Daemon cleanup | Remove remote fallback branches tied to env/path legacy behavior |
| Docs | Update architecture/config/daemon specs and CLI docs |

### Acceptance

1. Runtime remote behavior is fully project-id based.
2. Legacy path-coupled runtime routing is removed or fully gated/deprecated.

---

## Validation Matrix

| Scenario | Expected |
|---------|----------|
| Register project + daemon restart | Mapping persists and runtime commands continue to work |
| Start run with valid project_id | Event append + projection update + outbox entry |
| Retry same request_id | Idempotent response, no duplicate run creation |
| Unknown project_id | Deterministic error with remediation hint |
| Query after restart | Recovered from persisted projection/events |

## Risk Register

1. **Dual-source state drift** (legacy run files vs projections)
   - Mitigation: projection rebuild and temporary reconciliation checks.
2. **Migration complexity in handlers**
   - Mitigation: phased dual-read with explicit feature flags.
3. **Operational recovery concerns**
   - Mitigation: append-only events + deterministic replay tooling.

## Current Execution Focus

This implementation session starts with **Phase 0**.
