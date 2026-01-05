# orch-monitor - Python Textual TUI

Python Textual-based TUI for `orch monitor` - an alternative to the Go implementation.

## Overview

This is a Python implementation of the orch monitor TUI using the [Textual](https://textual.textualize.io/) framework. It provides:

- Issue dashboard panel - list issues with status
- Run dashboard panel - list runs with status, agent, elapsed time
- Issue detail panel - show issue content
- Run detail panel - show run events/capture
- Keybindings for navigation and actions
- Real-time auto-refresh every 5 seconds
- Integration with orch CLI commands

## Installation

```bash
cd orch-monitor-tui
uv sync
```

## Usage

### Run from source

```bash
cd orch-monitor-tui
uv run python -m orch_monitor
```

### Install as package

```bash
cd orch-monitor-tui
uv pip install -e .
orch-monitor
```

### Set vault path

The monitor reads vault configuration from:
1. `ORCH_VAULT` environment variable
2. `.orch/config.yaml` in the current directory

```bash
export ORCH_VAULT=/path/to/vault
uv run python -m orch_monitor
```

Or run from the repository root where `.orch/config.yaml` exists.

## Keybindings

### Global
- `q` - Quit
- `r` - Refresh data
- `tab` - Switch between Issues and Runs panels

### Issues Panel
- `↑`/`↓` - Navigate
- `enter` - Show issue detail

### Runs Panel
- `↑`/`↓` - Navigate
- `enter` - Show run detail
- `a` - Attach to run's tmux session (exits TUI)
- `s` - Stop run

## Features

### Implemented
- [x] Issue dashboard with ID, status, title
- [x] Run dashboard with issue, run ID, status, agent, elapsed time, branch
- [x] Issue detail panel showing full issue content
- [x] Run detail panel showing run metadata and recent events
- [x] Keybindings for navigation and actions
- [x] Real-time auto-refresh (5 second interval)
- [x] Integration with orch CLI (attach, stop)
- [x] Reads .orch/config.yaml for vault path
- [x] CSS styling for improved layout

### Not Yet Implemented
- [ ] Filter dialog for status filtering
- [ ] Mouse support
- [ ] Search within panels
- [ ] Syntax highlighting for issue/run content

## Architecture

### Modules

- `models.py` - Data models (Issue, Run, Event, Status enums)
- `vault.py` - Vault parser for reading issues/runs from filesystem
- `config.py` - Configuration loader for .orch/config.yaml
- `app.py` - Main Textual application with panels and UI logic
- `__main__.py` - CLI entry point

### Data Flow

1. Load config from `.orch/config.yaml` or `ORCH_VAULT`
2. Initialize VaultStore with vault path
3. Scan vault for issues (markdown files with `type: issue` frontmatter)
4. Scan runs directory for run files
5. Parse events from run files
6. Display in DataTable widgets
7. Auto-refresh every 5 seconds

## Comparison with Go Version

**Advantages:**
- Faster iteration on UI changes
- Hot-reload CSS for styling
- Rich widget library from Textual
- Easier to modify and extend

**Parity:**
- Reads same vault structure
- Calls same orch CLI commands
- Shows same core information

**Missing:**
- Filter dialog (placeholder implemented)
- Some advanced features from Go version

## Development

### Dependencies

Managed with `uv`:
- `textual>=7.0.0` - TUI framework
- `pyyaml>=6.0.3` - Config file parsing

### Project Structure

```
orch-monitor-tui/
├── orch_monitor/
│   ├── __init__.py
│   ├── __main__.py
│   ├── app.py
│   ├── config.py
│   ├── models.py
│   └── vault.py
├── pyproject.toml
├── .python-version
└── README.md
```
