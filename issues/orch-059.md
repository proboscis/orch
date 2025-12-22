---
type: issue
id: orch-059
title: Add key shortcut to request merge to main for a selected run in monitor
status: open
---

# Add key shortcut to request merge to main for a selected run in monitor

## Goal

Streamline the workflow by adding a key shortcut in the `orch monitor` dashboard to easily request merging a selected run's work back to the main branch.

## Requirements

1. **User Interaction**
   - In the "Runs" pane of `orch monitor`, allow the user to select a specific run.
   - Define a key shortcut (e.g., `M` or `Ctrl+M`) to trigger the merge request action for the highlighted run.

2. **Action Logic**
   - When the shortcut is pressed:
     - Identify the selected run and its associated branch/worktree.
     - Trigger the process to merge changes to `main`. This might involve:
       - Creating a Pull Request (using `orch pr create` logic).
       - Or executing a direct merge if configured/appropriate.
     - Provide feedback to the user (e.g., "Merge requested..." or "PR created...").

3. **Implementation**
   - Update `internal/monitor/` to capture the key input.
   - invoke the appropriate underlying functionality (likely reusing logic from `internal/pr` or `internal/git`).

## Relevant Locations

- `internal/monitor/`: Dashboard input handling and run selection.
- `internal/pr/`: Pull request creation logic.
