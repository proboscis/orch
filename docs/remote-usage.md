# Remote Usage with orch

This guide shows how to run orch against a remote daemon over TCP while keeping
the same daily `orch` workflow from your local machine.

## Overview

In remote mode, your local CLI sends requests to a server-hosted daemon. The
daemon is the source of truth for runs/issues state and resolves project
identity through repo mappings.

```text
Local client                  Remote host
┌──────────────────────┐      ┌────────────────────────────────────┐
│ orch CLI / monitor   │ TCP  │ orch daemon (--listen)            │
│ --remote zeus:7777   │─────▶│ repo mapping: repoid -> path      │
└──────────────────────┘      └────────────────────────────────────┘
```

## Prerequisites

1. orch installed on client and server.
2. Network route to server (VPN/Tailscale/SSH tunnel).
3. Project repository is available on the remote host.

### Quick checks

```bash
# Client
orch --version

# Server
orch --version
```

## Server Setup

### 1. Start daemon with TCP listener

```bash
# On remote server
orch daemon start --listen tcp://0.0.0.0:7777
```

### 2. Register project root for remote resolution

```bash
# From client (or server), point to remote daemon
orch --remote zeus:7777 daemon repo register /srv/repos/your-project
orch --remote zeus:7777 daemon repo list
```

If repo mappings are missing, remote commands fail with a store/project mapping
error.

## Client Setup

### Option A: per-command remote flag

```bash
orch --remote zeus:7777 ps
```

### Option B: environment variable

```bash
export ORCH_REMOTE=zeus:7777
orch ps
```

### Option C: persistent client config (recommended)

```yaml
# ~/.config/orch/client.yaml
remote:
  default: zeus
  hosts:
    zeus:
      addr: zeus:7777
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
orch --remote zeus:7777 run my-issue

# Monitor runs
orch --remote zeus:7777 ps

# Attach/send/capture as usual
orch --remote zeus:7777 attach my-issue
orch --remote zeus:7777 send my-issue "please include tests"
orch --remote zeus:7777 capture my-issue
```

## Important Behavior in Remote Mode

- `--project-root` is used to derive portable project identity.
- Remote commands depend on daemon-side repo registration (`daemon repo register`).

## Troubleshooting

### Cannot connect to daemon

```bash
orch --remote zeus:7777 daemon status
```

Verify listener and network path to the remote host.

### "No store available" / project mapping errors

```bash
orch --remote zeus:7777 daemon repo list
```

If missing, register the remote project root:

```bash
orch --remote zeus:7777 daemon repo register /srv/repos/your-project
```

## See Also

- [Configuration](./configuration.md)
- [CLI Command Reference](./reference/commands.md)
- [Getting Started](./getting-started.md)
- [Daily Workflow](./daily-workflow.md)
