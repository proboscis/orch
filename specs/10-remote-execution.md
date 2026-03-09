# Remote Execution

Status: Desired state

## Overview

orch supports remote execution with a strict three-plane model:

1. **orch-client** on the user's machine
2. **orch-master** as the control plane and single source of truth
3. **orch-worker** as the long-lived execution manager on each host

The master never depends on client-local paths. The worker owns host-local
execution. One worker may manage multiple active runs on its host.

## Architecture

```text
Client (MacBook)                  Master (Zeus)                     Target Host (Mac / Linux)
┌──────────────┐                 ┌──────────────────────┐          ┌──────────────────────┐
│ orch CLI/TUI │── TCP/RPC ────▶│ orch-master          │── RPC ──▶│ orch-worker          │
│              │                │  ├── project registry │          │  ├── git/worktrees   │
│ Control agent│                │  ├── run/issue state  │          │  ├── tmux/zellij     │
│ (local)      │                │  ├── scheduling       │          │  ├── agent sessions  │
│              │                │  └── event log        │          │  └── active runs     │
└──────────────┘                └──────────────────────┘          └──────────────────────┘
```

## Design Principles

1. **Master is authoritative**. All run and issue state is master-derived.
2. **Client is thin**. Client requests use `project_id` and never route by server path.
3. **Worker is host-scoped**. One worker represents one host/profile, not one run slot.
4. **Run multiplicity lives inside the worker**. A worker may manage multiple runs concurrently on its host.
5. **Target selection is explicit**. `--on <target>` chooses a host/profile, not an ad-hoc SSH destination.
6. **Control agent stays local**. Interactive planning remains on the client machine.

## Identity Model

Runtime routing uses only `RequestContext.project_id`.

```protobuf
message RequestContext {
  string project_id = 1;
  string request_id = 2;
  string client_id = 3;
}
```

Rules:

1. Client runtime RPCs must include `project_id`.
2. Master resolves `project_id` to server-local operational context via project registry.
3. `project_root` is operational data only, never the authoritative selector.
4. Path-derived fallback identity is forbidden.

## Project Registry

Master maintains a registry:

```text
project_id -> repo metadata -> workspace.root -> issue backend config
```

Typical admin flow:

```bash
orch --remote zeus:7777 daemon repo register https://github.com/acme/repo.git
orch --remote zeus:7777 daemon repo list
```

If a deployment intentionally uses an operational root instead of repo-URL
registration, that path is still an operational mapping owned by the master,
not a client-side runtime selector.

## Worker Model

Each execution host runs one long-lived `orch-worker` process (or one per host
profile when there is a real reason to separate capabilities).

Worker responsibilities:

- register with the master
- heartbeat continuously
- receive work assignments from the master
- manage multiple active runs on the host
- own host-local worktrees, sessions, and agent processes
- report events back to the master

Operational rule:

```text
repeated `orch worker start` on the same host/profile
  -> reuse/reconnect the same host worker
  -> do not create duplicate workers for routine operation
```

## Scheduling and Dispatch

Master assigns work to workers by host/profile.

Examples of master-to-worker effects:

- start run
- continue run
- stop run
- reconcile active sessions

The worker is not the source of truth for state. It is the source of execution.

```text
master
  -> assigns work to worker(host=mac)
worker(mac)
  -> updates host-local runtime
  -> reports events/results
master
  -> commits authoritative state
```

## Run Target Configuration

Runs specify where they execute via `--on <target>`.

```bash
orch run my-issue --on mac
```

Master-side config:

```yaml
targets:
  - name: mac
    host: mac
    repo: /Users/me/repos/project
  - name: zeus
    host: localhost
    repo: /home/me/repos/project
```

Semantics:

- `target.name` is the operator-facing selector
- `target.host` identifies the worker host/profile
- `target.repo` is the operational project root on that host
- empty target means the default worker on the master host

The `Run` model records:

- `target`
- `target_host`

## Client Connection

Client talks only to the master.

```bash
# Explicit
orch --remote zeus:7777 ps

# Environment
ORCH_REMOTE=zeus:7777 orch ps

# Client config
~/.config/orch/client.yaml
```

Example client config:

```yaml
remote:
  default: zeus
  hosts:
    zeus:
      addr: zeus:7777
```

## Attach / Capture / Send

Client operations target the run, not the worker directly.

Behavior:

- `orch attach` uses `target_host` when the run is remote
- `orch capture` reads through master-derived run metadata
- `orch send` targets the run session selected by master state

```text
client -> master -> run metadata -> target host session
```

## Control Agent

The control agent always runs on the client machine.

Master provides:

- prompt content
- selected agent backend
- model / variant
- extra args

Client owns:

- local control-agent session
- local control-session persistence
- local control UI lifecycle

## Monitor

`orch monitor` connects to the master for runs/issues state.
The control pane remains local.

Desired monitor semantics:

- monitor uses `project_id`
- monitor registration is keyed by project identity, not path
- monitor session naming is worker/identity aware, not path-hash based

## Command Surface

| Command | Purpose |
|---------|---------|
| `orch master ...` | control-plane lifecycle and admin commands |
| `orch worker ...` | host-worker lifecycle |
| `orch run --on <target>` | dispatch a run to a host/profile |
| `orch attach` | attach to the run session on its host |
| `orch ps/show` | always read master-derived state |

## Invariants

1. All runtime state is authoritative on the master.
2. Runtime routing uses `project_id`, never raw path.
3. Each host/profile has one long-lived worker in normal operation.
4. One worker may manage multiple active runs on its host.
5. Spawning duplicate workers on the same host is not a scaling strategy.
6. Target hosts need `orch-worker`, git, the configured multiplexer, and agent binaries.
7. Control agent remains client-local in both local and remote deployments.

## Validation Matrix

| Scenario | Expected |
|---------|----------|
| `orch worker start` twice on same host | same host worker reused |
| Two runs on same host | both active, one worker |
| `orch run --on mac` | run executes on Mac worker |
| `orch ps/show` | derived from master state only |
| Unknown `project_id` | fail-closed with registration guidance |
| Client without `--remote` but client.yaml default | master connection still resolves correctly |

## Non-Goals

- multi-master consensus
- path-based runtime routing
- one-worker-per-run operational model
- client-derived server execution paths
