package cli

const skillMainContent = `---
name: orch
description: Orchestrator for managing multiple LLM agents working on issues
---

# Orch - Multi-Agent Orchestrator

Orch is a command-line orchestrator for managing multiple LLM CLI agents (OpenCode, Claude, Codex, Gemini) using a unified vocabulary of issue/run/event.

## Quick Start

1. **Start work on an issue:**
` + "```" + `bash
   orch run my-issue-id
` + "```" + `

2. **Check status of all runs:**
` + "```" + `bash
   orch ps
` + "```" + `

3. **Interact with a running agent:**
` + "```" + `bash
   orch attach my-issue-id
   # Or use short ID from 'orch ps':
   orch attach a3b4
` + "```" + `

## Key Concepts

- **Issue**: A task or problem to work on (stored as markdown files)
- **Run**: An agent session working on an issue
- **Event**: State changes during a run (started, blocked, done, etc.)

## Skill Files

This skill package includes:
- **commands.md**: Complete command reference
- **workflows.md**: Common usage patterns
- **troubleshooting.md**: Debugging and repair
- **config.md**: Configuration options

## Common Commands

| Command | Purpose |
|---------|---------|
| ` + "`orch run <issue>`" + ` | Start work on an issue |
| ` + "`orch ps`" + ` | List all runs and their status |
| ` + "`orch attach <run>`" + ` | Interact with running agent |
| ` + "`orch monitor`" + ` | TUI dashboard for all runs |
| ` + "`orch stop <issue>`" + ` | Stop all runs for an issue |
| ` + "`orch continue <run>`" + ` | Resume a paused/blocked run |
| ` + "`orch repair`" + ` | Fix daemon/orphaned sessions |
`

const skillCommandsContent = `# Orch Command Reference

## Run Management

### orch run
Start a new run on an issue.

` + "```" + `bash
# Basic usage
orch run my-issue

# With specific agent
orch run my-issue --agent claude

# With model preset
orch run my-issue --preset opus:max

# With custom prompt
orch run my-issue --prompt "Focus on tests"

# Without creating a PR
orch run my-issue --no-pr

# Using existing worktree
orch run my-issue --worktree /path/to/worktree
` + "```" + `

### orch ps
List all runs with their status.

` + "```" + `bash
# Default output
orch ps

# JSON output
orch ps --json

# TSV output (for fzf/scripting)
orch ps --tsv

# Filter by status
orch ps --status running
orch ps --status blocked

# Filter by issue
orch ps --issue my-issue
` + "```" + `

### orch attach
Attach to a running agent session.

` + "```" + `bash
# By issue ID
orch attach my-issue

# By run reference
orch attach my-issue#20231220-100000

# By short ID (2-6 hex chars)
orch attach a3
orch attach a3b4c5
` + "```" + `

### orch stop
Stop runs for an issue.

` + "```" + `bash
# Stop all runs for an issue
orch stop my-issue

# Stop specific run
orch stop my-issue#20231220-100000

# Stop by short ID
orch stop a3b4
` + "```" + `

### orch continue
Resume a paused or blocked run.

` + "```" + `bash
# Continue with default behavior
orch continue my-issue

# Continue with a message
orch continue my-issue --message "Please proceed"

# Continue specific run
orch continue my-issue#20231220-100000
` + "```" + `

### orch send
Send a message to a running agent.

` + "```" + `bash
# Send message
orch send my-issue "Please also add tests"

# Send to specific run
orch send my-issue#20231220-100000 "Focus on error handling"
` + "```" + `

### orch exec
Execute a command in a run's worktree.

` + "```" + `bash
# Run a command
orch exec my-issue -- make test

# Run in specific run
orch exec my-issue#20231220-100000 -- npm run build
` + "```" + `

## Issue Management

### orch issue create
Create a new issue.

` + "```" + `bash
# Basic creation
orch issue create my-new-issue --title "Add feature X" --body "Description"

# From file
orch issue create my-issue --file issue.md
` + "```" + `

### orch issue list
List all issues.

` + "```" + `bash
orch issue list
orch issue list --json
orch issue list --status open
` + "```" + `

### orch show
Show issue or run details.

` + "```" + `bash
# Show issue
orch show my-issue

# Show run details
orch show my-issue#20231220-100000

# Show by short ID
orch show a3b4
` + "```" + `

### orch open
Open issue or run in browser/editor.

` + "```" + `bash
# Open issue
orch open my-issue

# Open run's PR
orch open a3b4 --pr
` + "```" + `

## Monitoring

### orch monitor
Launch TUI dashboard.

` + "```" + `bash
orch monitor
` + "```" + `

Key bindings:
- ` + "`j/k`" + `: Navigate up/down
- ` + "`Enter`" + `: Attach to selected run
- ` + "`s`" + `: Stop selected run
- ` + "`c`" + `: Continue blocked run
- ` + "`r`" + `: Refresh
- ` + "`q`" + `: Quit

### orch capture
Capture agent session to markdown.

` + "```" + `bash
# Capture run
orch capture my-issue

# Capture with output path
orch capture my-issue --output session.md

# Capture all completed runs
orch capture-all
` + "```" + `

## Daemon Management

### orch daemon
Manage the orch daemon.

` + "```" + `bash
# Check status
orch daemon status

# Start daemon
orch daemon start

# Stop daemon
orch daemon stop

# Restart daemon
orch daemon restart
` + "```" + `

### orch repair
Fix common issues with daemon and sessions.

` + "```" + `bash
# Repair all issues
orch repair

# Dry run
orch repair --dry-run
` + "```" + `

## Query and Debug

### orch query
Query runs with SQL.

` + "```" + `bash
# List running runs
orch query "SELECT * FROM runs WHERE status = 'running'"

# Count runs by issue
orch query "SELECT issue_id, count(*) FROM runs GROUP BY issue_id"

# Find recent runs
orch query "SELECT * FROM runs ORDER BY updated_at DESC LIMIT 10"
` + "```" + `

### orch log
View run logs.

` + "```" + `bash
orch log my-issue
orch log a3b4
` + "```" + `

### orch debug
Show internal state for debugging.

` + "```" + `bash
orch debug my-issue
orch debug a3b4
` + "```" + `

### orch schema
Show database schema for queries.

` + "```" + `bash
orch schema
` + "```" + `

## Utility Commands

### orch resolve
Mark a run as resolved (acknowledged).

` + "```" + `bash
orch resolve my-issue
orch resolve a3b4
` + "```" + `

### orch delete
Delete runs or issues.

` + "```" + `bash
# Delete specific run
orch delete run my-issue#20231220-100000

# Delete all runs for an issue
orch delete runs my-issue

# Delete issue and all its runs
orch delete issue my-issue
` + "```" + `

### orch models
List available models (requires opencode server).

` + "```" + `bash
orch models
` + "```" + `

### orch notify
Send test notification.

` + "```" + `bash
orch notify "Test message"
` + "```" + `
`

const skillWorkflowsContent = `# Orch Workflows

## Starting a New Task

### 1. Create or locate the issue
` + "```" + `bash
# Create a new issue
orch issue create my-feature --title "Implement feature X" --body "Details..."

# Or list existing issues
orch issue list
` + "```" + `

### 2. Start a run
` + "```" + `bash
# Start with default agent
orch run my-feature

# Or with specific preset
orch run my-feature --preset opus:max
` + "```" + `

### 3. Monitor progress
` + "```" + `bash
# Check status
orch ps

# Or use TUI dashboard
orch monitor
` + "```" + `

### 4. Interact if needed
` + "```" + `bash
# Attach to provide guidance
orch attach my-feature

# Or send a message
orch send my-feature "Please add error handling"
` + "```" + `

## Handling Blocked Runs

When a run status shows "blocked", the agent needs input:

### 1. Check why it's blocked
` + "```" + `bash
orch show my-feature
` + "```" + `

### 2. Provide input
` + "```" + `bash
# Attach and interact
orch attach my-feature

# Or send a message and continue
orch send my-feature "Here's the answer to your question"
orch continue my-feature
` + "```" + `

## Multi-Agent Orchestration

### Running multiple issues in parallel
` + "```" + `bash
# Start runs on different issues
orch run feature-a
orch run feature-b
orch run bugfix-c

# Monitor all runs
orch monitor
` + "```" + `

### Using different agents for different tasks
` + "```" + `bash
# Use Claude for documentation
orch run docs-update --agent claude

# Use Codex for complex algorithms
orch run algorithm-impl --agent codex

# Use OpenCode with max settings for architecture
orch run architecture-design --preset opus:max
` + "```" + `

## PR Review Flow

### 1. Wait for completion
` + "```" + `bash
# Check status
orch ps --status done

# Show details including PR link
orch show my-feature
` + "```" + `

### 2. Review the PR
` + "```" + `bash
# Open PR in browser
orch open my-feature --pr
` + "```" + `

### 3. Request changes if needed
` + "```" + `bash
# Continue the run with feedback
orch continue my-feature --message "Please address review comments"
` + "```" + `

### 4. Mark as resolved
` + "```" + `bash
orch resolve my-feature
` + "```" + `

## Capturing Sessions

### Capture for documentation
` + "```" + `bash
# Capture a completed run
orch capture my-feature --output feature-session.md

# Capture all completed runs
orch capture-all
` + "```" + `

## Working with Worktrees

### Use existing worktree
` + "```" + `bash
# Point to existing worktree
orch run my-feature --worktree /path/to/worktree
` + "```" + `

### Configure default worktree directory
In ` + "`.orch/config.yaml`" + `:
` + "```" + `yaml
worktree_dir: ~/.git-worktrees
` + "```" + `

## Querying Run History

### Find all runs for an issue
` + "```" + `bash
orch query "SELECT * FROM runs WHERE issue_id = 'my-feature'"
` + "```" + `

### Find failed runs
` + "```" + `bash
orch query "SELECT * FROM runs WHERE status = 'failed'"
` + "```" + `

### Get run statistics
` + "```" + `bash
orch query "SELECT status, count(*) FROM runs GROUP BY status"
` + "```" + `
`

const skillTroubleshootingContent = `# Orch Troubleshooting

## Common Issues

### Run stuck in "running" but agent not responding

**Symptoms:**
- ` + "`orch ps`" + ` shows "running" status
- ` + "`orch attach`" + ` shows no activity

**Solution:**
` + "```" + `bash
# Try repair first
orch repair

# If still stuck, stop and restart
orch stop my-issue
orch run my-issue
` + "```" + `

### Daemon not responding

**Symptoms:**
- Commands hang or timeout
- "daemon not running" errors

**Solution:**
` + "```" + `bash
# Check daemon status
orch daemon status

# Restart daemon
orch daemon restart

# If restart fails, kill and start fresh
pkill -f "orch daemon"
orch daemon start
` + "```" + `

### Orphaned tmux sessions

**Symptoms:**
- tmux sessions exist but orch doesn't know about them
- "session already exists" errors

**Solution:**
` + "```" + `bash
# Run repair to clean up
orch repair

# Or manually list and kill tmux sessions
tmux list-sessions
tmux kill-session -t orch-my-issue-20231220-100000
` + "```" + `

### Worktree conflicts

**Symptoms:**
- "worktree already exists" errors
- Can't start new runs

**Solution:**
` + "```" + `bash
# List git worktrees
git worktree list

# Remove stale worktrees
git worktree remove /path/to/worktree --force

# Then run repair
orch repair
` + "```" + `

### Run shows wrong status

**Symptoms:**
- Status doesn't match actual agent state
- Run shows "running" but agent exited

**Solution:**
` + "```" + `bash
# Repair will detect and fix state mismatches
orch repair

# Check debug info
orch debug my-issue
` + "```" + `

## Using orch repair

The ` + "`orch repair`" + ` command fixes common issues:

` + "```" + `bash
# Run repair
orch repair

# See what would be fixed without making changes
orch repair --dry-run
` + "```" + `

**What repair fixes:**
- Orphaned tmux sessions
- Stale run states
- Daemon connectivity issues
- Worktree inconsistencies

## Debug Commands

### View internal state
` + "```" + `bash
orch debug my-issue
` + "```" + `

### View run logs
` + "```" + `bash
orch log my-issue
` + "```" + `

### Query database directly
` + "```" + `bash
# See all data
orch query "SELECT * FROM runs"

# Find problematic runs
orch query "SELECT * FROM runs WHERE status = 'running' AND updated_at < datetime('now', '-1 hour')"
` + "```" + `

### Check database schema
` + "```" + `bash
orch schema
` + "```" + `

## Getting Help

### View tutorial
` + "```" + `bash
orch tutorial
` + "```" + `

### Command help
` + "```" + `bash
orch --help
orch run --help
orch ps --help
` + "```" + `

## Recovery Procedures

### Complete reset (last resort)

If nothing else works:

` + "```" + `bash
# Stop all runs
orch stop --all

# Kill daemon
orch daemon stop

# Kill any remaining tmux sessions
tmux kill-server

# Clear state (careful - loses run history)
rm -rf ~/.local/share/orch/runs/*

# Restart
orch daemon start
` + "```" + `

### Recovering a run's work

If a run failed but has useful changes:

` + "```" + `bash
# Find the worktree
orch show my-issue

# Navigate to worktree and commit changes manually
cd /path/to/worktree
git status
git add .
git commit -m "WIP: changes from failed run"
git push origin my-issue
` + "```" + `
`

const skillConfigHeader = `# Orch Configuration

## Configuration File

Orch uses ` + "`.orch/config.yaml`" + ` in your project root for configuration.

## Configuration Precedence

1. Command-line flags (highest)
2. Repo-local ` + "`.orch/config.yaml`" + `
3. Parent directory ` + "`.orch/config.yaml`" + ` files
4. Environment variables
5. Global ` + "`~/.config/orch/config.yaml`" + ` (lowest)

## Basic Configuration

` + "```" + `yaml
# Default agent (opencode, claude, codex, gemini)
agent: opencode

# Default model and variant for opencode
opencode:
  default_model: anthropic/claude-opus-4-5
  default_variant: max

# Where to store git worktrees
worktree_dir: ~/.git-worktrees

# Base branch for creating feature branches
base_branch: main

# Target branch for PRs
pr_target_branch: main

# Skip PR creation
no_pr: false

# Terminal multiplexer (tmux or zellij)
multiplexer: tmux
` + "```" + `

## Presets

Define reusable model configurations:

` + "```" + `yaml
presets:
  - name: opus:max
    backend: opencode
    model: anthropic/claude-opus-4-5
    variant: max

  - name: sonnet:high
    backend: opencode
    model: anthropic/claude-sonnet-4
    variant: high

  - name: claude-direct
    backend: claude
    profile: default

default_preset: opus:max
` + "```" + `

Use presets with:
` + "```" + `bash
orch run my-issue --preset opus:max
` + "```" + `

## Issues Configuration

` + "```" + `yaml
issues:
  # Backend: local (file-based) or github
  backend: local
  
  # Path to issues storage
  path: ~/my-project-issues
` + "```" + `

Or use environment variable:
` + "```" + `bash
export ORCH_ISSUES_ROOT=~/my-project-issues
` + "```" + `

## GitHub Backend

` + "```" + `yaml
issues:
  backend: github

github:
  owner: myorg
  repo: myrepo
  label_filter: orch  # Only sync issues with this label
  poll_interval: 300  # Seconds between syncs
` + "```" + `

## Monitor Configuration

` + "```" + `yaml
monitor:
  ps_columns:
    - index
    - id
    - issue
    - agent
    - status
    - branch
    - pr
    - updated
` + "```" + `

## Slack Notifications

` + "```" + `yaml
slack:
  enabled: true
  webhook_url: https://hooks.slack.com/services/xxx
  # Or use bot token:
  # bot_token: xoxb-xxx
  # channel: "#orch-notifications"
  notify_on:
    - blocked
    - done
    - failed
` + "```" + `

## Environment Variables

| Variable | Description |
|----------|-------------|
| ` + "`ORCH_ISSUES_ROOT`" + ` | Path to issues storage |
| ` + "`ORCH_PROJECT_ROOT`" + ` | Project root (where .orch/ lives) |
| ` + "`ORCH_AGENT`" + ` | Default agent |
| ` + "`ORCH_MODEL`" + ` | Default model |
| ` + "`ORCH_MODEL_VARIANT`" + ` | Default model variant |
| ` + "`ORCH_WORKTREE_DIR`" + ` | Worktree directory |
| ` + "`ORCH_BASE_BRANCH`" + ` | Base branch |
| ` + "`ORCH_PR_TARGET_BRANCH`" + ` | PR target branch |
| ` + "`ORCH_DEFAULT_PRESET`" + ` | Default preset |
| ` + "`ORCH_MULTIPLEXER`" + ` | Terminal multiplexer |
| ` + "`ORCH_LOG_LEVEL`" + ` | Log level (error/warn/info/debug) |
`

const skillConfigFooter = `

## Prompt Templates

Customize the prompt sent to agents:

` + "```" + `yaml
# Global prompt template
prompt_template: |
  Please read 'ORCH_PROMPT.md' in the current directory and follow the instructions found there.

# Agent-specific templates
opencode:
  prompt_template: |
    Custom prompt for opencode...

claude:
  prompt_template: |
    Custom prompt for claude...
` + "```" + `

## Agent-Specific Configuration

### OpenCode
` + "```" + `yaml
opencode:
  default_model: anthropic/claude-opus-4-5
  default_variant: max
  prompt_template: |
    Custom opencode prompt...
` + "```" + `

### Claude
` + "```" + `yaml
claude:
  prompt_template: |
    Custom claude prompt...
` + "```" + `

### Codex
` + "```" + `yaml
codex:
  prompt_template: |
    Custom codex prompt...
` + "```" + `

### Gemini
` + "```" + `yaml
gemini:
  prompt_template: |
    Custom gemini prompt...
` + "```" + `

## Control Agent

Configure the agent used for orch monitor's control features:

` + "```" + `yaml
control_agent: opencode
control_model: anthropic/claude-sonnet-4
control_model_variant: high
` + "```" + `
`
