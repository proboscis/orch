# orch-monitor

`orch-monitor` is orch's Python/Textual terminal user interface. It presents
daemon-backed Runs and Issues dashboards and launches a control-agent terminal
beside them in a tmux or Zellij session.

This is the only orch TUI. The former Go `orch monitor` subcommand has been
removed; use the standalone `orch-monitor` executable.

## Install

From the orch repository root, install the CLI and TUI together:

```bash
make install
```

Or install only this package:

```bash
uv tool install ./orch-monitor-tui
```

The package exposes the `orch-monitor` console script.

## Start

Use the bare command for the first launch:

```bash
orch-monitor
```

The launcher resolves the project and daemon, then opens this multiplexer
layout:

```text
+---------------------------+------------------------------+
| Runs dashboard            | Issues dashboard             |
|                           +------------------------------+
|                           | Control agent terminal       |
+---------------------------+------------------------------+
```

Bare `orch-monitor` attaches when the project already has a live monitor
session.

### Layout and control-agent restarts

```bash
# Replace the layout and resume its saved control-agent session.
orch-monitor --new

# Replace both the layout and control-agent session.
orch-monitor --new-control-agent
```

When replacing an existing layout, `--new` requires a usable saved session in
`.orch/control-session.json`. It fails before killing the current layout when
that state is missing. `--new-control-agent` implies `--new` and clears the old
control-agent state intentionally.

The launcher requests repository context and control-agent configuration from
the daemon, writes the returned content to `ORCH_CONTROL_PROMPT.md`, and asks
the selected agent to read it. Both this generated prompt and
`.orch/control-session.json` are ignored by Git.

## Launcher options

| Option | Purpose |
|--------|---------|
| `--project PROJECT` | Select a project by Git repository URL or normalized repository ID. |
| `--runs` | Open only the Runs dashboard. |
| `--issues` | Open only the Issues dashboard. |
| `--agent AGENT` | Override the configured control agent. |
| `--new` | Recreate the layout while retaining the saved control-agent session. |
| `--new-control-agent` | Recreate the layout and control-agent session. |
| `--multiplexer {tmux,zellij}`, `-m` | Override multiplexer selection. |
| `--verbose`, `-v` | Print launcher diagnostics to standard error. |
| `--remote ADDRESS` | Override remote daemon routing; an empty value forces local routing. |
| `--list` | List registered monitor instances for the project. |
| `--kill MONITOR_ID` | Kill one registered monitor instance. |
| `--kill-all` | Kill all registered monitor instances for the project. |

For example:

```bash
orch-monitor --project github.com/proboscis/orch
orch-monitor --multiplexer tmux
orch-monitor --remote master.example:7777 --list
```

## Keybindings

All dashboard applications bind `?` for help, `q` to quit, `Ctrl+C` to quit
with priority, and `r` to refresh. Run and issue tables support `j`/`k` for row
movement and `g`/`G` for top and bottom.

### Combined application

`OrchMonitorApp` is the reusable combined application. The default
multiplexer layout runs the separate Runs and Issues applications documented
below.

| Key | Action |
|-----|--------|
| `Enter` | Select the highlighted run or issue. |
| `a` | Attach to a run. |
| `s` | Stop a run. |
| `X` | Kill a run's terminal session. |
| `n` | Start a run for an issue. |
| `o` | Open an issue in the editor. |
| `x` | Close an issue. |
| `f` | Filter the focused table. |
| `Ctrl+f` | Clear filters on the focused table. |
| `d` | Show a run diff. |
| `Tab` | Switch between Runs and Issues. |

### Runs pane (`--runs`)

| Key | Action |
|-----|--------|
| `Enter` | Attach to the highlighted run. |
| `s` | Stop the highlighted run. |
| `X` | Kill the highlighted run's terminal session. |
| `f` | Open run filters. |
| `Ctrl+f` | Clear run filters. |
| `d` | Show the highlighted run's diff. |

The Runs pane uses `Enter`, not `a`, for attach. The `a` binding exists on the
combined application.

### Issues pane (`--issues`)

| Key | Action |
|-----|--------|
| `Enter`, `o` | Open the highlighted issue in the editor. |
| `n` | Open the agent selector for a new run. |
| `x` | Close the highlighted issue. |
| `f` | Open issue filters. |
| `Ctrl+f` | Clear issue filters. |

The agent selector uses `Esc` to cancel, `Enter` to confirm, `j`/`k` to move,
and `1` through `9` for quick selection.

The Runs detail panel uses `Tab` to move between tabs, `1`/`2`/`3` for Stats,
Issue, and Changes, `j`/`k` or arrows to scroll, and `g`/`G` for top and bottom.

## Status display

| Status | Short display | Color |
|--------|---------------|-------|
| `queued` | `queue` | white |
| `booting` | `boot` | green |
| `running` | `run` | green |
| `waiting` | `wait` | yellow |
| `rate_limited` | `rlimit` | yellow |
| `pr_open` | `pr` | cyan |
| `done` | `done` | blue |
| `failed` | `fail` | red |
| `canceled` | `cancel` | dim |
| `unknown` | `?` | magenta |

## Configuration

The TUI uses orch project/client resolution and reads `.orch/config.yaml`.
Monitor defaults are nested under `monitor`:

```yaml
monitor:
  default_run_statuses:
    - queued
    - booting
    - running
    - waiting
    - rate_limited
    - pr_open
  default_issue_statuses:
    - open
  default_issue_filter:
    tags:
      - active
    tag_mode: any  # any = OR, all = AND
```

These defaults initialize the UI when `.orch/monitor-filters.yaml` does not
exist. Interactive filter changes are persisted in that file and take
precedence on later launches.

## Architecture

The daemon is the authoritative source for issue, run, monitor, and Git state.
The TUI requests state and actions through the daemon API and keeps display
formatting, control-agent session persistence, and terminal interaction local.

```text
+----------------------+       daemon API       +----------------------+
| Textual dashboards   | <--------------------> | orch daemon          |
| + layout launcher    |                        | authoritative state  |
+----------------------+                        +----------------------+
```

`orch_monitor/app.py` re-exports the Hy dashboard implementations;
`orch_monitor/__main__.py` provides the console entrypoint and layout
launchers. `widgets.py` contains the tables and detail tabs, while
`multiplexer.py` implements tmux and Zellij integration.

## Development

```bash
cd orch-monitor-tui
uv sync --all-extras
uv run python -m orch_monitor --help
uv run pytest tests/ -v
```

See the [full orch-monitor guide](../docs/orch-monitor.md) for workflow details
and the complete binding reference.
