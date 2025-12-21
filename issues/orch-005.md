---
type: issue
id: orch-005
title: Add orch delete command for removing runs
status: resolved
priority: medium
---

# Add orch delete command for removing runs

Implement `orch delete` command to remove runs and their associated resources.

## Usage

```bash
orch delete RUN_REF           # Delete specific run
orch delete ISSUE_ID --all    # Delete all runs for an issue
orch delete --older-than 30d  # Delete runs older than 30 days
```

## Options

- `--all` - Delete all runs for the specified issue
- `--older-than <duration>` - Delete runs older than duration (e.g., 7d, 2w, 1m)
- `--status <status>` - Only delete runs with specific status (done/failed/canceled)
- `--force` - Skip confirmation prompt
- `--dry-run` - Show what would be deleted without deleting

## Cleanup Actions

1. Remove run document (`runs/<issue-id>/<run-id>.md`)
2. Remove run log directory if exists (`runs/<issue-id>/<run-id>.log/`)
3. Kill tmux session if still running
4. Optionally remove git worktree (`--with-worktree` flag)
5. Optionally remove git branch (`--with-branch` flag)

## Exit Codes

- 0: Success
- 6: Run not found
- 10: Internal error
