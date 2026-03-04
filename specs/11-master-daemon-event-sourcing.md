# Master Daemon + Event-Sourced Runtime

Status: Draft (v0)

## Why

Remote operation currently mixes two identities:

- **Logical identity**: which project the user means
- **Filesystem identity**: where that project lives on a specific host

For a multi-project master daemon, clients must only send logical identity.
Server-local filesystem paths must be resolved server-side from daemon-owned
configuration.

## Goals

1. Make `project_id` the primary identity for daemon APIs.
2. Keep durable daemon/project config in YAML under XDG config.
3. Keep runtime/operational state in SQLite under XDG data.
4. Introduce event-sourcing for run lifecycle and projection rebuildability.
5. Remove remote request resolution dependency on legacy project-root env fallback.

## Non-goals

- Rewrite all existing proto request shapes in one release.
- Remove file-based issue/run store immediately.
- Introduce distributed consensus or multi-master behavior.

## Architecture

```text
CLI Client
  |
  | protobuf (unix/tcp)
  v
Master Daemon
  |
  +-- Config Loader (YAML)
  |     XDG_CONFIG_HOME/orch/server.yaml
  |     XDG_CONFIG_HOME/orch/projects/<project_id>.yaml
  |
  +-- Runtime Store (SQLite)
  |     XDG_DATA_HOME/orch/master.db
  |     - events (append-only)
  |     - projections (run_state, issue_state, queue)
  |     - outbox
  |     - idempotency keys
  |
  +-- Executors
        git/worktree/multiplexer/session actions on server-local workspace
```

## Storage Contract

### 1) Config (user/admin managed)

Location:

- `XDG_CONFIG_HOME/orch/server.yaml`
- `XDG_CONFIG_HOME/orch/projects/<project_id>.yaml`

`server.yaml` (example):

```yaml
version: 1
listen:
  unix: true
  tcp: "0.0.0.0:7777"
auth:
  mode: token # token | mtls | none (dev)
runtime:
  db_path: "~/.local/share/orch/master.db"
  max_concurrent_runs: 8
```

`projects/<project_id>.yaml` (example):

```yaml
version: 1
project_id: proboscis-orch
display_name: orch
repo:
  origin_url: git@github.com:proboscis/orch.git
workspace:
  root: /srv/repos/orch
issues:
  backend: local
  path: /srv/repos/orch/issues
defaults:
  agent: opencode
  base_branch: main
  worktree_dir: /srv/worktrees
```

### 2) Runtime state (daemon managed)

Location:

- `XDG_DATA_HOME/orch/master.db`

Semantics:

- SQLite is daemon-owned; daemon is sole writer.
- WAL mode enabled for read concurrency.
- Runtime state survives daemon restart.

### 3) Logs and runtime artifacts

- `XDG_STATE_HOME/orch/daemon.log`
- `XDG_STATE_HOME/orch/daemon-stderr.log`
- `XDG_RUNTIME_DIR/orch/daemon.sock`, `daemon.pid`, `daemon.lock`

## Identity Rules

1. Client runtime RPCs use `project_id`.
2. Daemon resolves `project_id -> projects/<project_id>.yaml -> workspace.root`.
3. Client does not send server filesystem paths on normal runtime RPCs.
4. Remote handler resolution MUST NOT fallback to legacy project-root env values.

## API Direction

Introduce request context used by runtime RPCs:

```protobuf
message RequestContext {
  string project_id = 1;
  string request_id = 2; // idempotency key
  string client_id = 3;
}
```

Admin RPC family:

- `RegisterProject`
- `UpdateProjectConfig`
- `GetProject`
- `ListProjects`
- `DeleteProject` (optional, guarded)

Runtime RPCs (`StartRun`, `ContinueRun`, `ListRuns`, `ListIssues`, etc.) consume
`project_id` and resolve storage/workspace from daemon config.

## Event-Sourcing Model

### Event store

Append-only table keyed by stream type + stream id + stream version.

```text
stream_type: project | issue | run
stream_id:   <project_id> | <issue_id> | <run_id>
version:     monotonically increasing per stream
```

### Core run events

- `RunRequested`
- `RunQueued`
- `WorktreePrepared`
- `SessionStarted`
- `RunStatusChanged`
- `RunCompleted`
- `RunFailed`
- `RunCanceled`

### Projections

- `run_state` (for `ps` / `show`)
- `issue_state` (for issue-centric views)
- `queue_state` (for scheduling/limits)

### Outbox + idempotency

- `outbox` stores pending side effects (`create_worktree`, `start_session`, etc.)
- `idempotency_keys` deduplicates retried client requests by `request_id`

## Failure Semantics

1. Command accepted only after event append succeeds.
2. Projection update runs in same transaction when possible.
3. Side effects are tracked through outbox; retries append follow-up events.
4. Unknown `project_id` returns explicit actionable error.

## Migration Strategy (high-level)

```text
Phase A: Add project YAML registry and admin APIs
Phase B: Add RequestContext(project_id, request_id)
Phase C: Route runtime handlers by project_id (dual-read period)
Phase D: Introduce event tables + projections
Phase E: Move ps/show/list to projection reads
Phase F: Remove remote fallback to legacy project-root env and path-based runtime inputs
```

## Acceptance Criteria

1. `orch --remote <host> ps` works via project_id mapping only.
2. Daemon restart preserves project mappings and runtime queryability.
3. Run lifecycle can be reconstructed from event stream.
4. No runtime remote command requires passing server filesystem paths.
