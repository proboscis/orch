# Getting Started with orch

This guide will take you from zero to your first working agent run in about 5 minutes.

## Prerequisites

Before installing orch, ensure you have:

1. **Go 1.25+** - For building orch from source (or use pre-built binaries)
2. **tmux** or **zellij** - Terminal multiplexer for agent sessions
3. **An LLM CLI** - At least one of:
   - [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude`)
   - [OpenCode](https://github.com/sst/opencode) (`opencode`)
   - [Codex](https://github.com/openai/codex) (`codex`)
   - [Gemini CLI](https://github.com/google/gemini-cli) (`gemini`)
4. **Git** - For worktree management. Your repository must have an `origin`
   remote — orch derives the project identity from its URL. (For a local
   sandbox the URL does not need to exist on GitHub; it is used as an
   identity.)
5. **GitHub CLI** (`gh`) - Required for the PR workflow. Run `gh auth login` before creating PRs.
6. **uv** - Required only when installing or developing the Python TUI (`orch-monitor`)

### Quick prerequisite check

```bash
# Verify Go
go version

# Verify the terminal multiplexer you plan to use
tmux -V                 # or: zellij --version

# Verify your LLM CLI (example with claude)
claude --version

# Verify GitHub CLI authentication for PR workflows
gh auth status
```

## Installation

### Option 1: Install script (recommended)

```bash
curl -sSL https://raw.githubusercontent.com/proboscis/orch/main/install.sh | bash
```

Prefer to have your AI agent do the whole setup (binary + skill + guided
first run)? Follow the [Agent Install Runbook](./agent-install.md) instead.

### Option 2: Download pre-built binary

```bash
# macOS (Apple Silicon)
curl -L https://github.com/proboscis/orch/releases/latest/download/orch-darwin-arm64 -o orch
chmod +x orch
sudo mv orch /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/proboscis/orch/releases/latest/download/orch-darwin-amd64 -o orch
chmod +x orch
sudo mv orch /usr/local/bin/

# Linux (x86_64)
curl -L https://github.com/proboscis/orch/releases/latest/download/orch-linux-amd64 -o orch
chmod +x orch
sudo mv orch /usr/local/bin/
```

### Option 3: Install from source

```bash
go install github.com/proboscis/orch/cmd/orch@latest
```

### Option 4: Build from a local checkout

For the CLI and daemon only:

```bash
make build
make install-cli
```

The default `make` target installs both the CLI and the Python TUI. It requires
`uv` because it runs the TUI installer:

```bash
make
```

### Verify installation

```bash
orch --help
```

## Initialize your project

Navigate to your git repository and initialize orch:

```bash
cd /path/to/your/repo

# Create the config directory
mkdir -p .orch

# Create a minimal config file
cat > .orch/config.yaml << 'EOF'
agent: claude
base_branch: main
EOF
```

### Register the project with the daemon (required)

The daemon resolves every command through a project identity derived from
your `origin` remote URL (normalized to `<owner>-<repo>`). Map that identity
to your checkout once:

```bash
orch daemon repo register "$(pwd)"
# => Registered repo mapping: <owner>-<repo> -> /path/to/your/repo
```

Without this mapping, commands fail with
`unknown project_id "…" (register daemon project mapping)`. The mapping is
stored in `~/.config/orch/projects/<project_id>.yaml` and takes effect
immediately — no daemon restart needed.

### Setting up the issues directory

orch needs a place to store issues. Configure it in `.orch/config.yaml`.

**Option A: Use a separate issues directory (recommended for teams)**

```bash
# Create an issues directory
mkdir -p ~/orch-issues/issues

# Tell orch where to find issues
cat >> .orch/config.yaml << 'EOF'
issues:
  path: ~/orch-issues
EOF
```

**Option B: Keep issues in the same repo**

```bash
# Create issues directory in your repo
mkdir -p issues

# Update config
cat >> .orch/config.yaml << 'EOF'
issues:
  backend: local
  path: ./issues
EOF
```

## Create your first issue

Create issues through the CLI so orch validates them and writes them to the
configured issue store:

```bash
orch issue create my-first-issue --title "Add a hello world function" <<'EOF'
Create a simple function in this repository that prints "Hello, World!".

Requirements:
- Create a new file with an appropriate name for the project.
- Make the function callable.
- Add a brief comment explaining what it does.
EOF
```

Use `--edit` instead when you want to write the body in your editor:

```bash
orch issue create my-first-issue --title "Add a hello world function" --edit
```

### Advanced: issue file layout

The file backend stores generated documents under an `issues/` subdirectory
of the configured `issues.path`, or under `Issues/` when that directory already
exists. For example, `path: ./issues` normally stores this issue at
`./issues/issues/my-first-issue.md`. Treat this as storage detail; use
`orch issue create`, `orch issue show`, and `orch issue list` for normal work.

## Daemon and worker start automatically

The daemon starts on your first orch command, and the **worker** — the
process that actually launches agent sessions — auto-starts on demand when a
run targets this host (ADR-0002), including after reboots. Verify with:

```bash
orch worker status   # expect: Local Process: running / Master Registration: active
```

Set `ORCH_WORKER_AUTOSTART=0` to disable autostart and manage workers by hand
with `orch worker start`.

## Run your first agent

Start an agent to work on the issue:

```bash
orch run my-first-issue --no-pr
```

`--no-pr` keeps this first exercise local. Omit it in a repository where you
want the agent to commit, push, and open a pull request.

This will:

1. Create a new git worktree for isolation
2. Create a new git branch
3. Start your configured agent in a terminal multiplexer (tmux or zellij)
4. Send the issue content as the initial prompt

The command returns immediately - the agent runs in the background.

### First run: expect a trust prompt

On a fresh machine or directory, agent CLIs show a one-time interactive gate
(e.g. "Do you trust the contents of this directory?") and the run parks at
`waiting`. This is the normal interaction loop, not a failure:

```bash
orch capture my-first-issue   # see what the agent is asking
orch send my-first-issue ""   # empty message = press Enter (accept the default)
# or take over the terminal directly:
orch attach my-first-issue    # answer, then detach (see below)
```

## Check status

See what's running:

```bash
orch ps
```

The run normally moves through `booting` and `running`, then reaches
`waiting` when the agent's input box is available. Use `orch capture` before
deciding whether it needs an answer or has finished its turn.

### Status meanings

| Status | What it means |
|--------|---------------|
| `queued` | Run created, starting soon |
| `booting` | Agent is launching |
| `running` | Agent is actively working |
| `waiting` | The agent's input box is free — it needs your input, or just finished its turn (`orch capture` to tell which) |
| `pr_open` | Agent created a PR |
| `done` | Task completed |
| `failed` | Something went wrong |

## Interact with the agent

Attach to the agent's terminal multiplexer session:

```bash
orch attach my-first-issue
```

This opens the tmux or zellij session where you can:

- Watch the agent work in real-time
- Type messages to the agent
- Paste images (if the agent supports it)
- Provide input when the agent asks questions

**Detach without stopping the agent**: press `Ctrl+B` then `D` in tmux, or
`Ctrl+O` then `D` in zellij.

## See the result

Wait for the agent to finish its current turn, then read its report before
inspecting the result:

```bash
orch wait my-first-issue --timeout 600
orch capture my-first-issue

# View run details
orch show my-first-issue

# If a PR was created, it will show the URL
```

## Stop a run

If you need to stop the agent:

```bash
# Stop all runs for an issue
orch stop my-first-issue

# Stop a specific run
orch stop my-first-issue#20260120-163045

# Mark the issue complete when you have accepted the result
orch resolve my-first-issue
```

## Next steps

- Learn the [core concepts](./concepts.md) (Issue, Run, Event, etc.)
- Set up [remote usage](./remote-usage.md) for server-based orchestration
- Configure [different agents](./agents/claude.md)
- Set up [backend integrations](./backends/file.md) (GitHub)
- Explore all [CLI commands](./reference/commands.md)
- Use [SQL queries](./reference/query.md) to analyze your runs

## Next Level

Once you're comfortable with the basics, explore these power-user features:

### orch-monitor TUI

A visual dashboard for managing issues and runs:

```bash
# Install the TUI
uv tool install ./orch-monitor-tui

# Launch or attach to it
orch-monitor

# Restart with a fresh layout and control agent session
orch-monitor --new-control-agent
```

Use bare `orch-monitor` for the first launch. The `--new` flag only restarts
the layout while resuming an existing control agent session.

Features:
- See all issues and runs at a glance
- Start runs with a keypress
- Attach to agents directly from the UI
- Chat with a control agent to manage tasks

See the [orch-monitor guide](./orch-monitor.md) for details.

### Control Agent

Use a persistent AI agent to manage your orch workflow through conversation:

```bash
# Start or attach to control agent
orch agent

# Force a new session
orch agent --new
```

Ask it to create issues, start runs, check status—all through natural language.

### Daily Workflow Patterns

Learn efficient patterns for working with orch day-to-day:
- Morning routines for checking overnight progress
- How to handle waiting agents
- Running multiple agents in parallel
- Reviewing and merging agent PRs

See the [Daily Workflow guide](./daily-workflow.md).

### View Agent Changes

Quickly see what an agent has changed:

```bash
# Show full diff
orch diff my-issue

# Show just the stats
orch diff --stat my-issue
```

## Troubleshooting

### `project identity required: failed to resolve git remote`

The repository has no `origin` remote, or you ran the command outside the
repository. Add a remote (`git remote add origin …`) and run orch commands
from inside the repo — they are project-scoped by your current directory.
Alternatively set `ORCH_PROJECT` explicitly.

### `unknown project_id "…" (register daemon project mapping)`

The project is not registered with the daemon. Run
`orch daemon repo register "$(pwd)"` from the repository root (see
[Register the project with the daemon](#register-the-project-with-the-daemon-required)).

### `no active workers available`

The target host has no worker. On the master's own host workers auto-start
(ADR-0002); if you still see this, check `ORCH_WORKER_AUTOSTART` and the
daemon log, or run `orch worker start` by hand.

### Run fails immediately

Enable debug output:

```bash
orch run --verbose my-issue
# or
ORCH_DEBUG=1 orch run my-issue
```

### Agent seems stuck

Check if it's waiting for input:

```bash
orch ps  # Look for "waiting" status
orch attach my-issue  # Connect and provide input
```

### System state seems corrupted

Run the repair command:

```bash
orch repair
```

### Check daemon logs

The daemon is global (one per machine, not per project):

```bash
# macOS
tail -f ~/Library/Logs/orch/daemon.log

# Linux (XDG state dir)
tail -f ~/.local/state/orch/daemon.log
```
