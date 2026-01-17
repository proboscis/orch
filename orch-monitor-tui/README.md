# Orch Monitor TUI (Python/Textual)

Python Textual-based terminal user interface for `orch monitor`.

## Features

- **Issue Dashboard**: View all issues with status filtering
- **Run Dashboard**: Monitor active and completed runs with real-time status
- **Detail Panels**: View issue content and run events
- **Keybindings**: Quick navigation and actions
- **Real-time Updates**: Automatic refresh with daemon-based data

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

Run from any directory (uses `ORCH_VAULT` env var or `.orch/config.yaml`):

```bash
orch-monitor
```

Or specify vault path:

```bash
orch-monitor --vault ~/my-vault
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
| `q` | Quit |
| `r` | Refresh data |
| `tab` | Switch between Runs/Issues tabs |
| `f` | Filter runs by status |
| `up/down` | Navigate list |
| `enter` | Select item (attach to run / start issue) |
| `a` | Attach to selected run's tmux session |
| `s` | Stop selected run |
| `n` | Create new run for selected issue |

## Configuration

The TUI respects the same configuration as the Go `orch` CLI:

- `ORCH_VAULT` environment variable
- `.orch/config.yaml` in the vault directory

## Architecture

```
orch_monitor/
  __init__.py   - Package initialization
  __main__.py   - Entry point
  app.py        - Main Textual application (RunsDashboard, IssuesDashboard, OrchMonitorApp)
  config.py     - Configuration management and socket path resolution
  daemon.py     - DaemonClient for communicating with Go daemon via Unix socket
  models.py     - Data models (Issue, Run, Event, Status)
  widgets.py    - Custom Textual widgets (RunTable, IssueTable, DetailPanel)
```

The TUI uses a daemon-only architecture:
- All data comes from the Go daemon via Unix socket
- No direct file/vault access
- Automatic refresh via polling (configurable interval)

## Daemon Communication

The TUI communicates with the orch daemon via Unix socket at `$VAULT/.orch/daemon.sock`:

- `list_runs` - List all runs with optional status filter
- `list_issues` - List all issues
- `get_run` - Get details for a specific run
- `get_issue` - Get details for a specific issue
- `send` - Send a message to a running agent

## Differences from Go Monitor

This Python TUI provides a simpler, more focused interface:

- No integrated chat pane (use `orch attach` for direct interaction)
- Simplified layout with tabs instead of multi-pane tmux windows
- Focus on monitoring and quick actions

The Go monitor remains available for users who prefer the integrated experience.
