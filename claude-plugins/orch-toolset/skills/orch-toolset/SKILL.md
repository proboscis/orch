---
name: orch-toolset
description: |
  Use when working with the orch CLI for managing LLM agent runs, issues, and multi-agent orchestration.
  Covers orch issue create/list/open, orch run/ps/stop/resolve/continue, orch monitor/attach/capture, orch send/exec, and control agent workflows.
  Trigger terms: orch, orchestrator, issue management, run management, agent runs, multi-agent, tmux session, worktree.
version: 1.0.0
---

# Orch Toolset

Orch is a non-interactive orchestrator for managing multiple LLM CLI agents (Claude, Codex, Gemini) using issues and runs as the core abstraction.

## Design Philosophy

- **Non-interactive by default**: Runs start, agent works, CLI returns immediately
- **Append-only events**: Run state derived from immutable event log
- **UI as skin**: CLI is stable contract; tmux/VSCode/Obsidian are optional layers
- **Delegate PTY control**: Use tmux for interaction, not direct TTY

## Core Vocabulary

- **Issue**: Unit of work specification (e.g., `orch-055`)
- **Run**: Single execution attempt for an issue (multiple runs per issue possible)
- **Event**: Immutable append-only record in run log
- **RUN_REF**: Reference format - `ISSUE_ID#RUN_ID`, `ISSUE_ID` (latest), or SHORT_ID (6-char hex)

## Core Workflow

1. Create or review the issue with `orch issue create` or `orch open`
2. Start work with `orch run <issue-id>` (picks agent, creates worktree, launches tmux session)
3. Track progress with `orch ps` and inspect with `orch show` or `orch open`
4. Monitor or interact with `orch monitor`, `orch attach`, `orch capture`, and `orch send`
5. Stop stale runs with `orch stop` and mark finished work with `orch resolve`

## Command Categories

### Issue Management
- `orch issue create <ID>`: Create new issue with frontmatter
- `orch issue list`: List all issues (filter with `--status`)
- `orch open <ID>`: Open issue/run in Obsidian or editor

### Run Management
- `orch run <ISSUE_ID>`: Start new run (creates worktree, launches agent in tmux)
- `orch continue <RUN_REF>`: Resume from existing worktree/branch
- `orch ps`: List runs with status filtering
- `orch show <RUN_REF>`: Inspect run details and events
- `orch stop <RUN_REF>`: Kill tmux session and mark canceled
- `orch resolve <ISSUE_ID>`: Mark issue as resolved

### Monitoring
- `orch monitor`: Interactive TUI dashboard for all runs
- `orch attach <RUN_REF>`: Attach to agent's tmux session
- `orch capture <RUN_REF>`: Get agent output without attaching

### Agent Communication
- `orch send <RUN_REF> <MESSAGE>`: Send text to running agent
- `orch exec <RUN_REF> -- COMMAND`: Execute command in run's worktree

### Maintenance
- `orch repair`: Fix system state corruption
- `orch tick <RUN_REF>`: Resume blocked runs

## Run Lifecycle States

```
queued -> booting -> running -> blocked -> pr_open -> done
                            \-> failed
                            \-> canceled
                            \-> unknown
```

State meanings:
- `queued`: Run created, waiting to start
- `booting`: Agent starting up
- `running`: Agent actively working
- `blocked`: Agent needs input (waiting on question)
- `blocked_api`: API rate limit hit
- `pr_open`: PR created, awaiting review
- `done`: Work completed successfully
- `failed`: Run failed with error
- `canceled`: Manually stopped via `orch stop`
- `unknown`: Agent exited unexpectedly

## Best Practices for Multi-Run Orchestration

### Issue/Run Design
- Keep each issue focused; split large work into separate issues
- One run per attempt; use `orch continue` for retries from same branch
- Use issue status (open/resolved) separately from run status

### Monitoring Strategy
- Use `orch ps --status running,blocked` for active attention
- Use `orch monitor` for multi-agent coordination and rapid context-switching
- Set up background daemon (auto-runs) and check periodically

### Communication
- Prefer `orch send` + `orch capture` for programmatic loops
- Use `orch attach` for interactive handoffs (image paste, complex setup)
- Record decisions in issue/run notes (via `orch open`)

### Workflow Tips
- Stop idle/stale runs before starting new ones
- Use short IDs (2-6 hex chars) for speed: `orch attach a3b4c5`
- Keep issue queue clean: resolve completed, close duplicates
- Use `orch exec -- <test>` for isolated testing without tmux

## Control Agent Workflow

When acting as a control agent managing other runs:

1. **Survey**: `orch issue list` to see all work items
2. **Monitor**: `orch ps --status running,blocked` to prioritize attention
3. **Investigate**: `orch capture <run>` to fetch output without attaching
4. **Inspect**: `orch open <run>` to read full run doc with context
5. **Guide**: `orch send <run> "focused guidance"` to steer work
6. **Execute**: `orch exec <run> -- pytest` to test in isolation
7. **Manage**: `orch stop <issue>` to kill stale runs
8. **Resolve**: `orch resolve <issue>` to mark completed work

### Key Control Patterns
- Use `orch capture` + `orch send` for async coordination
- Use `orch attach` only when direct interaction needed (image paste, complex dialog)
- Check `orch ps` frequently to catch blockers early
- Open run docs in editor to read full context before sending guidance

## Event Format

Events are append-only records in run documents:

```
- <ISO8601> | <type> | <name> | key=value | ...
```

Types: `status`, `artifact`, `phase`, `test`, `monitor`

## Configuration

Config resolution order:
1. Command-line options (`--vault`, `--backend`)
2. `.orch/config.yaml` in current/parent directories
3. Environment variables (`ORCH_VAULT`, `ORCH_BACKEND`)
4. Global config (`~/.config/orch/config.yaml`)

Example `.orch/config.yaml`:
```yaml
vault: ~/vault
agent: claude
worktree_root: .git-worktrees
base_branch: main
```

## Quick Command Reference

| Goal | Command |
|------|---------|
| Create issue | `orch issue create <ID> --title "..."` |
| Start run | `orch run <ISSUE>` |
| List active | `orch ps --status running,blocked` |
| Inspect run | `orch show <RUN>` |
| Watch agent | `orch attach <RUN>` |
| Get output | `orch capture <RUN>` |
| Send guidance | `orch send <RUN> "message"` |
| Run tests | `orch exec <RUN> -- pytest` |
| Stop run | `orch stop <RUN>` |
| Mark done | `orch resolve <ISSUE>` |
| TUI dashboard | `orch monitor` |

For detailed syntax, flags, and examples, see [reference.md](reference.md).
