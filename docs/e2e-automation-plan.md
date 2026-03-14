# E2E Automation Plan

This file defines how the manual E2E surface should be automated.

## Principle

Every path in the manual E2E suite is automatable.

The real split is not:

```text
automatable vs manual-only
```

It is:

```text
safe/stable enough for PR CI
vs
needs external hosts, credentials, or paid backends
vs
manual fallback when those contracts are unavailable
```

## Lane Model

### 1. PR CI

Goal: catch regressions on every PR without depending on Zeus, GitHub write
access, or paid backend availability.

Must include:

- local single-host master/worker/client flow
- remote master reachability smoke
- same-machine target-host simulation
- backend smoke for deterministic local lanes
  - `tmux`
  - `claude` via shim
  - `codex` via shim
  - `zellij` when installed on the runner
- run-control matrix smoke for `attach` / `capture` / `send`

PR CI does not need to prove every paid or remote integration on every push.
It needs to fail fast on command-plane regressions with deterministic local
contracts.

Entrypoint:

- `scripts/e2e-pr-ci.sh`
- `scripts/e2e-run-control-local.sh`

Expected environment:

- local filesystem only
- no remote SSH requirement
- no GitHub write requirement
- no paid backend requirement

### 2. Nightly / Lab E2E

Goal: validate real distributed topology and real integrations.

Must include:

- real Zeus full flow
  - start master/worker
  - register repo
  - run
  - create PR
  - close PR
  - stop run
- real target-host flow
  - Zeus master
  - remote target worker
  - `--on <target>`
  - worker-local `project_id -> local repo root` registration on the target host
- full backend matrix where credentials are available
  - `tmux`
  - `zellij`
  - `opencode`
  - `claude`
  - `codex`
  - heredoc/stdin `orch send` checks for tmux/zellij and real Claude/Codex when enabled
- run-control matrix where `attach` / `capture` / `send` all work across host boundaries
- verify `orch ps` exposes the real execution host in `HOST` / `target_host`
- verify Zeus OpenCode sessions stay alive after session creation instead of flipping to `failed`

Entrypoints:

- `scripts/e2e-master-worker-client-zeus.sh`
- `scripts/e2e-master-worker-client-target.sh`
- `scripts/e2e-backend-matrix-smoke.sh` with real backend lanes enabled
- `scripts/e2e-run-control-zeus.sh`
- `scripts/e2e-run-control-matrix.sh`

Expected environment:

- SSH reachability
- real remote repo clones
- local repo registration on each worker host
- GitHub token/write access
- backend auth/config

### 3. Manual Fallback

Goal: preserve an operator procedure when automation contracts are unavailable.

Manual docs remain:

- [docs/e2e-master-worker-client.md](./e2e-master-worker-client.md)
- [docs/e2e-backend-matrix.md](./e2e-backend-matrix.md)

## Mapping From Manual Sections

| Manual section | Automatable | Lane | Entrypoint |
|---|---|---|---|
| `docs/e2e-master-worker-client.md` 1-5b | yes | PR CI | `scripts/e2e-master-worker-client-local.sh` |
| `docs/e2e-master-worker-client.md` 6 | yes | PR CI | `scripts/e2e-master-worker-client-remote-smoke.sh` |
| `docs/e2e-master-worker-client.md` 7 | yes | PR CI | handled by each script cleanup |
| `docs/e2e-master-worker-client.md` 8 | yes | Nightly / Lab | `scripts/e2e-master-worker-client-zeus.sh` |
| `docs/e2e-master-worker-client.md` 9 | yes | PR CI + Nightly / Lab | `scripts/e2e-backend-matrix-smoke.sh` |
| `docs/e2e-master-worker-client.md` 10 | yes | PR CI + Nightly / Lab | `scripts/e2e-master-worker-client-target-local.sh`, `scripts/e2e-master-worker-client-target.sh` |

## Exit Criteria

Desired state is:

```text
PR CI
  -> command plane regressions fail on every PR

Nightly / Lab
  -> real distributed regressions fail with explicit environment contracts

Manual
  -> only fallback, not the primary validation mechanism
```

## Current Status

Implemented:

- `scripts/e2e-pr-ci.sh`
- `scripts/e2e-master-worker-client-local.sh`
- `scripts/e2e-master-worker-client-remote-smoke.sh`
- `scripts/e2e-master-worker-client-target-local.sh`
- `scripts/e2e-master-worker-client-target.sh`
- `scripts/e2e-master-worker-client-zeus.sh`
- `scripts/e2e-backend-matrix-smoke.sh`
- `scripts/e2e-run-control-local.sh`
- `scripts/e2e-run-control-zeus.sh`
- `scripts/e2e-run-control-matrix.sh`

Next operational step:

- wire `scripts/e2e-pr-ci.sh` into PR CI
- run Zeus/target/backend real lanes from a nightly or lab environment with the
  required secrets and host contracts
