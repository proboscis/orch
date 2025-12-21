# Interactive Monitor

## Overview

`orch monitor` provides an interactive terminal UI for managing multiple concurrent runs. It allows users to:
- See all active runs at a glance
- Switch between agent sessions with keyboard shortcuts
- Answer blocked questions inline
- Receive notifications on status changes

## Design Principles

1. **tmux-native**: Build on tmux rather than fighting it
2. **Keyboard-driven**: Fast switching with minimal keystrokes
3. **Context-aware**: Show relevant info when switching
4. **Non-blocking**: Monitor doesn't interfere with agent work

## Architecture

### Session Layout

```
orch-monitor (tmux session)
├── window 0: dashboard (orch ps --watch + controls)
├── window 1: orch-008 agent session
├── window 2: orch-009 agent session
├── window 3: orch-010 agent session
└── ...
```

All runs consolidated into one tmux session with:
- Window 0: Dashboard/control panel
- Windows 1-N: Individual agent sessions

### Dashboard View

```
┌─ ORCH MONITOR ──────────────────────────────────────────────────┐
│                                                                  │
│  #  ID      ISSUE     STATUS   AGO   SUMMARY                     │
│  1  3f68c8  orch-008  blocked  5m    Add issue status to ps      │
│  2  f94c3e  orch-009  blocked  3m    Show elapsed time           │
│  3  43d956  orch-010  running  1m    Add summary column to ps    │
│                                                                  │
│  ● running: 1    ◐ blocked: 2    ✓ done: 0    ✗ failed: 0       │
│                                                                  │
├──────────────────────────────────────────────────────────────────┤
│  [1-9] attach   [a] answer   [s] stop   [n] new run   [q] quit   │
└──────────────────────────────────────────────────────────────────┘
```

## Keyboard Shortcuts

### In Dashboard (window 0)

| Key | Action |
|-----|--------|
| `1-9` | Attach to run by index |
| `a` | Answer mode - select blocked run to answer questions |
| `s` | Stop mode - select run to stop |
| `n` | New run - select issue to start |
| `r` | Refresh display |
| `f` | Filter runs (fzf) |
| `q` | Quit monitor |
| `?` | Show help |

### In Agent Window (window 1-N)

| Key | Action |
|-----|--------|
| `Ctrl-b 0` | Return to dashboard (tmux native) |
| `Ctrl-b n/p` | Next/previous run (tmux native) |
| `Esc Esc` | Quick return to dashboard (custom binding) |

### Global (tmux prefix)

| Key | Action |
|-----|--------|
| `Ctrl-b 0` | Go to dashboard |
| `Ctrl-b 1-9` | Go to run by window number |
| `Ctrl-b w` | tmux window picker |

## Commands

### orch monitor

Start the interactive monitor:

```bash
orch monitor              # Start monitor with all active runs
orch monitor --issue X    # Monitor runs for specific issue only
orch monitor --attach     # Auto-attach to monitor if exists
```

### Options

| Option | Description |
|--------|-------------|
| `--issue <ID>` | Filter to specific issue |
| `--status <status>` | Filter by status (running,blocked) |
| `--attach` | Attach to existing monitor session |
| `--new` | Force create new monitor (kill existing) |

## Implementation

### Phase 1: Basic Monitor (Shell Script)

Simple implementation using bash/tmux:

```bash
#!/bin/bash
# orch-monitor.sh

SESSION="orch-monitor"

# Create session with dashboard window
tmux new-session -d -s $SESSION -n dashboard

# Dashboard shows orch ps in a loop
tmux send-keys -t $SESSION:dashboard "watch -n2 'orch ps'" Enter

# Add window for each active run
for run in $(orch ps --tsv | tail -n+2 | cut -f1,2); do
  # ... create window and attach to run's session
done

tmux attach -t $SESSION
```

### Phase 2: Go Implementation

Proper implementation in Go:

```go
type Monitor struct {
    session     string           // tmux session name
    runs        []*RunWindow     // active run windows
    dashboard   *Dashboard       // dashboard state
    store       store.Store
}

type RunWindow struct {
    windowIndex int
    run         *model.Run
    session     string  // original agent session
}

func (m *Monitor) Start() error {
    // 1. Create monitor tmux session
    // 2. Create dashboard window
    // 3. For each active run, create window linked to agent session
    // 4. Start dashboard UI loop
    // 5. Handle keyboard input
}
```

### Phase 3: Real-time Updates

- Daemon sends notifications via Unix socket
- Monitor subscribes to status changes
- Dashboard updates without polling

## Context Display

When switching to a run window, show context header:

```
┌─ orch-008 ─────────────────────────────────────────────────────┐
│ Issue: Show issue status in orch ps output                      │
│ Status: blocked (2 questions pending)                           │
│ Branch: issue/orch-008/run-20251221-122336                      │
│ Updated: 5 minutes ago                                          │
├─────────────────────────────────────────────────────────────────┤
│ [Ctrl-b 0] dashboard  [Ctrl-b n/p] next/prev  [Ctrl-b w] list   │
└─────────────────────────────────────────────────────────────────┘

< agent output below >
```

## Answer Mode

When pressing `a` in dashboard:

```
┌─ ANSWER QUESTION ───────────────────────────────────────────────┐
│                                                                  │
│  Blocked runs with pending questions:                            │
│                                                                  │
│  [1] orch-008 - 2 questions                                      │
│      Q1: Should I add color coding? (yes/no)                     │
│      Q2: Include in JSON output? (yes/no)                        │
│                                                                  │
│  [2] orch-009 - 1 question                                       │
│      Q1: Use "ago" suffix or just duration? (ago/bare)           │
│                                                                  │
├──────────────────────────────────────────────────────────────────┤
│  Select run [1-2], or [Esc] to cancel:                           │
└──────────────────────────────────────────────────────────────────┘
```

After selecting run, show question details and prompt for answer:

```
┌─ ANSWER: orch-008 Q1 ───────────────────────────────────────────┐
│                                                                  │
│  Question: Should I add color coding to the status column?       │
│                                                                  │
│  Context: Currently status is plain text. I can add ANSI         │
│  colors like green for running, yellow for blocked, etc.         │
│                                                                  │
│  Choices: [y]es  [n]o  [s]kip                                    │
│                                                                  │
├──────────────────────────────────────────────────────────────────┤
│  Your answer (or type custom response):                          │
│  > _                                                             │
└──────────────────────────────────────────────────────────────────┘
```

## Notifications

Desktop notifications (optional) when:
- Run becomes blocked (needs input)
- Run completes (done/failed)
- Run stalls (no output for N minutes)

```bash
# macOS
osascript -e 'display notification "orch-008 needs input" with title "Orch"'

# Linux
notify-send "Orch" "orch-008 needs input"
```

## Configuration

In `.orch/config.yaml`:

```yaml
monitor:
  refresh_interval: 2s
  notifications: true
  notification_command: "notify-send 'Orch' '{message}'"

  # Key bindings (tmux format)
  keybindings:
    dashboard: "C-b 0"
    quick_return: "Escape Escape"
```

## Future Enhancements

1. **Split view**: Show multiple runs side-by-side
2. **Log streaming**: Tail run logs in dashboard
3. **Resource usage**: Show CPU/memory per agent
4. **Timeline view**: Visual timeline of run events
5. **Web UI**: Browser-based monitor (websocket to daemon)
