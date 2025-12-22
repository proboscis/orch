---
name: orch-toolset
description: |
  Use when working with the orch CLI for issue/run management, monitoring, or agent communication.
  Covers orch issue create/list/open, orch run/ps/stop/resolve, orch monitor/attach/capture, and orch send.
version: 0.1.0
---

# Orch Toolset

## Core workflow

1. Create or review the issue with `orch issue create` or `orch open`.
2. Start work with `orch run <issue-id>` and pick an agent if needed.
3. Track progress with `orch ps` and inspect details with `orch show` or `orch open`.
4. Monitor or interact with agents using `orch monitor`, `orch attach`, `orch capture`, and `orch send`.
5. Stop stale runs with `orch stop` and mark finished work with `orch resolve`.

## Command quick reference

- Issue management: `orch issue create`, `orch issue list`, `orch open`
- Run management: `orch run`, `orch ps`, `orch stop`, `orch resolve`, `orch show`
- Monitoring: `orch monitor`, `orch attach`, `orch capture`
- Agent communication: `orch send`

For full syntax, flags, and examples, see `reference.md`.

## Best practices for multi-run orchestration

- Keep each issue focused; split large work across separate issues and runs.
- Use `orch ps --status running,blocked` to prioritize attention across active runs.
- Prefer `orch send` and `orch capture` for programmatic coordination; use `orch attach` for interactive handoffs.
- Record decisions and next steps in the issue/run notes (open with `orch open`).
- Stop or cancel idle runs to reduce noise before starting new ones.

## Control agent workflow

When acting as the control agent:

1. Use `orch issue list` and `orch ps` to understand the queue and active runs.
2. For blocked runs, fetch context with `orch capture` and review the run doc via `orch open`.
3. Send concise guidance with `orch send`, then recheck status with `orch ps`.
4. If a run is stuck or obsolete, stop it with `orch stop` before starting a replacement.
5. Resolve the issue with `orch resolve` once completed (this marks the issue, not the run).
