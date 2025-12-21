---
type: issue
id: orch-022
title: Add command to resolve runs and hide them by default
summary: Implement 'orch resolve' to mark runs/issues as resolved and update 'orch ps' default filtering
status: open
priority: high
---

# Add command to resolve runs and hide them by default

We need a workflow to officially "close" or "resolve" a run (and potentially its associated issue) once the work is verified and merged. These resolved runs should then be hidden from the default `orch ps` view to reduce clutter.

## Requirements

1.  **New Command**: `orch resolve RUN_REF`
    *   Marks the run as `resolved` (or `archived`?). `resolved` seems better as a final state.
    *   Optionally updates the Issue status to `closed` or `resolved` if all its runs are resolved? (Maybe simpler: just mark the run).
    *   Should probably verify the run is `done` or `merged` first? Or allow force resolve.
    *   **Action**: Updates the run's status to `StatusResolved` (new status).

2.  **Update `orch ps`**:
    *   **Default Behavior**: Exclude `resolved` runs from the list.
    *   **Flag**: Add `--all` or `-a` to show resolved runs.
    *   **Filter**: `orch ps --status resolved` should work.

3.  **Model Update**:
    *   Add `StatusResolved` to `internal/model/event.go`.

## Implementation Plan

1.  **Model**:
    *   Add `StatusResolved = "resolved"` in `internal/model/event.go`.
    *   Update `colorStatus` in `ps.go` to handle it (maybe gray/dim).

2.  **CLI - Resolve Command (`internal/cli/resolve.go`)**:
    *   `orch resolve <RUN_REF>`
    *   Resolve run.
    *   Append `StatusResolved` event.
    *   (Optional) Clean up worktree? `orch delete` does that. Maybe prompt: "Worktree still exists, delete it? [y/N]". For now, keep it simple (just status change).

3.  **CLI - PS (`internal/cli/ps.go`)**:
    *   In `newPsCmd`, default `Status` filter should exclude `resolved`.
    *   Wait, currently `Status` filter is empty (shows all).
    *   Logic:
        *   If `--all` is set, show everything.
        *   If `--status` is explicit, show those statuses.
        *   If neither, show all EXCEPT `resolved`.

## User Experience

```bash
# Mark a run as finished/resolved
orch resolve 66ff6

# List active runs (66ff6 is hidden)
orch ps

# List all runs including resolved
orch ps -a

# List only resolved
orch ps --status resolved
```
