# Orch Cluster Architecture — Implementation Plan

Reference: [specs/12-orch-cluster-architecture.md](./12-orch-cluster-architecture.md)

## Delivery Principles

1. Treat `orch-master` as the single source of truth at every phase.
2. Prefer breaking-cut correctness over temporary path-based compatibility.
3. Make `RequestContext.project_id` mandatory for runtime behavior.
4. Add semgrep guardrails before and during migration slices.
5. Validate each phase with integration/e2e checks (distributed + Zeus single-host).

## Phase 0: Naming Surface + Compatibility Entrypoints

**Goal**: Make master/worker terminology first-class while preserving operator ergonomics.

### Changes

| Area | Work |
|------|------|
| CLI command tree | Keep `orch daemon` as compatibility alias; add `orch master` and `orch worker` command roots |
| Docs | Update command references and architecture docs to primary `master`/`worker` terminology |
| Integration tests | Add outside-repo coverage for `orch master status` and `orch daemon status` equivalence |

### Acceptance

1. `orch master status` works outside a project root.
2. `orch daemon status` remains functional as compatibility alias.
3. Docs consistently describe daemon as alias for master.

---

## Phase 1: Runtime Identity Strictness (Breaking Cut)

**Goal**: Enforce `project_id`-based runtime routing and remove path/env runtime fallback behavior.

### Changes

| Area | Work |
|------|------|
| Proto contracts | Ensure runtime request messages include `RequestContext` (at minimum `project_id`) |
| Daemon handlers | Route runtime store/project resolution only through `RequestContext.project_id` |
| Error semantics | Return deterministic project-scoped errors for unknown/missing project mappings |
| Legacy fallback removal | Remove path/env-based runtime routing branches from handler code |

### Acceptance

1. Runtime RPCs fail closed when `project_id` is missing/unknown.
2. Runtime routing does not depend on legacy path fields or env fallback.
3. Existing runtime tests pass with explicit context semantics.

---

## Phase 2: Semgrep Enforcement for Cluster Identity Invariants

**Goal**: Encode the breaking-cut architecture invariants as mechanical guardrails.

### Changes

| Area | Work |
|------|------|
| Rule set | Add rules banning `resolveStoreFromProto(...)` usage in proto runtime handlers |
| Rule set | Add rules banning runtime fallback routing by legacy path fields in master handler paths |
| Rule scope | Exclude test files and keep messages tied to spec 12 intent |

### Acceptance

1. Semgrep fails when new path-based runtime fallback is introduced.
2. Current implementation satisfies new rules with zero violations.

---

## Phase 3: Worker Protocol Bootstrap

**Goal**: Introduce master<->worker protocol primitives while keeping current executor behavior available as a built-in worker profile.

### Changes

| Area | Work |
|------|------|
| Protocol | Add worker RPCs (`RegisterWorker`, `Heartbeat`, `LeaseWork`, `ReportEvent`, `AcknowledgeEffect`) |
| Master state | Add worker/lease tables and scheduling hooks |
| Worker runtime | Implement registration heartbeat loop and lease execution skeleton |
| Zeus mode | Support co-located master+worker with same protocol semantics |

### Acceptance

1. Worker can register and heartbeat to master.
2. Master can issue and track leases for runnable work.
3. Zeus single-host mode runs via same worker protocol path.

---

## Phase 4: Execution Offload and Master Decoupling

**Goal**: Make worker plane own execution side effects; master remains orchestration/state authority.

### Changes

| Area | Work |
|------|------|
| Scheduling | Route run start/continue/stop side effects through lease/outbox flow |
| Worker effects | Move git/worktree/session/agent execution to worker handlers |
| Master isolation | Remove assumptions that master host has local project checkout |

### Acceptance

1. Master can orchestrate projects not checked out locally.
2. Worker reports runtime events that drive master projections.
3. `orch ps/show` correctness remains master-derived.

---

## Phase 5: Legacy Cleanup

**Goal**: Remove obsolete daemon/path-coupled semantics after worker flow is stable.

### Changes

| Area | Work |
|------|------|
| API cleanup | Deprecate/remove legacy runtime path fields where no longer needed |
| Code cleanup | Remove dead fallback helpers and compatibility-only branches |
| Docs and examples | Finalize master/worker terminology and operational guides |

### Acceptance

1. Runtime orchestration uses project identity and worker protocol only.
2. No remaining path-coupled runtime routing in master handlers.

---

## Validation Matrix

| Scenario | Expected |
|---------|----------|
| Runtime request with known `project_id` | Routed to correct project store and succeeds |
| Runtime request with unknown `project_id` | Deterministic error with registration guidance |
| Runtime request missing context | Fail-closed error |
| Distributed deployment (master on host A, worker on host B) | Run lifecycle works via lease/event flow |
| Zeus single-host mode | Same semantics as distributed topology |

## Risk Register

1. **Partial context rollout causing mixed routing semantics**
   - Mitigation: semgrep enforcement + explicit handler-level fail-closed behavior.
2. **Compatibility regressions during breaking cut**
   - Mitigation: keep command aliases while hardening runtime contracts.
3. **Worker protocol rollout complexity**
   - Mitigation: phased bootstrap with built-in worker profile before full offload.

## Current Execution Focus

Current focus is **Phase 1 + Phase 2**: complete strict `project_id` runtime routing and add semgrep guardrails that prevent reintroduction of path-based routing.
