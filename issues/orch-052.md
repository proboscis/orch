---
type: issue
id: orch-052
title: Add Shell Exec Keybinding to Orch Monitor Dashboard
status: open
---

# Add Shell Exec Keybinding to Orch Monitor Dashboard

Add a keyboard shortcut in the orch monitor dashboard to open a shell session in the selected run.

## Feature Request

When viewing runs in `orch monitor`, add a keybinding (e.g., 'e' or 'x') that executes:
```
orch exec <run_id> -- zsh
```

This allows quick shell access to a run's environment directly from the dashboard without needing to copy the run ID and run the command manually.

## Acceptance Criteria

- [ ] Add keybinding to monitor dashboard (suggest 'e' for exec or 'x' for shell)
- [ ] Execute `orch exec <selected_run_id> -- zsh` when key is pressed
- [ ] Handle case when no run is selected
- [ ] Update help/legend to show new keybinding
