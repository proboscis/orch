---
type: issue
id: orch-011
title: Implement orch monitor command with TUI dashboard
summary: Interactive tmux-based monitor using bubbletea
status: open
priority: high
---

# Implement orch monitor command with TUI dashboard

Implement `orch monitor` as specified in `specs/08-monitor.md` - an interactive terminal UI for managing multiple concurrent runs.

## Core Features

1. **Dashboard window** showing all active runs with status
2. **Keyboard shortcuts** (1-9) to switch between run windows
3. **Answer mode** (a) to answer blocked questions inline
4. **tmux integration** - all runs as windows in one session

## Architecture

```
orch-monitor (tmux session)
├── window 0: dashboard (bubbletea TUI)
├── window 1: orch-008 agent session
├── window 2: orch-009 agent session
└── ...
```

## Implementation Steps

### 1. Add bubbletea dependency

```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
```

### 2. Create monitor package

```
internal/monitor/
  monitor.go      # Main Monitor struct and logic
  dashboard.go    # Bubbletea model for dashboard
  keymap.go       # Key bindings
  styles.go       # Lipgloss styles
```

### 3. Monitor struct

```go
type Monitor struct {
    session   string           // tmux session name "orch-monitor"
    store     store.Store
    runs      []*RunWindow
    dashboard *Dashboard
}

type RunWindow struct {
    index       int          // window index (1-9)
    run         *model.Run
    agentSession string      // original agent tmux session
}
```

### 4. Dashboard TUI (bubbletea)

```go
type Dashboard struct {
    runs     []RunRow
    cursor   int
    width    int
    height   int
}

type RunRow struct {
    index    int
    shortID  string
    issueID  string
    status   model.Status
    summary  string
    updated  time.Time
}

func (d Dashboard) View() string {
    // Render table with lipgloss
}

func (d Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Handle key presses: 1-9, a, s, q, etc.
}
```

### 5. CLI command

```go
func newMonitorCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "monitor",
        Short: "Interactive monitor for managing runs",
        RunE:  runMonitor,
    }
    cmd.Flags().String("issue", "", "Filter to specific issue")
    cmd.Flags().String("status", "", "Filter by status")
    return cmd
}
```

### 6. tmux session management

- Create `orch-monitor` session if not exists
- Window 0: Run bubbletea dashboard
- For each active run: link or create window connected to agent session
- Handle window switching via tmux commands

## Key Bindings

| Key | Action |
|-----|--------|
| `1-9` | Switch to run window |
| `a` | Answer mode |
| `s` | Stop run |
| `n` | New run (select issue) |
| `r` | Refresh |
| `q` | Quit |

## Dashboard Display

```
┌─ ORCH MONITOR ──────────────────────────────────────────────────┐
│  #  ID      ISSUE     STATUS   AGO   SUMMARY                     │
│  1  3f68c8  orch-008  blocked  5m    Add issue status to ps      │
│  2  f94c3e  orch-009  running  3m    Show elapsed time           │
│                                                                  │
│  ● running: 1    ◐ blocked: 1    ✓ done: 0                      │
├──────────────────────────────────────────────────────────────────┤
│  [1-9] attach   [a] answer   [s] stop   [n] new   [q] quit       │
└──────────────────────────────────────────────────────────────────┘
```

## Files to Create/Modify

- `internal/monitor/monitor.go` - new
- `internal/monitor/dashboard.go` - new
- `internal/monitor/styles.go` - new
- `internal/cli/monitor.go` - new
- `internal/cli/root.go` - add monitor command
- `go.mod` - add bubbletea dependencies
