# orch-monitor TUI

`orch-monitor` is orch's terminal user interface. It is implemented in Python
with Textual and displays run and issue state supplied by the orch daemon. The
default launcher also opens a control-agent terminal beside the dashboards.

The former Go `orch monitor` subcommand has been removed. Use the standalone
`orch-monitor` executable for all TUI and monitor-instance operations.

## Installation

From the orch repository root, `make install` installs both the Go CLI and the
Python TUI. Its `install-tui` dependency reinstalls the local TUI package:

```bash
make install
```

To install only the TUI with uv:

```bash
uv tool install ./orch-monitor-tui
```

Confirm that the executable is available:

```bash
orch-monitor --help
```

## First launch

Start with the bare command:

```bash
orch-monitor
```

On first use, the launcher connects to the daemon, resolves the current
project, creates a tmux or Zellij session, and opens three working areas:

```text
+---------------------------+------------------------------+
| Runs dashboard            | Issues dashboard             |
|                           +------------------------------+
|                           | Control agent terminal       |
+---------------------------+------------------------------+
```

If that monitor session already exists, bare `orch-monitor` attaches to it.
The control agent is a real terminal pane managed by the Python launcher; it is
not a chat widget inside either Textual dashboard.

### Restarting sessions

Use `--new` to replace the multiplexer layout while preserving the saved
control-agent session:

```bash
orch-monitor --new
```

When an existing layout is being replaced, `--new` first requires a resumable
control-agent ID in `.orch/control-session.json`. If the file has no usable
session, the command exits with an error before destroying the current layout.
Use bare `orch-monitor` to attach to the layout as-is, or use
`--new-control-agent` to deliberately start over.

Use `--new-control-agent` to replace both the layout and control-agent session:

```bash
orch-monitor --new-control-agent
```

This option implies `--new` and clears the saved control-agent session before
launching the replacement layout.

## Command-line options

| Option | Behavior |
|--------|----------|
| `--project PROJECT` | Select a project by Git repository URL or normalized repository ID. |
| `--runs` | Run only the Runs Textual dashboard; used by a multiplexer pane and useful for focused testing. |
| `--issues` | Run only the Issues Textual dashboard; used by a multiplexer pane and useful for focused testing. |
| `--agent AGENT` | Override the configured control-agent command. |
| `--new` | Restart an existing layout and preserve its saved control-agent session. |
| `--new-control-agent` | Restart the layout with a fresh control-agent session. |
| `--multiplexer {tmux,zellij}`, `-m` | Override automatic multiplexer selection. |
| `--verbose`, `-v` | Print launcher timing and diagnostic messages to standard error. |
| `--remote ADDRESS` | Use a remote daemon address or alias, overriding `ORCH_REMOTE` and the client default. An empty value forces local routing. |
| `--list` | List monitor instances registered for the selected project. |
| `--kill MONITOR_ID` | Kill one registered monitor instance. |
| `--kill-all` | Kill every registered monitor instance for the selected project. |

`--list`, `--kill`, and `--kill-all` are mutually exclusive. Their project
scope is resolved in the same way as the TUI, including `--project` and remote
client configuration.

Examples:

```bash
orch-monitor --project github.com/proboscis/orch
orch-monitor --multiplexer tmux
orch-monitor --remote master.example:7777
orch-monitor --remote master.example:7777 --list
orch-monitor --kill MONITOR_ID
```

## Keybindings

The tables below are generated from the binding declarations in
`orch_monitor_app.hy`, `runs_dashboard.hy`, `issues_dashboard.hy`,
`agent_screen.hy`, and `widgets.py`.

### Combined dashboard application

These are the bindings on the reusable `OrchMonitorApp`. The default
multiplexer layout runs the separate Runs and Issues applications documented
below. In the combined application, the action selected by a key such as
`Enter`, `f`, or `Ctrl+f` depends on which table has focus.

| Key | Action |
|-----|--------|
| `?` | Open help. |
| `q` | Quit. |
| `Ctrl+C` | Quit with priority. |
| `r` | Refresh data. |
| `Enter` | Select the highlighted run or issue. |
| `a` | Attach to the highlighted run. |
| `s` | Stop the highlighted run. |
| `X` | Kill the highlighted run's terminal session. |
| `n` | Start a new run for the highlighted issue. |
| `o` | Open the highlighted issue in the editor. |
| `x` | Close the highlighted issue. |
| `f` | Open the filter for the focused table. |
| `Ctrl+f` | Clear filters for the focused table. |
| `d` | Show the highlighted run's diff. |
| `Tab` | Switch focus between Runs and Issues. |

### Runs dashboard (`--runs`)

| Key | Action |
|-----|--------|
| `?` | Open help. |
| `q` | Quit. |
| `Ctrl+C` | Quit with priority. |
| `r` | Refresh runs. |
| `Enter` | Attach to the highlighted run. |
| `s` | Stop the highlighted run. |
| `X` | Kill the highlighted run's terminal session. |
| `f` | Open run filters. |
| `Ctrl+f` | Clear run filters. |
| `d` | Show the highlighted run's diff. |

The standalone Runs pane does not bind `a`; use `Enter` to attach there. The
`a` binding belongs to the combined application.

### Issues dashboard (`--issues`)

| Key | Action |
|-----|--------|
| `?` | Open help. |
| `q` | Quit. |
| `Ctrl+C` | Quit with priority. |
| `r` | Refresh issues. |
| `Enter` | Open the highlighted issue in the editor. |
| `o` | Open the highlighted issue in the editor. |
| `n` | Start a new run for the highlighted issue. |
| `x` | Close the highlighted issue. |
| `f` | Open issue filters. |
| `Ctrl+f` | Clear issue filters. |

Run and issue tables also support `j`/`k` for row movement and `g`/`G` for the
top and bottom. Textual's normal arrow-key table navigation remains available.

### Agent selection screen

Pressing `n` on an issue opens the agent selector.

| Key | Action |
|-----|--------|
| `Esc` | Cancel. |
| `Enter` | Start with the selected agent or preset. |
| `j`, `k` | Move down or up. |
| `1` through `9` | Immediately select the corresponding entry. |

### Run detail tabs

The Runs dashboard detail area has Stats, Issue, and Changes tabs.

| Key | Action |
|-----|--------|
| `Tab` | Select the next detail tab. |
| `1` | Select Stats. |
| `2` | Select Issue. |
| `3` | Select Changes. |
| `j`, `Down` | Scroll down. |
| `k`, `Up` | Scroll up. |
| `g` | Jump to the top. |
| `G` | Jump to the bottom. |

## Status colors

The Runs table shortens status names and applies the following colors:

| Status | Display | Color | Meaning |
|--------|---------|-------|---------|
| `queued` | `queue` | white | The run is waiting to start. |
| `booting` | `boot` | green | The agent is starting. |
| `running` | `run` | green | The agent is working. |
| `waiting` | `wait` | yellow | The agent is waiting for input. |
| `rate_limited` | `rlimit` | yellow | Progress is paused by an API rate limit. |
| `pr_open` | `pr` | cyan | The run has an open pull request. |
| `done` | `done` | blue | The run completed. |
| `failed` | `fail` | red | The run failed. |
| `canceled` | `cancel` | dim | The run was canceled. |
| `unknown` | `?` | magenta | The daemon returned an unclassified run state. |

## Filters and configuration

Press `f` in a dashboard to edit filters. Filter state is persisted in
`.orch/monitor-filters.yaml`; `Ctrl+f` clears the filters for the focused
dashboard.

The `monitor` section of `.orch/config.yaml` supplies initial filters when no
persisted monitor filter file exists. `monitor.default_issue_filter` can
select issue tags and choose OR or AND matching:

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

Once filters have been saved interactively, the persisted file takes
precedence over these defaults.

## Control agent

During a normal daemon-backed layout launch, the launcher requests the
control-agent configuration and repository context from the daemon. It writes
the returned prompt to `ORCH_CONTROL_PROMPT.md` in the project root, then starts
the configured agent with an instruction to read that file. The generated
prompt is runtime state and is ignored by Git.

The resumable control-agent ID is stored locally in:

```text
.orch/control-session.json
```

The file records `session_id` and `agent_type`. `--new` checks it before
replacing an existing layout; `--new-control-agent` clears it. The file is also
ignored by Git.

The control agent can run non-interactive orch commands such as `orch issue
create`, `orch run`, `orch ps`, `orch capture`, `orch send`, and `orch stop`.
Use the pane as an agent terminal; its input behavior is provided by the
selected agent, not by Textual keybindings.

## Architecture

The daemon is the source of truth for run, issue, monitor, and Git state. The
Python processes request data and actions through the daemon API; they keep
display formatting, control-agent session persistence, and terminal
interaction in the client.

```text
+-------------------+       daemon API       +----------------------+
| Python Textual UI | <--------------------> | orch daemon          |
| + layout launcher |                        | authoritative state  |
+-------------------+                        +----------------------+
```

The launcher automatically reconnects to or starts the daemon when necessary.
If startup or repair fails, it exits with the daemon error and suggests
`orch repair`.

## Typical workflow

1. Launch with `orch-monitor`.
2. Select the Issues pane and highlight an issue.
3. Press `n`, then select an agent with `Enter` or `1` through `9`.
4. Watch the run in the Runs pane.
5. Press `Enter` in the Runs pane to attach when interaction is needed.
6. Press `d` to inspect the run diff.

## See also

- [Configuration](./configuration.md)
- [Command reference](./reference/commands.md)
- [Daily workflow](./daily-workflow.md)
