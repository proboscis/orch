---
type: issue
id: orch-058
title: Add ability to change issue status from orch monitor
status: open
---

# Add ability to change issue status from orch monitor

## Goal

Allow users to change the status of an issue (e.g., mark as "canceled", "completed", or "in_progress") directly from the `orch monitor` dashboard.

## Requirements

1. **Status Update Action**
   - Provide a way for users to select an issue in the monitor's issue pane and update its status.
   - Since `orch monitor` is tmux-based, this might be implemented via:
     - A new CLI command (e.g., `orch issue status <id> <status>`) that the user can run.
     - A keybinding or a specific interaction within the monitor if feasible.

2. **Supported Statuses**
   - The system should support standard statuses: `open`, `in_progress`, `completed`, `canceled`, `blocked`.

3. **Implementation**
   - Update `internal/cli/issue.go` (if it exists) or add a new command to handle status updates.
   - Ensure the `internal/store` can persist the status change.
   - The `orch monitor` dashboard should reflect these changes upon refresh.

## Relevant Locations

- `internal/cli/`: For the CLI command to update status.
- `internal/monitor/`: For displaying status and potentially providing interaction.
- `internal/store/`: For persisting issue status.
