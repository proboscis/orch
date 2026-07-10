# orch Documentation

Pick your path by what you are trying to do.

## Use orch

| Start here | |
|---|---|
| [Getting Started](./getting-started.md) | Install → first issue → first run → see it work |
| [Agent Install Runbook](./agent-install.md) | One-liner setup executed by your own AI agent |
| [ローカルクイックスタート (日本語)](./local-quickstart.ja.md) | 1 台のマシンで試す最短経路 |

| Daily use | |
|---|---|
| [Daily Workflow](./daily-workflow.md) | Morning routine, parallel runs, reviewing PRs |
| [Core Concepts](./concepts.md) | Issue, Run, Event, Status, Worktree |
| [Configuration](./configuration.md) | All config options with examples |
| [Remote Usage](./remote-usage.md) | Run orch against a remote daemon over TCP |
| [Python orch-monitor TUI](./orch-monitor.md) | Visual dashboard for issues and runs |

| Details | |
|---|---|
| Agents | [Claude](./agents/claude.md) · [Codex](./agents/codex.md) · [OpenCode](./agents/opencode.md) · [Gemini](./agents/gemini.md) · [Custom](./agents/custom.md) |
| Issue backends | [File](./backends/file.md) · [GitHub](./backends/github.md) |
| Architecture decisions | [ADR-0001: issue hex IDs](./adr/ADR-0001-issue-hex-ids.md) · [ADR-0002: worker autostart](./adr/ADR-0002-idempotent-worker-autostart.md) |
| Reference | [Commands](./reference/commands.md) · [Statuses](./reference/statuses.md) · [Events](./reference/events.md) · [SQL Queries](./reference/query.md) |

## Contribute to orch

Everything under [development/](./development/): the
[Development Guide](./development/README.md) (versioning, branching, PR
rubric) and the canonical E2E validation docs
([master/worker/client](./development/e2e-master-worker-client.md),
[backend matrix](./development/e2e-backend-matrix.md),
[automation plan](./development/e2e-automation-plan.md)).
Agent-facing instructions live in [AGENTS.md](../AGENTS.md) at the repo root.

## Internal design records

Architecture decisions live under [adr/](./adr/); start with
[ADR-0001](./adr/ADR-0001-issue-hex-ids.md) for issue identity and
[ADR-0002](./adr/ADR-0002-idempotent-worker-autostart.md) for worker startup.

Everything under [design/](./design/) documents the coupling cores and their
laws — most importantly the run-status law book
[run-state-machine.md](./design/run-state-machine.md) and the
[coupling-core roadmap](./design/coupling-core-roadmap.md). These are
records of decided invariants, not user documentation: read them before
changing `internal/daemon/step.go`, `monitor.go`, `worker_plane.go`, or
`internal/model`.
