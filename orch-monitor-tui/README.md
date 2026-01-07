# Orch Monitor TUI (Python/Textual)

Python Textual-based terminal user interface for `orch monitor`.

## Features

- **Issue Dashboard**: View all issues with status filtering
- **Run Dashboard**: Monitor active and completed runs with real-time status
- **Detail Panels**: View issue content and run events
- **Keybindings**: Quick navigation and actions
- **Real-time Updates**: Refresh data on demand

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
| `↑/↓` | Navigate list |
| `enter` | Select item (attach to run / start issue) |
| `a` | Attach to selected run's tmux session |
| `s` | Stop selected run |
| `n` | Create new run for selected issue |

## Configuration

The TUI respects the same configuration as the Go `orch` CLI:

- `ORCH_VAULT` environment variable
- `.orch/config.yaml` in the vault directory

## Architecture

- `models.py`: Data models (Issue, Run, Event, Status)
- `vault.py`: Vault reader for parsing markdown files
- `config.py`: Configuration management
- `widgets.py`: Custom Textual widgets (RunTable, IssueTable, DetailPanel)
- `app.py`: Main Textual application
- `__main__.py`: Entry point

## Differences from Go Monitor

This Python TUI provides a simpler, more focused interface:

- No integrated chat pane (use `orch attach` for direct interaction)
- Simplified layout with tabs instead of multi-pane tmux windows
- Focus on monitoring and quick actions

The Go monitor remains available for users who prefer the integrated experience.
