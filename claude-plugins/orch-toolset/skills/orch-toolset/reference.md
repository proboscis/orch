# Orch Command Reference

## Run References (RUN_REF)

Commands that accept a RUN_REF understand multiple formats:

- `ISSUE_ID#RUN_ID` - Target specific run (e.g., `orch-055#20251223-165605`)
- `ISSUE_ID` - Target latest run for that issue (e.g., `orch-055`)
- `SHORT_ID` - First 6 hex chars of run ID (e.g., `a3b4c5`)

## Global Flags

All commands support these flags:

| Flag | Description |
|------|-------------|
| `--vault PATH` | Vault path (or env `ORCH_VAULT`) |
| `--backend file\|github\|linear` | Backend selection (file is default) |
| `--json` | Machine-readable JSON output |
| `--tsv` | TSV output (useful for fzf) |
| `--quiet` | Suppress human output |
| `--log-level` | error\|warn\|info\|debug |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Issue not found |
| 3 | Worktree error |
| 4 | Tmux error |
| 5 | Agent launch error |
| 6 | Run not found |
| 7 | Question not found |
| 10 | Internal error |

---

## Issue Management

### orch issue create ISSUE_ID

Create a new issue document.

```bash
# Basic usage
orch issue create orch-055 --title "Create Claude Code plugin"

# With full options
orch issue create orch-055 \
  --title "Create Claude Code plugin" \
  --summary "Claude Code skill plugin for orch toolset" \
  --body "Detailed description here..."

# Open editor after creation
orch issue create orch-055 --edit
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--title` | Issue title |
| `--summary` | One-line summary |
| `--body` | Detailed description |
| `--edit` | Open $EDITOR after creation |

**Creates:** `issues/<ISSUE_ID>.md` with frontmatter:
```yaml
---
type: issue
id: orch-055
title: Create Claude Code plugin
status: open
summary: ...
---
```

### orch issue list

List all issues in vault.

```bash
# All issues
orch issue list

# Filter by status
orch issue list --status open

# With run info
orch issue list --with-runs

# JSON output
orch issue list --json
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--status` | Filter by status (open/closed/resolved) |
| `--with-runs` | Show active runs per issue |

### orch open ISSUE_ID|RUN_REF

Open issue or run document in editor.

```bash
# Open issue
orch open orch-055

# Open specific run
orch open orch-055#20251223-165605

# Use short ID
orch open a3b4c5

# Open in Obsidian
orch open orch-055 --app obsidian

# Just print path (for scripting)
orch open orch-055 --print-path
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--app` | obsidian\|editor\|default |
| `--print-path` | Print path without opening |

---

## Run Management

### orch run ISSUE_ID

Start a new run - creates worktree, launches agent in tmux, returns immediately.

```bash
# Basic usage (uses default agent: claude)
orch run orch-055

# Specify agent
orch run orch-055 --agent codex
orch run orch-055 --agent gemini
orch run orch-055 --agent custom --agent-cmd "my-agent --flag"

# Custom branch
orch run orch-055 --branch "feature/my-branch"

# Dry run (show what would happen)
orch run orch-055 --dry-run

# Skip PR creation instructions
orch run orch-055 --no-pr

# Specify profile
orch run orch-055 --agent claude --profile my-profile
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--agent` | claude\|codex\|gemini\|custom |
| `--agent-cmd` | Command for custom agent |
| `--profile` | Agent profile to use |
| `--run-id` | Manual run ID (default: YYYYMMDD-HHMMSS) |
| `--base-branch` | Base branch (default: main) |
| `--branch` | Custom branch name |
| `--worktree-dir` | Worktree location (default: ~/.orch/worktrees) |
| `--repo-root` | Explicit git root |
| `--tmux / --no-tmux` | Enable/disable tmux (default: tmux) |
| `--tmux-session` | Custom session name |
| `--dry-run` | Show plan without executing |
| `--no-pr` | Skip PR creation instructions |

**Default Conventions:**
- RUN_ID: `YYYYMMDD-HHMMSS`
- Branch: `issue/<ISSUE_ID>/run-<RUN_ID>`
- Worktree: `<worktree_dir>/<ISSUE_ID>/<SHORT_ID>_<AGENT>_<RUN_ID>`
- Tmux session: `run-<ISSUE_ID>-<RUN_ID>`

### orch continue RUN_REF|ISSUE_ID

Resume from existing worktree/branch with a new run.

```bash
# Continue latest run for issue
orch continue orch-055

# Continue specific run
orch continue orch-055#20251223-165605

# Continue with different agent
orch continue orch-055 --agent codex

# Continue from specific branch
orch continue --branch "issue/orch-055/run-20251223" --issue orch-055
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--agent` | Agent (default: previous run's agent) |
| `--agent-cmd` | Custom agent command |
| `--profile` | Agent profile |
| `--branch` | Specific branch to continue from |
| `--issue` | Issue ID when using --branch |
| `--tmux / --no-tmux` | Enable/disable tmux |
| `--no-pr` | Skip PR instructions |

**Behavior:**
- Fails if target run is still active (use `orch stop` first)
- Reuses existing worktree and branch
- Creates new run doc with `continued_from` reference

### orch ps

List runs with status information.

```bash
# All recent runs
orch ps

# Filter by status
orch ps --status running
orch ps --status running,blocked
orch ps --status pr_open,done

# Filter by issue
orch ps --issue orch-055

# Show all including resolved
orch ps --all

# Sort options
orch ps --sort updated    # default
orch ps --sort started

# Machine-readable
orch ps --json
orch ps --tsv  # for fzf
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--status` | Filter: queued,booting,running,blocked,blocked_api,pr_open,done,resolved,failed,canceled,unknown |
| `--issue-status` | Filter by issue status (open/closed) |
| `--issue` | Filter by issue ID |
| `--limit N` | Max runs (default: 50) |
| `--sort` | updated\|started |
| `--since` | Only runs after timestamp |
| `--absolute-time` | Show absolute timestamps |
| `--all` | Include resolved runs |

**TSV Columns:**
```
issue_id, issue_status, run_id, short_id, agent, status, updated_at, pr_url, branch, worktree_path, tmux_session
```

### orch show RUN_REF

Inspect a run's details, events, and artifacts.

```bash
# Show run details
orch show orch-055#20251223-165605

# Use short ID
orch show a3b4c5

# Custom tail length
orch show orch-055 --tail 100

# Only events
orch show orch-055 --events-only

# Show pending questions
orch show orch-055 --questions
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--tail N` | Event tail length (default: 80) |
| `--events-only` | Only show events |
| `--questions` | Show unanswered questions |

### orch stop ISSUE_ID|RUN_REF|--all

Stop running agents and mark as canceled.

```bash
# Stop all runs for an issue
orch stop orch-055

# Stop specific run
orch stop orch-055#20251223-165605

# Stop by short ID
orch stop a3b4c5

# Stop all active runs
orch stop --all

# Force stop (even if session missing)
orch stop orch-055 --force
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--all` | Stop all active runs |
| `--force` | Force cancel even if session missing |

**Behavior:**
- `ISSUE_ID` alone stops ALL active runs for that issue
- Kills tmux session if exists
- Appends `status=canceled` event

### orch resolve ISSUE_ID

Mark an issue as resolved.

```bash
# Resolve issue
orch resolve orch-055

# Force resolve (without completed runs)
orch resolve orch-055 --force
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--force` | Allow resolving without completed runs |

**Note:** This marks the *issue* status, not individual run status.

---

## Monitoring

### orch monitor

Interactive TUI dashboard for managing all runs.

```bash
# Start monitor
orch monitor

# Filter to specific issue
orch monitor --issue orch-055

# Filter by status
orch monitor --status running,blocked

# Start and immediately attach
orch monitor --attach

# Start new run from monitor
orch monitor --new

# Specify agent for new runs
orch monitor --agent codex
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--issue` | Filter to specific issue |
| `--status` | Filter by run status |
| `--attach` | Immediately attach to run |
| `--new` | Start a new run |
| `--agent` | Agent for new runs |

**Keyboard Shortcuts:**
| Key | Action |
|-----|--------|
| `1-9` | Attach to run by index |
| `s` | Stop mode |
| `n` | New run |
| `r` | Refresh |
| `f` | Filter (fzf) |
| `q` | Quit |
| `R` | Resolve issue |
| `c` | Open control agent |
| `Ctrl-b 0` | Return to dashboard |

### orch attach RUN_REF

Attach to agent's tmux session for direct interaction.

```bash
# Attach to run
orch attach orch-055#20251223-165605

# Use short ID
orch attach a3b4c5

# Attach to specific window
orch attach orch-055 --window agent
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--pane` | log\|shell (reserved) |
| `--window` | Specific window name |

**Behavior:**
- Attaches to existing session
- If session missing but worktree exists, creates new session
- Useful for image paste, complex interactions

### orch capture RUN_REF

Capture agent output without attaching.

```bash
# Basic capture (100 lines)
orch capture orch-055

# More lines
orch capture orch-055 --lines 200

# JSON output
orch capture orch-055 --json

# Use short ID
orch capture a3b4c5
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--lines N` | Number of lines (default: 100) |
| `--json` | JSON output format |

**Use cases:**
- Monitor progress without interrupting
- Feed output to other commands/scripts
- Check for errors or completion

---

## Agent Communication

### orch send RUN_REF MESSAGE

Send text to a running agent via tmux.

```bash
# Send guidance
orch send orch-055 "Please focus on fixing the tests first"

# Send without pressing Enter
orch send orch-055 "partial input" --no-enter

# Use short ID
orch send a3b4c5 "Continue with the implementation"
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--no-enter` | Don't send Enter key after message |

**Best Practice:** Combine with `orch capture` for programmatic coordination:
```bash
# Check what agent is doing
orch capture orch-055

# Send guidance based on output
orch send orch-055 "The test failure is in auth.go line 45"

# Check response
orch capture orch-055
```

### orch exec RUN_REF -- COMMAND [ARGS]

Execute command in run's worktree environment.

```bash
# Run tests
orch exec orch-055 -- pytest tests/

# Run linter
orch exec orch-055 -- go vet ./...

# Git commands
orch exec orch-055 -- git status

# Custom environment
orch exec orch-055 --env DEBUG=1 -- ./script.sh

# Shell mode
orch exec orch-055 --shell -- "ls -la && pwd"

# Quiet mode (no extra output)
orch exec orch-055 --quiet -- pytest
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--env KEY=VALUE` | Set environment variable (repeatable) |
| `--no-orch-env` | Don't set ORCH_* variables |
| `--shell` | Run through shell |
| `--quiet` | Suppress orch output |

**Environment Variables Set:**
- `ORCH_ISSUE_ID` - Current issue ID
- `ORCH_RUN_ID` - Current run ID
- `ORCH_RUN_PATH` - Path to run document
- `ORCH_WORKTREE_PATH` - Worktree directory
- `ORCH_BRANCH` - Git branch name
- `ORCH_VAULT` - Vault path

---

## Maintenance

### orch repair

Fix system state corruption.

```bash
# Check problems without fixing
orch repair --dry-run

# Repair interactively
orch repair

# Force repair without confirmation
orch repair --force
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--dry-run` | Report problems only |
| `--force` | No confirmation prompts |

**Fixes:**
- Restarts stuck daemon
- Marks "running" runs with missing sessions as failed
- Detects orphaned worktrees/sessions (warns)
- Corrects inconsistent state

---

## Examples by Use Case

### Starting Fresh Work
```bash
orch issue create orch-100 --title "Add feature X" --body "Details..."
orch run orch-100
orch ps --status running
```

### Monitoring Active Runs
```bash
orch ps --status running,blocked
orch capture a3b4c5 --lines 200
orch monitor
```

### Guiding a Stuck Agent
```bash
orch capture orch-055
# Review output, then send guidance
orch send orch-055 "Focus on the auth module first"
```

### Running Tests in Isolation
```bash
orch exec orch-055 -- pytest tests/ -v
orch exec orch-055 -- go test ./...
```

### Cleaning Up
```bash
orch stop orch-055  # Stop all runs for issue
orch resolve orch-055  # Mark issue resolved
orch repair --dry-run  # Check for problems
```

### Control Agent Loop
```bash
orch issue list --status open
orch ps --status running,blocked
orch capture <blocked-run>
orch send <blocked-run> "guidance"
orch resolve <completed-issue>
```
