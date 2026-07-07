# orch-monitor TUI

orch-monitor is a terminal user interface (TUI) for managing orch issues and runs visually. It provides a dashboard view of your workflow and integrates with the control agent for chat-based management.

`orch monitor` and `orch-monitor` are separate frontends. `orch monitor` is the
built-in Go TUI shipped as an `orch` subcommand, while `orch-monitor` is the
optional standalone Python TUI installed from `orch-monitor-tui`. Both read the
same daemon state, but they have separate binaries, launch flows, and docs.

## Installation

The TUI is packaged separately and can be installed with uv:

```bash
# Install from the orch-monitor-tui directory
uv tool install ./orch-monitor-tui

# Or install in development mode
cd orch-monitor-tui
uv pip install -e .
```

Verify installation:

```bash
orch-monitor --help
```

## Launching

```bash
# Start the TUI
orch-monitor

# Start with a fresh control agent session
orch-monitor --new

# Specify a project identity (repo URL or repoid)
orch-monitor --project github.com/owner/repo
```

## Interface Overview

The TUI is divided into three main panels:

```
┌─────────────────────────────────────────────────────────────────┐
│                         orch-monitor                            │
├─────────────────────────┬───────────────────────────────────────┤
│                         │                                       │
│   Issues Panel          │   Runs Panel                          │
│                         │                                       │
│   > fix-bug-123        │   20260115-093012  running   5m ago   │
│     add-feature-x       │   20260115-091530  pr_open   1h ago   │
│     optimize-db         │                                       │
│     update-deps         │                                       │
│                         │                                       │
├─────────────────────────┴───────────────────────────────────────┤
│                                                                 │
│   Control Agent Panel                                           │
│                                                                 │
│   Agent: How can I help you today?                              │
│   You: Create an issue for fixing the login timeout             │
│   Agent: I'll create that issue for you...                      │
│                                                                 │
│   > _                                                           │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Issues Panel (Left)

Displays all issues in your issues directory:
- Navigate with `j`/`k` or arrow keys
- Selected issue highlighted with `>`
- Shows issue status via color coding

### Runs Panel (Right)

Shows runs for the selected issue:
- Lists all runs with their status and last update time
- Automatically updates as runs progress
- Empty if no runs exist for selected issue

### Control Agent Panel (Bottom)

Interactive chat with the control agent:
- Type messages to create issues, start runs, check status
- Agent can perform orch operations on your behalf
- Maintains conversation history within the session

## Keybindings

### Global

| Key | Action |
|-----|--------|
| `?` | Show help overlay |
| `Tab` | Switch focus between panels |
| `q` | Quit orch-monitor |
| `Ctrl+C` | Force quit |

### Issues Panel

| Key | Action |
|-----|--------|
| `j` / `↓` | Move to next issue |
| `k` / `↑` | Move to previous issue |
| `Enter` | Focus on runs for this issue |
| `n` | Start a new run for selected issue |
| `e` | Edit the selected issue |
| `r` | Refresh issues list |

### Runs Panel

| Key | Action |
|-----|--------|
| `j` / `↓` | Move to next run |
| `k` / `↑` | Move to previous run |
| `Enter` / `a` | Attach to selected run |
| `d` | Show diff for selected run |
| `s` | Stop selected run |
| `l` | View run logs/events |
| `c` | Continue selected run |
| `r` | Refresh runs list |

### Control Agent Panel

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Ctrl+L` | Clear chat history |
| `Esc` | Return focus to issues panel |

## Status Colors

The TUI uses colors to indicate run status at a glance:

| Color | Status | Meaning |
|-------|--------|---------|
| 🟢 Green | `running` | Agent is actively working |
| 🟡 Yellow | `waiting` | Agent needs your input |
| 🔵 Blue | `pr_open` | PR created, ready for review |
| ⚪ White | `done` | Completed successfully |
| 🔴 Red | `failed` | Error occurred |
| ⚫ Gray | `queued` | Waiting to start |

## Working with the Control Agent

The control agent understands natural language for common operations:

### Creating Issues

```
You: Create an issue to fix the login timeout bug
Agent: I'll create that issue. What details should I include?
You: Session expires after 5 minutes, should be 30. Check src/auth/
Agent: Created issue 'fix-login-timeout'. Would you like me to start a run?
```

### Starting Runs

```
You: Start a run for fix-login-timeout
Agent: Starting run for fix-login-timeout with claude agent...
       Run 20260115-142030 is now running.
```

### Checking Status

```
You: What's running right now?
Agent: You have 2 active runs:
       - fix-login-timeout: running (started 10m ago)
       - update-deps: waiting (needs input about React version)
```

### Getting Help

```
You: How do I see the diff for a completed run?
Agent: You can press 'd' while a run is selected in the Runs panel,
       or from the command line: orch diff <run-ref>
```

## Workflow Example

1. **Launch orch-monitor**
   ```bash
   orch-monitor --new
   ```

2. **Create an issue via control agent**
   - Focus on the control agent panel (Tab)
   - Type: "Create an issue to add dark mode support"
   - Provide details when prompted

3. **Start a run**
   - Select the new issue in the Issues panel
   - Press `n` to start a new run
   - Or ask the control agent: "Start a run for add-dark-mode"

4. **Monitor progress**
   - Watch the status change in the Runs panel
   - Status updates automatically

5. **Help if waiting**
   - If status turns yellow (waiting), press `a` to attach
   - Provide the needed input
   - Press `Ctrl+B D` to detach

6. **Review when done**
   - When status turns blue (pr_open), press `d` to see diff
   - Review and merge the PR

## Tips

### Efficient Navigation

- Use `Tab` to quickly switch between panels
- `j`/`k` for vim-style navigation
- `?` anytime you forget a keybinding

### Multi-tasking

- Start several runs, then monitor all from one place
- Control agent can manage multiple issues while you watch progress
- Use the Runs panel to quickly switch between active runs

### Troubleshooting

If the TUI seems unresponsive:
1. Press `r` to refresh the current panel
2. Check if a run is actually waiting (`orch ps` in another terminal)
3. Try `q` and restart if needed

If control agent isn't responding:
1. It may be thinking - wait a moment
2. Press `Ctrl+L` to clear and try again
3. Restart with `--new` flag

## Configuration

The TUI respects orch configuration from `.orch/config.yaml`:

```yaml
# Affects TUI behavior
agent: claude          # Default agent for new runs
base_branch: main      # Default base branch
```

The monitor resolves configuration from `ORCH_PROJECT` and
`.orch/config.yaml` (`issues.path`).

## See Also

- [Daily Workflow](./daily-workflow.md) - Command-line workflow patterns
- [Commands Reference](./reference/commands.md) - Full CLI reference
- [orch agent](./reference/commands.md#orch-agent) - Standalone control agent
