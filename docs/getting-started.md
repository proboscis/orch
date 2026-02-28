# Getting Started with orch

This guide will take you from zero to your first working agent run in about 5 minutes.

## Prerequisites

Before installing orch, ensure you have:

1. **Go 1.22+** - For building orch from source (or use pre-built binaries)
2. **tmux** or **zellij** - Terminal multiplexer for agent sessions
3. **An LLM CLI** - At least one of:
   - [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude`)
   - [OpenCode](https://github.com/sst/opencode) (`opencode`)
   - [Codex](https://github.com/openai/codex) (`codex`)
   - [Gemini CLI](https://github.com/google/gemini-cli) (`gemini`)
4. **Git** - For worktree management

### Quick prerequisite check

```bash
# Verify Go
go version

# Verify tmux
tmux -V

# Verify your LLM CLI (example with claude)
claude --version
```

## Installation

### Option 1: Download pre-built binary (recommended)

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

### Option 2: Install from source

```bash
go install github.com/proboscis/orch/cmd/orch@latest
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

### Setting up the issues directory

orch needs a place to store issues. You have two options:

**Option A: Use a separate issues directory (recommended for teams)**

```bash
# Create an issues directory
mkdir -p ~/orch-issues/issues

# Tell orch where to find issues
export ORCH_ISSUES_ROOT=~/orch-issues
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

An issue is a markdown file describing a task for the agent:

```bash
# Create an issue file
cat > ~/orch-issues/issues/my-first-issue.md << 'EOF'
---
type: issue
id: my-first-issue
title: Add a hello world function
status: open
---

# Add a hello world function

Create a simple function in this repository that prints "Hello, World!".

## Requirements
- Create a new file with appropriate naming for the project
- The function should be callable
- Add a brief comment explaining what it does
EOF
```

Or use the CLI:

```bash
orch issue create my-first-issue --title "Add a hello world function" --edit
```

## Run your first agent

Start an agent to work on the issue:

```bash
orch run my-first-issue
```

This will:
1. Create a new git worktree for isolation
2. Create a new git branch
3. Start a tmux session with your configured agent
4. Send the issue content as the initial prompt

The command returns immediately - the agent runs in the background.

## Check status

See what's running:

```bash
orch ps
```

Example output:
```
ISSUE            STATUS   RUN                 AGENT   UPDATED
my-first-issue   running  20260120-163045     claude  2m ago
```

### Status meanings

| Status | What it means |
|--------|---------------|
| `queued` | Run created, starting soon |
| `booting` | Agent is launching |
| `running` | Agent is actively working |
| `waiting` | Agent needs your input |
| `pr_open` | Agent created a PR |
| `done` | Task completed |
| `failed` | Something went wrong |

## Interact with the agent

Attach to the agent's terminal session:

```bash
orch attach my-first-issue
```

This opens the tmux session where you can:
- Watch the agent work in real-time
- Type messages to the agent
- Paste images (if the agent supports it)
- Provide input when the agent asks questions

**Detach without stopping the agent**: Press `Ctrl+B` then `D`

## See the result

When the agent finishes (status becomes `done` or `pr_open`):

```bash
# Check final status
orch ps

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
```

## Next steps

- Learn the [core concepts](./concepts.md) (Issue, Run, Event, etc.)
- Set up [remote usage](./remote-usage.md) for server-based orchestration
- Configure [different agents](./agents/claude.md)
- Set up [backend integrations](./backends/file.md) (GitHub, Linear)
- Explore all [CLI commands](./reference/commands.md)
- Use [SQL queries](./reference/query.md) to analyze your runs

## Next Level

Once you're comfortable with the basics, explore these power-user features:

### orch-monitor TUI

A visual dashboard for managing issues and runs:

```bash
# Install the TUI
uv tool install ./orch-monitor-tui

# Launch it
orch-monitor --new
```

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

```bash
tail -f .orch/daemon.log
```
