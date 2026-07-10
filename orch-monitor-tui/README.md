# Orch Monitor TUI (Python/Textual)

Python Textual-based terminal user interface for `orch monitor`.

## Features

- **Issue Dashboard**: View all issues with status filtering
- **Run Dashboard**: Monitor active and completed runs with real-time status
- **Detail Panels**: View issue content and run events
- **Keybindings**: Quick navigation and actions
- **Real-time Updates**: Automatic refresh with daemon-based data
- **Multiplexer Support**: Works with both tmux and zellij

## Quick Start (Onboarding)

New to orch-monitor? Here's how to get started:

1. **Install orch-monitor**: `uv tool install /path/to/orch/orch-monitor-tui`
2. **Launch the TUI**: `orch-monitor` (or `orch-monitor --new-control-agent` for fresh start)
3. **Press `?`** to see all keybindings and workflow tips
4. **Navigate** with arrow keys, **Tab** to switch panels
5. **Start a run** on an issue with `n`, select an agent
6. **Monitor progress** in the Runs panel
7. **Attach** to a running agent with `a` or Enter

> **Tip**: Press `?` anytime within the TUI for a quick reference of keybindings and workflow.

## Workflow Guide

This section describes the end-to-end workflow for using orch-monitor TUI with the control agent to manage issue-driven development.

### 1. Setup Repository with Orch

```bash
cd your-repo
mkdir -p .orch
# Configure `.orch/config.yaml` with agent/runtime preferences.
```

### 2. Launch orch-monitor

```bash
orch-monitor  # First launch, or attach to an existing session

# To replace both the layout and control agent session:
orch-monitor --new-control-agent
```

### 3. Meet the Control Agent

The control agent runs in the bottom pane. You can:
- Ask how to use orch-monitor
- Get help with commands
- Discuss project plans

### 4. Create Issues via Discussion

Talk with the control agent to create issue files:
- Describe what you want to build
- Control agent creates the issue markdown file
- Issue appears in the **Issues Panel** (left side)

### 5. Start a Run

Multiple ways to start a run on an issue:
- **Click** on an issue in the Issues panel
- **Arrow keys + `n`** to select issue and start new run
- **Ask control agent**: "run orch-123"

Select your agent: claude / opencode / codex

### 6. Monitor the Run

The run appears in the **Runs Panel** (top):
- Watch status: queued → booting → running → pr_open → done
- See elapsed time, agent, branch info

### 7. Interact with Running Agents

While a run is active:
- **Select run + Enter** or **click**: Attach to see agent output
- **`a`**: Attach to selected run's terminal session
- Ask control agent to send messages via `orch send`

### 8. Review and Merge

When run creates a PR:
- Status shows `pr_open`
- Review the PR on GitHub
- Ask control agent to review the work
- Merge when satisfied

### 9. Control Agent Commands

The control agent can:
- `orch run <issue>` - Start a run
- `orch send <run> [message]` - Send a message to a running agent, or read it from stdin/heredoc
- `orch capture <run>` - Capture agent's last output
- `orch stop <run>` - Stop a run
- `orch ps` - List all runs
- Review work and provide feedback

### 10. Parallel Development

Run multiple issues in parallel:
- Each run gets its own git worktree
- Agents work independently
- Monitor all runs from single TUI
- Merge PRs as they complete

### The Development Loop

```
Create Issue → Start Run → Monitor → Review PR → Merge → Repeat
     ↑                                                    |
     └────────────────────────────────────────────────────┘
```

Enjoy making as many issue files with control agent and running them in parallel on worktrees!

## Prerequisites

The TUI requires the orch daemon to be running. The daemon starts automatically when you run any `orch` command (e.g., `orch ps`).

```
+-----------------------------+
|     Python Textual TUI      |
|        DaemonClient         |
+-----------+-----------------+
            | Unix socket (JSON)
            v
+-----------------------------+
|         Go Daemon           |
|   (single source of truth)  |
+-----------------------------+
```

## Installation

```bash
uv tool install /path/to/orch/orch-monitor-tui
```

Or from git:

```bash
uv tool install "git+https://github.com/proboscis/orch#subdirectory=orch-monitor-tui"
```

## Usage

Run from any directory (uses `ORCH_PROJECT` or `.orch/config.yaml`):

```bash
orch-monitor
```

Or pin a project explicitly:

```bash
orch-monitor --project github.com/owner/repo
```

### Terminal Multiplexer

By default, orch-monitor auto-detects the multiplexer (prefers the one you're inside, or falls back to tmux):

```bash
# Use tmux (default)
orch-monitor

# Use zellij
orch-monitor --multiplexer zellij
orch-monitor -m zellij

# Or set via environment variable
export ORCH_MULTIPLEXER=zellij
orch-monitor
```

### Session Management

```bash
# Restart layout only (preserves control agent session/conversation)
orch-monitor --new

# Restart both layout AND control agent (fresh start)
orch-monitor --new-control-agent

# Or explicitly combine flags
orch-monitor --new --new-control-agent
```

The `--new` flag restarts the multiplexer layout while preserving your control agent's conversation context. This is useful when:
- Layout gets corrupted or panes are misaligned
- You want to refresh the TUI panels without losing your chat history

Use `--new-control-agent` when you want a completely fresh start, including a new control agent session.

### Other Options

```bash
# Use a different control agent
orch-monitor --agent claude
```

If the daemon is not running, the TUI will show an error notification. Start the daemon with:

```bash
orch ps
```

### Development

```bash
cd orch-monitor-tui
uv sync
uv run python -m orch_monitor
```

## Keybindings

| Key | Action |
|-----|--------|
| `?` | Show help screen with keybindings and workflow tips |
| `q` | Quit |
| `r` | Refresh data |
| `tab` | Switch between Runs/Issues tabs |
| `f` | Filter runs by status |
| `ctrl+f` | Clear all filters |
| `up/down` | Navigate list |
| `enter` | Select item (attach to run / open issue in `$EDITOR`*) |
| `a` | Attach to selected run's session |
| `s` | Stop selected run |
| `X` | Kill session (force terminate) |
| `n` | Create new run for selected issue |
| `o` | Open issue in `$EDITOR`* |
| `x` | Close issue |

*When running inside a multiplexer (tmux/zellij), opening issues in `$EDITOR` creates a new multiplexer tab/window, allowing you to edit without leaving the monitor. Outside a multiplexer, the TUI suspends while the editor is open.

## Configuration

The TUI respects the same configuration as the Go `orch` CLI:

- `ORCH_PROJECT` environment variable (project identity: repo URL or normalized repo ID)
- `.orch/config.yaml` found by searching upward from current directory
- `.orch/config.yaml` in the selected project workspace

## Architecture

```
orch_monitor/
  __init__.py    - Package initialization
  __main__.py    - Entry point and layout launchers
  app.py         - Main Textual application (RunsDashboard, IssuesDashboard, OrchMonitorApp)
  config.py      - Configuration management and socket path resolution
  daemon.py      - DaemonClient for communicating with Go daemon via Unix socket
  models.py      - Data models (Issue, Run, Event, Status)
  multiplexer.py - Multiplexer abstraction (Strategy pattern for tmux/zellij)
  widgets.py     - Custom Textual widgets (RunTable, IssueTable, DetailPanel)
```

The TUI uses a daemon-centric architecture:
- Runs/issues data comes from the Go daemon via Unix socket
- Control-agent prompt/config comes from daemon (`get_control_agent_config`)
- Control-agent session file is managed locally at `.orch/control-session.json`
- Automatic refresh via polling (configurable interval)

## Daemon Communication

The TUI communicates with the orch daemon via Unix socket at `$PROJECT_ROOT/.orch/daemon.sock`:

- `list_runs` - List all runs with optional status filter
- `list_issues` - List all issues
- `get_run` - Get details for a specific run
- `get_issue` - Get details for a specific issue
- `get_control_agent_config` - Fetch control-agent prompt and launch config
- `send` - Send a message to a running agent

## Differences from Go Monitor

This Python TUI provides a simpler, more focused interface:

- No integrated chat pane (use `orch attach` for direct interaction)
- Simplified layout with tabs instead of multi-pane tmux windows
- Focus on monitoring and quick actions

The Go monitor remains available for users who prefer the integrated experience.
