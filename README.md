# orch

Orchestrator for managing multiple LLM CLIs (Claude, Codex, Gemini, OpenCode) using a unified vocabulary of **Issue**, **Run**, and **Event**.

orch runs AI coding agents non-interactively in the background, creating isolated git worktrees for each task. Check status with `orch ps`, interact when needed with `orch attach`.

## Quick Start

```bash
# Install
go install github.com/proboscis/orch/cmd/orch@latest

# Create an issue
mkdir -p issues && cat > issues/my-task.md << 'EOF'
---
type: issue
id: my-task
title: Add hello world function
status: open
---
Add a hello world function to the project.
EOF

# Run an agent
orch run my-task

# Check status
orch ps

# Interact when needed
orch attach my-task
```

**[Read the full Getting Started guide](./docs/getting-started.md)**

## Documentation

| Guide | Description |
|-------|-------------|
| **[Getting Started](./docs/getting-started.md)** | Install → First issue → First run → See it work |
| **[Core Concepts](./docs/concepts.md)** | Issue, Run, Event, Status, Worktree explained |
| **[Configuration](./docs/configuration.md)** | All config options with examples |

### Backends

| Backend | Description |
|---------|-------------|
| [File](./docs/backends/file.md) | Local markdown files (default) |
| [GitHub](./docs/backends/github.md) | GitHub Issues integration |
| [Linear](./docs/backends/linear.md) | Linear integration |

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
- **Status**: Current state derived from events (running, blocked, done, etc.)

```
User runs: orch run my-issue
  → Creates worktree + branch
  → Starts agent in tmux session
  → Returns immediately (non-blocking)

User checks: orch ps
  → Shows all runs with status

User interacts: orch attach my-issue
  → Connects to tmux session
  → Ctrl+B D to detach
```

## Status Quick Reference

| Status | Meaning | User Action |
|--------|---------|-------------|
| `running` | Agent is working | Wait, or attach to watch |
| `blocked` | Agent needs input | `orch attach` to help |
| `pr_open` | PR created | Review the PR |
| `done` | Completed | Celebrate! |
| `failed` | Error occurred | Check logs, retry |

## Releasing

```bash
git tag v0.1.0
git push --tags
```

See [GitHub Releases](https://github.com/proboscis/orch/releases) for binaries.

## License

MIT
