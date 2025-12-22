---
type: issue
id: orch-057
title: Add sort order options to orch monitor dashboard
status: open
---

# Add sort order options to orch monitor dashboard

## Goal

Allow users to customize the sorting order of "Runs" and "Issues" panes in the `orch monitor` dashboard. Users should be able to toggle or specify sorting by name, last update time, status, etc.

## Requirements

1. **Sorting Criteria**
   - Support sorting for both **Runs** and **Issues** panes.
   - Criteria should include:
     - **Name/ID**: Alphabetical sorting.
     - **Last Update**: Most recently updated first.
     - **Status**: Grouping by status (e.g., active/pending/completed).

2. **CLI Integration**
   - Add flags to `orch monitor` to set initial sort order (e.g., `--sort-runs`, `--sort-issues`).
   - Define valid sort keys (e.g., `name`, `updated`, `status`).

3. **Dashboard UI/Interaction**
   - If possible within the tmux/dashboard constraints, provide a way to switch sort order while the monitor is running (e.g., keybindings or periodic refresh based on updated logic).
   - Alternatively, ensure the display logic in `internal/monitor/` correctly implements the chosen sorting before rendering the panes.

4. **Implementation Details**
   - Update `internal/monitor/monitor.go` to handle sorting logic when gathering data for the dashboard panes.
   - Modify the data retrieval functions for issues and runs to support ordered results.

## Relevant Locations

- `internal/cli/monitor.go`: CLI flags for sorting.
- `internal/monitor/monitor.go`: Dashboard rendering and data gathering logic.
- `internal/store/`: Underlying data retrieval that might need to support sorting.
