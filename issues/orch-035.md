---
type: issue
id: orch-035
title: Fix run attachment targeting wrong pane after monitor restart
status: resolved
---

# Fix run attachment targeting wrong pane after monitor restart

## Problem

When the orch control agent creates and runs an issue, selecting the run in the dashboard correctly attaches to the run's agent panel. However, after:

1. Detaching from the tmux session
2. Starting a new monitor with `orch monitor --new`

Selecting the same run and pressing Enter attaches to the **previous monitor's control agent panel** instead of the **run's actual agent panel**.

## Expected Behavior

Selecting a run should always attach to that run's agent session/pane, regardless of which monitor instance is being used.

## Root Cause (suspected)

The run's pane/session reference is likely cached or stored from the original monitor session, and not updated when a new monitor starts. The stored reference points to the old control agent pane.

## Acceptance Criteria

- [x] Selecting a run in any monitor instance attaches to the correct run agent pane
- [x] Pane references should be resolved dynamically based on the run's actual tmux session
- [x] Old/stale pane references should not affect new monitor instances

## Implementation Notes

- Fix uses tmux window linking by window ID instead of swap-pane content swapping.
- Run selection now opens the run in its own tmux window; the dashboard chat pane remains the control agent chat.
- This change avoids stale pane IDs across monitor restarts (root cause of orch-035).

## Future Work

- Prefer a first-class UI (VSCode extension or standalone WebUI/Electron) for flexible layout control while keeping tmux as the session/process layer.
- Deprioritize terminal-emulator layout integration unless the UI path proves infeasible.
- If swap-pane UX is revisited, require an `orch` swap wrapper with slot tagging plus reconciliation on startup to avoid pane-ID drift.
- Evaluate move/join-pane or nested tmux as alternatives if a dedicated UI is insufficient, noting added complexity and UX tradeoffs.
