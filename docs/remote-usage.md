# Remote Usage with orch

> **Next milestone — out of beta scope.** Multi-host mode works and is used in
> development, but the current beta covers **single-machine use only**. Expect
> rough edges here and no support until the multi-host milestone lands.

This guide shows how to run orch against a remote daemon over TCP while keeping
the same daily `orch` workflow from your local machine.

## Overview

In remote mode, your local CLI sends requests to a server-hosted daemon. The
daemon is the source of truth for runs/issues state and resolves project
identity through repo mappings.

```text
Local client                         Remote host
┌──────────────────────────────┐      ┌────────────────────────────────────┐
│ orch CLI / monitor           │ TCP  │ orch daemon (--listen opt-in)     │
│ --remote master-host:7777    │─────▶│ repo mapping: repoid -> path      │
└──────────────────────────────┘      └────────────────────────────────────┘
```

## Prerequisites

1. orch installed on client and server.
2. Network route to server (VPN/Tailscale/SSH tunnel).
3. The project repository is available on each host that may execute work.

### Quick checks

```bash
# Client
orch --version

# Server
orch --version
```

## Server Setup

### 1. Opt in to the TCP listener (multi-host is opt-in — ADR-0003)

By default the daemon binds `127.0.0.1:7777` (loopback only), including when
an ordinary orch command auto-starts it. Nothing is reachable from other
hosts until you opt in. The TCP API is **unauthenticated**: whoever can
reach the port controls the daemon, so bind the narrowest interface that
works and keep port `7777` limited to trusted networks (VPN/Tailscale or a
firewall).

```bash
# On the server that should act as master — explicit multi-host opt-in:
orch daemon kill
orch daemon start --listen tcp://0.0.0.0:7777

# Better: bind a specific trusted interface (e.g. the Tailscale address)
orch daemon start --listen tcp://100.64.0.12:7777
```

The daemon logs a warning whenever it binds a non-loopback address. For an
SSH-tunnel-only setup, keep the loopback default and tunnel port 7777.

### 2. Register repository URL for remote resolution

```bash
# From client (or server), point to remote daemon
orch --remote master-host:7777 daemon repo register https://github.com/your-org/your-project.git
orch --remote master-host:7777 daemon repo list
```

If repo mappings are missing, remote commands fail with a store/project mapping
error.

## Client Setup

### Option A: per-command remote flag

```bash
orch --remote master-host:7777 ps
```

### Option B: environment variable

```bash
export ORCH_REMOTE=master-host:7777
orch ps
```

### Option C: persistent client config (recommended)

Global (`~/.config/orch/client.yaml`) or per repository
(`<repo>/.orch/client.yaml`, discovered by walking up from the current
directory). Per-repo values override the global ones field-wise
(`remote.default` when non-empty; host aliases merged, per-repo wins).

```yaml
# ~/.config/orch/client.yaml or <repo>/.orch/client.yaml
remote:
  default: primary
  hosts:
    primary:
      addr: master-host:7777
```

```bash
# Uses remote.default
orch ps

# Override with another alias
orch --remote cloud ps

# Bypass remote.default for one command (use local daemon)
orch --remote "" ps
```

## Running and Managing Work

```bash
# Start a run remotely
orch --remote master-host:7777 --project yourorg-yourrepo run my-issue

# Monitor runs
orch --remote master-host:7777 --project yourorg-yourrepo ps

# Attach/send/capture as usual
orch --remote master-host:7777 --project yourorg-yourrepo attach my-issue
orch --remote master-host:7777 --project yourorg-yourrepo send my-issue "please include tests"
orch --remote master-host:7777 --project yourorg-yourrepo capture my-issue
```

## Important Behavior in Remote Mode

- `--project` / `ORCH_PROJECT` provides project identity scope.
- Remote commands depend on daemon-side repo registration (`daemon repo register`).
- Before `orch run` dispatches to a remote master, the client idempotently
  starts its local managed worker and registers it to that master. The client
  host can therefore execute untargeted runs from that master.
- Set `ORCH_WORKER_AUTOSTART=0` on the client to disable that pre-dispatch
  worker start. The same setting on the master disables automatic startup of
  its colocated worker; workers must then be started manually with
  `orch worker start`.

## Troubleshooting

### Cannot connect to daemon

```bash
orch --remote master-host:7777 daemon status
```

Verify listener and network path to the remote host.

### "No store available" / project mapping errors

```bash
orch --remote master-host:7777 daemon repo list
```

If missing, register the repository URL on the remote daemon:

```bash
orch --remote master-host:7777 daemon repo register https://github.com/your-org/your-project.git
```

Then scope runtime commands by project identity (repo ID):

```bash
orch --remote master-host:7777 --project yourorg-yourrepo ps --json
```

### Issue resolution and worker project mappings

The master resolves the issue before dispatch and sends an issue snapshot in
the worker lease. A worker does not need a duplicate copy of the master's
issue file. If `run` reports `issue not found`, verify that the issue exists in
the selected project on the master:

```bash
orch --remote master-host:7777 daemon repo list
orch --remote master-host:7777 --project yourorg-yourrepo issue show my-issue
```

The executing worker does still need a local checkout registered under the
same project identity. If it reports `no local project mapping`, run this on
that worker host from the checkout:

```bash
orch --remote="" daemon repo register "$(pwd)"
orch --remote master-host:7777 worker status --json
```

## See Also

- [Configuration](./configuration.md)
- [CLI Command Reference](./reference/commands.md)
- [Getting Started](./getting-started.md)
- [Daily Workflow](./daily-workflow.md)
