---
type: issue
id: orch-036
title: Refactor monitor pane management to eliminate swap-based architecture
status: resolved
---

# Refactor monitor pane management to eliminate swap-based architecture

## Summary

The current monitor pane management uses `tmux swap-pane` to display run sessions, which is fundamentally fragile and leads to bugs like orch-035. This issue proposes a robust redesign using tmux's native session/window management instead of content swapping.

## Problem Analysis

### Current Architecture (Fragile)

```
Monitor Session (orch-monitor)
┌─────────────────────────────────────────┐
│  Window 0 (dashboard)                   │
│  ┌─────────┬─────────────────────────┐  │
│  │ runs    │        chat             │  │
│  │ (pane)  │       (pane)            │  │
│  ├─────────┤    [SWAPPED with        │  │
│  │ issues  │     run session]        │  │
│  │ (pane)  │                         │  │
│  └─────────┴─────────────────────────┘  │
└─────────────────────────────────────────┘

Run Session (run-orch-035-20231220)
┌─────────────────────────────────────────┐
│  Window 0                               │
│  ┌─────────────────────────────────────┐│
│  │   agent terminal                    ││
│  │   [SWAPPED with monitor chat]       ││
│  └─────────────────────────────────────┘│
└─────────────────────────────────────────┘
```

When `OpenRun()` is called:
1. `tmux.SwapPane(runPane, chatPane)` swaps **content** between panes
2. Pane IDs remain in their original sessions (only content moves)
3. Code must track "chatPane now shows run content, runPane now shows chat content"
4. This state is stored in-memory only (`m.activeRun`, `m.activeTitle`)
5. On monitor restart, state is lost and pane references become stale

### Specific Fragility Points

1. **Pane IDs don't move with swap**: After `SwapPane(A, B)`, pane A is still in its original session but shows B's content
2. **Title-based lookup is unreliable**: `findPaneByTitle()` can match wrong panes if titles are duplicated
3. **In-memory state only**: `m.activeRun` and `m.activeTitle` are lost on monitor restart
4. **No recovery mechanism**: If anything goes wrong, there's no way to detect or fix stale state
5. **Complex mental model**: Developers must track "which pane ID shows which content" after every swap

## Proposed Solution: Window-Based Architecture

### Design Principle

Instead of swapping pane contents between sessions, use tmux's window management:
- Keep dashboard in window 0 (always stable)
- Use a dedicated "run view" window (window 1) for viewing runs
- Link/unlink run session windows as needed

### New Architecture

```
Monitor Session (orch-monitor)
┌─────────────────────────────────────────┐
│  Window 0 (dashboard) - ALWAYS STABLE   │
│  ┌─────────┬─────────────────────────┐  │
│  │ runs    │        chat             │  │
│  │ (pane)  │       (pane)            │  │
│  ├─────────┤  [control agent always] │  │
│  │ issues  │                         │  │
│  │ (pane)  │                         │  │
│  └─────────┴─────────────────────────┘  │
│                                         │
│  Window 1 (run-view) - LINKED WINDOW    │
│  ┌─────────────────────────────────────┐│
│  │   [linked from run session]         ││
│  │   shows: run-orch-035-20231220:0    ││
│  └─────────────────────────────────────┘│
└─────────────────────────────────────────┘
```

### Key Changes

1. **OpenRun() becomes**:
   ```go
   func (m *Monitor) OpenRun(run *model.Run) error {
       sessionName := run.TmuxSession
       if sessionName == "" {
           sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
       }

       // Unlink previous run window if any
       if m.hasRunWindow() {
           tmux.UnlinkWindow(m.session, runViewWindowIdx)
       }

       // Link the run's window into monitor session
       err := tmux.LinkWindow(sessionName, 0, m.session, runViewWindowIdx)
       if err != nil {
           return err
       }

       // Switch to run view window
       return tmux.SelectWindow(m.session, runViewWindowIdx)
   }
   ```

2. **CloseRunPane() becomes**:
   ```go
   func (m *Monitor) CloseRun() error {
       if m.hasRunWindow() {
           tmux.UnlinkWindow(m.session, runViewWindowIdx)
       }
       return tmux.SelectWindow(m.session, dashboardWindowIdx)
   }
   ```

3. **No in-memory state needed**: The linked window relationship is managed by tmux itself

### Benefits

| Aspect | Current (Swap) | Proposed (Link) |
|--------|----------------|-----------------|
| State tracking | In-memory, lost on restart | Managed by tmux |
| Pane identity | Confusing after swap | Clear and stable |
| Recovery | None | Automatic (just unlink/relink) |
| Mental model | Complex | Simple |
| Monitor restart | Breaks run viewing | Works correctly |

## Alternative Approaches Considered

### A. Session Switching (Simpler but different UX)
```go
func (m *Monitor) OpenRun(run *model.Run) error {
    return tmux.SwitchClient(run.TmuxSession)
}
```
- Pros: Simplest possible implementation
- Cons: Loses dashboard context while viewing run

### B. Join-Pane (Move pane across sessions)
```go
tmux.JoinPane(runPane, monitorChatPane)  // Actually moves the pane
```
- Pros: Pane actually moves, clear ownership
- Cons: More complex, need to manage original layout restoration

### C. Embedded Attach (Nested tmux)
Create a pane that runs `tmux attach-session -t run-session -r`
- Pros: Clear separation
- Cons: Nested tmux complexity, keybinding conflicts

## Implementation Plan

### Phase 1: Add tmux helper functions
- [ ] Add `JoinPane(src, dst string) error` to tmux package
- [ ] Add `MovePane(src, dst string) error` to tmux package
- [ ] Add `HasWindow(session string, index int) bool` to tmux package

### Phase 2: Refactor Monitor struct
- [ ] Remove `activeRun` and `activeTitle` fields
- [ ] Add `runViewWindowIdx` constant (value: 1)
- [ ] Add `hasRunWindow() bool` helper method

### Phase 3: Rewrite OpenRun/CloseRun
- [ ] Implement new `OpenRun()` using `LinkWindow`
- [ ] Implement new `CloseRun()` using `UnlinkWindow`
- [ ] Update `SwitchRuns()`, `SwitchIssues()`, `SwitchChat()` to handle window switching

### Phase 4: Update dashboard navigation
- [ ] Update keybindings to switch between windows when run is open
- [ ] Consider adding a "back to dashboard" keybinding when in run view

### Phase 5: Cleanup
- [ ] Remove `SwapPane` usage from monitor
- [ ] Remove title-based pane lookup for run management
- [ ] Update tests

## Acceptance Criteria

- [ ] Selecting a run works correctly after `orch monitor --new`
- [ ] Run view persists correctly across monitor restarts
- [ ] Dashboard panes are never modified when viewing runs
- [ ] Navigation between dashboard and run view is smooth
- [ ] No in-memory state required for run viewing

## Files to Modify

- `internal/tmux/tmux.go` - Add new helper functions
- `internal/monitor/monitor.go` - Refactor OpenRun/CloseRun
- `internal/monitor/dashboard.go` - Update navigation keybindings

## Related Issues

- orch-035: Fix run attachment targeting wrong pane after monitor restart (immediate fix)
- This issue: Fundamental architectural fix to prevent similar bugs
