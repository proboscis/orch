---
type: issue
id: repair-command
title: Implement orch repair command
status: open
priority: medium
---

# Implement orch repair command

Implement the `orch repair` command as specified in `specs/03-commands.md`.

## Requirements

- Detect and restart abnormal daemon
- Find "running" runs with no tmux session → mark as failed
- Detect orphaned worktrees/sessions (warning only)
- Fix contradictory states

## Options

- `--dry-run` - Report problems without fixing
- `--force` - Fix without confirmation

## Exit Codes

- 0: Success (no repair needed)
- 1: Repair executed (problems found and fixed)
- 10: Internal error
