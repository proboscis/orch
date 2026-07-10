# orch

Orchestrator for managing multiple LLM CLIs (Claude, Codex, Gemini, OpenCode) using a unified vocabulary of **Issue**, **Run**, and **Event**.

orch runs AI coding agents non-interactively in the background, creating isolated git worktrees for each task. Check status with `orch ps`, interact when needed with `orch attach`.

> **Beta scope: single machine.** The current beta covers running orch on one
> machine (daemon, worker, and agents all local — zero process management
> needed). Multi-host / cluster mode (remote masters, `ORCH_REMOTE`,
> `--on <target>`) exists in the code but is the **next milestone**: not part
> of the beta, not yet supported.

## Install via your AI agent (one line)

Paste this into your coding agent (Claude Code, Codex, OpenCode, …):

```
Fetch https://github.com/proboscis/orch/releases/latest/download/agent-install.md and follow it exactly: install the orch binary, install the orch skill into this agent, then walk me through my first orch run interactively.
```

The agent installs the binary, installs the [orch skill](./claude-plugins/orch-toolset/skills/orch-toolset/SKILL.md)
into its own skills directory (so every future session knows orch), and then
runs the first tutorial together with you. Manual setup is below.

## Quick Start

```bash
# Install (recommended)
curl -sSL https://raw.githubusercontent.com/proboscis/orch/main/install.sh | bash

# Or with Go
go install github.com/proboscis/orch/cmd/orch@latest

# One-time, inside your git repo (needs an `origin` remote — orch derives
# project identity from its URL):
mkdir -p .orch && printf 'agent: claude\nbase_branch: main\n' > .orch/config.yaml
orch daemon repo register "$(pwd)"   # map project identity -> this checkout
# (daemon and worker start automatically on demand — no manual process management)

# Create an issue
orch issue create my-task --title "Add hello world function"

# Run an agent
orch run my-task

# Check status
orch ps

# Interact when needed
orch attach my-task
```

**[Read the full Getting Started guide](./docs/getting-started.md)** —
日本語版のローカル入門は **[ローカルクイックスタート](./docs/local-quickstart.ja.md)**。

## Documentation

| Guide | Description |
|-------|-------------|
| **[Getting Started](./docs/getting-started.md)** | Install → First issue → First run → See it work |
| **[Agent Install Runbook](./docs/agent-install.md)** | One-liner setup executed by your own AI agent (binary + skill + guided first run) |
| **[ローカルクイックスタート (日本語)](./docs/local-quickstart.ja.md)** | 1 台のマシンで試す最短経路(クラスタ設定なし) |
| **[Remote Usage](./docs/remote-usage.md)** | Multi-host mode — next milestone, out of beta scope |
| **[Daily Workflow](./docs/daily-workflow.md)** | Morning routine, parallel runs, reviewing PRs |
| **[orch-monitor TUI](./docs/orch-monitor.md)** | Visual dashboard for managing issues and runs |
| **[Core Concepts](./docs/concepts.md)** | Issue, Run, Event, Status, Worktree explained |
| **[Configuration](./docs/configuration.md)** | All config options with examples |
| **Architecture Decision Records** | [ADR-0001: issue hex IDs](./docs/adr/ADR-0001-issue-hex-ids.md) · [ADR-0002: worker autostart](./docs/adr/ADR-0002-idempotent-worker-autostart.md) |

### Backends

| Backend | Description |
|---------|-------------|
| [File](./docs/backends/file.md) | Local markdown files (default) |
| [GitHub](./docs/backends/github.md) | GitHub Issues integration |

### Agents

| Agent | Description |
|-------|-------------|
| [Claude](./docs/agents/claude.md) | Anthropic's Claude Code |
| [OpenCode](./docs/agents/opencode.md) | Multi-provider open-source agent |
| [Codex](./docs/agents/codex.md) | OpenAI's Codex |
| [Gemini](./docs/agents/gemini.md) | Google's Gemini |
| [Custom](./docs/agents/custom.md) | Bring your own agent |

### Reference

| Reference | Description |
|-----------|-------------|
| [Commands](./docs/reference/commands.md) | Full CLI reference |
| [Events](./docs/reference/events.md) | Event types and format |
| [Statuses](./docs/reference/statuses.md) | Status state machine |
| [SQL Queries](./docs/reference/query.md) | Query examples and schema |

## Key Concepts

- **Issue**: A task specification (markdown file or external ticket)
- **Run**: One execution attempt for an issue (isolated worktree + branch)
- **Event**: Append-only log entry tracking run progress
- **Status**: Current state derived from events (running, waiting, done, etc.)

```
User runs: orch run my-issue
  → Creates worktree + branch
  → Starts agent in a terminal multiplexer (tmux or zellij)
  → Returns immediately (non-blocking)

User checks: orch ps
  → Shows all runs with status

User interacts: orch attach my-issue
  → Connects to the terminal multiplexer session
  → Detach with Ctrl+B D (tmux) or Ctrl+O D (zellij)
```

## Status Quick Reference

| Status | Meaning | User Action |
|--------|---------|-------------|
| `running` | Agent is working | Wait, or attach to watch |
| `waiting` | Agent needs input | `orch attach` to help |
| `pr_open` | PR created | Review the PR |
| `done` | Completed | Celebrate! |
| `failed` | Error occurred | Check logs, retry |

## Contributing

See the **[Development Guide](./docs/development/README.md)** for:
- Versioning philosophy (Semver)
- Trunk-based development workflow
- Branch naming conventions
- PR guidelines

## Releasing

```bash
git tag v0.1.0
git push --tags
```

See [GitHub Releases](https://github.com/proboscis/orch/releases) for binaries.

## License

MIT
