# Daily Workflow with orch

This guide covers recurring work after orch is installed and your first run
works. For the canonical create → run → observe → interact loop, start with
[Getting Started](./getting-started.md) or the validated Japanese
[local quickstart](./local-quickstart.ja.md).

## Morning Routine

Start your day by checking what happened overnight:

```bash
# Workers auto-start on demand (ADR-0002); check state any time with
orch worker status

# Check status of all runs
orch ps

# See runs that completed or need attention
orch ps --status done,pr_open,waiting,failed
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

Create and edit a focused issue, then start its run:

```bash
orch issue create fix-login-timeout --title "Fix login session timeout" --edit
orch run fix-login-timeout

# Alternatives: select an agent or enable verbose diagnostics
# (run only the variant you need)
orch run --agent opencode fix-login-timeout
orch run --verbose fix-login-timeout
```

The agent works in an isolated worktree in the background. Put current
behavior, expected behavior, relevant files, and verification requirements in
the issue body; the first-run guides cover heredoc creation, local `--no-pr`
runs, and trust prompts in detail.

## Monitoring Progress

### Check Status Periodically

```bash
# Quick status check
orch ps

# Filter by status
orch ps --status running,waiting

# Detailed view of a specific run
orch show fix-login-timeout
```

For the complete status vocabulary and transition rules, see
[Statuses](./reference/statuses.md). A `waiting` run may need input or may have
finished its turn; use `orch capture` to distinguish the two.

## Interacting with Agents

### When to Attach vs Send

| Situation | Use | Why |
|-----------|-----|-----|
| Need to inspect a `waiting` run | `capture` | Read the current terminal without taking it over |
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

# Send multi-line feedback
orch send fix-login-timeout <<'EOF'
Please fix the failing login path first.
Then rerun the auth-focused tests.
EOF
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

If the PR needs work and the run is still waiting/alive, send feedback first:

```bash
orch send fix-login-timeout "Please fix the edge case in session.ts"
```

If a run actually failed/canceled/unknown, restart it:

```bash
# Restart from the last failed run with the same agent
orch restart-from fix-login-timeout#20260120-163045

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
3. **Prioritize waiting runs** - Agents waiting for input aren't making progress
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

# Clean up failed worktrees without deleting run history
orch clean my-failed-run
```

### Leaving runs overnight

It's often fine to leave agents running overnight:
- Long tasks may complete by morning
- Status will show what happened
- PRs will be ready for review

Just be mindful of API costs for long-running agents.

## Next Steps

- Use [orch-monitor](./orch-monitor.md) TUI for a visual dashboard
- Set up the [control agent](./reference/commands.md#orch-agent) for chat-based management
- Learn [SQL queries](./reference/query.md) for advanced run analysis
