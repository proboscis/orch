# Daily Workflow with orch

This guide covers typical day-to-day usage patterns for working with orch and AI agents.

## Morning Routine

Start your day by checking what happened overnight:

```bash
# Check status of all runs
orch ps

# See runs that completed or need attention
orch ps --status done,pr_open,blocked,failed
```

Review any PRs that agents created:

```bash
# List runs with open PRs
orch ps --status pr_open

# View the diff for a specific run
orch diff my-issue

# Open the PR in browser
gh pr view <pr-number> --web
```

## Starting New Work

### 1. Create an Issue

Issues define what you want the agent to accomplish:

```bash
# Create and edit an issue
orch issue create fix-login-timeout --title "Fix login session timeout" --edit
```

This opens your editor. Write a clear task description:

```markdown
---
type: issue
id: fix-login-timeout
title: Fix login session timeout
status: open
---

# Fix login session timeout

Users report being logged out after 5 minutes of inactivity.

## Current Behavior
- Session expires after 5 minutes
- No warning before logout

## Expected Behavior  
- Session should last 30 minutes
- Show warning 5 minutes before expiry

## Files to check
- src/auth/session.ts
- src/middleware/auth.ts
```

### 2. Start a Run

```bash
# Start an agent working on the issue
orch run fix-login-timeout

# With a specific agent
orch run --agent opencode fix-login-timeout

# With verbose output for debugging
orch run --verbose fix-login-timeout
```

The agent starts working in the background. You can continue with other tasks.

## Monitoring Progress

### Check Status Periodically

```bash
# Quick status check
orch ps

# Filter by status
orch ps --status running,blocked

# Detailed view of a specific run
orch show fix-login-timeout
```

### Status Meanings

| Status | What it means | Your action |
|--------|---------------|-------------|
| `running` | Agent is actively working | Wait, or watch with `attach` |
| `blocked` | Agent needs input or is stuck | Use `attach` or `send` to help |
| `pr_open` | Agent created a PR | Review the PR |
| `done` | Work completed successfully | Celebrate! |
| `failed` | Something went wrong | Check logs, possibly restart |

## Interacting with Agents

### When to Attach vs Send

| Situation | Use | Why |
|-----------|-----|-----|
| Need to have a conversation | `attach` | Interactive back-and-forth |
| Agent is stuck on something | `attach` | See what's happening, provide guidance |
| Quick instruction or clarification | `send` | Don't need to watch the response |
| Paste an image or long text | `attach` | Full terminal access |

### Using attach

```bash
# Connect to the agent's terminal
orch attach fix-login-timeout
```

Once attached, you can:
- Watch the agent work in real-time
- Type messages directly to the agent
- Paste images (if agent supports it)
- Provide files or code snippets

**Detach without stopping**: Press `Ctrl+B` then `D` (tmux) or `Ctrl+O` then `D` (zellij)

### Using send

```bash
# Send a quick instruction
orch send fix-login-timeout "Also add unit tests for the session module"

# Provide context
orch send fix-login-timeout "The config file is at config/auth.yaml"
```

### Using capture

```bash
# See what the agent is currently outputting
orch capture fix-login-timeout

# Capture from all running agents
orch capture-all
```

## Reviewing Agent Work

When status becomes `pr_open`:

### 1. Review the diff

```bash
# See what changed
orch diff fix-login-timeout

# Just the summary
orch diff --stat fix-login-timeout
```

### 2. Check the PR

```bash
# View PR details
gh pr view <pr-number>

# Open in browser
gh pr view <pr-number> --web
```

### 3. Run tests in the worktree

```bash
# Execute commands in the run's worktree
orch exec fix-login-timeout -- npm test
orch exec fix-login-timeout -- make lint
```

### 4. Request changes if needed

If the PR needs work, you can continue the run:

```bash
# Continue work with the same agent
orch continue fix-login-timeout

# Or attach and provide feedback
orch attach fix-login-timeout
# Type: "The tests are failing, please fix the edge case in session.ts"
```

## Parallel Work

Run multiple agents simultaneously on different issues:

```bash
# Start several runs
orch run fix-login-timeout
orch run add-dark-mode  
orch run refactor-api

# Monitor all at once
orch ps

# Each runs in its own isolated worktree
```

### Tips for parallel work

1. **Use descriptive issue IDs** - Easy to tell runs apart in `ps` output
2. **Check status regularly** - Some may finish quickly, others may block
3. **Prioritize blocked runs** - Agents waiting for input aren't making progress
4. **Review PRs as they come in** - Don't let them pile up

## End of Day

Before signing off:

```bash
# Check final status
orch ps

# Stop any runs that won't finish overnight (optional)
orch stop fix-login-timeout

# Or stop all runs
orch stop --all

# Clean up failed runs
orch delete my-failed-run
```

### Leaving runs overnight

It's often fine to leave agents running overnight:
- Long tasks may complete by morning
- Status will show what happened
- PRs will be ready for review

Just be mindful of API costs for long-running agents.

## Example Day

```bash
# === Morning ===

# Check overnight progress
orch ps

# Two runs finished with PRs
orch diff fix-bug-123
orch diff add-feature-x
gh pr view 42 --web
gh pr view 43 --web

# === Start new work ===

# Create today's tasks
orch issue create optimize-db-queries --title "Optimize slow database queries" --edit
orch issue create update-deps --title "Update npm dependencies" --edit

# Start the agents
orch run optimize-db-queries
orch run update-deps

# === Midday check ===

orch ps
# ISSUE                STATUS   RUN                 AGENT   UPDATED
# optimize-db-queries  running  20260115-093012     claude  5m ago
# update-deps          blocked  20260115-093045     claude  2m ago

# Help the blocked agent
orch attach update-deps
# Agent: "Should I update React to v19 or stay on v18?"
# You: "Stay on v18 for now, we're not ready for the migration"

# === Afternoon ===

orch ps
# ISSUE                STATUS   RUN                 AGENT   UPDATED
# optimize-db-queries  pr_open  20260115-093012     claude  1h ago
# update-deps          running  20260115-093045     claude  30m ago

# Review the finished PR
orch diff optimize-db-queries
gh pr view 45

# === End of day ===

orch ps
# Both done with PRs
# Review tomorrow, signing off
```

## Next Steps

- Use [orch-monitor](./orch-monitor.md) TUI for a visual dashboard
- Set up the [control agent](./reference/commands.md#orch-agent) for chat-based management
- Learn [SQL queries](./reference/query.md) for advanced run analysis
