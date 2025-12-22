---
type: issue
id: orch-060
title: Display run's last tmux capture in monitor dashboard
status: open
---

# Display run's last tmux capture in monitor dashboard

## Goal

Enhance the `orch monitor` dashboard to display the last captured tmux pane content for the currently selected run. This allows the user to inspect the current state and activity of the agent for that run directly from the dashboard.

## Requirements

1. **UI Layout**
   - Add a view or pane at the bottom of the `orch monitor` dashboard.
   - This pane should display the content of the last tmux capture for the selected run.

2. **Functionality**
   - When a run is selected in the "Runs" list, fetch its latest tmux capture.
   - Display the capture content (text/terminal output) in the new pane.
   - Handle cases where no capture is available (e.g., show a "No capture available" message).

3. **Context**
   - Helps users debug or monitor the progress of a run without manually attaching to the tmux session or checking log files.

## Relevant Locations

- `internal/monitor/`: Dashboard UI layout and rendering logic.
- `internal/agent/`: Likely handles the coordination of runs and might have access to capture data.
- `internal/tmux/`: Logic related to tmux operations and potentially capturing pane content.
