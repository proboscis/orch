# File Backend

The file backend (default) stores issues as local markdown files. This is the simplest setup and works well for solo developers or teams that want full control over their issue storage.

## Overview

- Issues are markdown files with YAML frontmatter
- Runs are logged as markdown files in a `runs/` directory
- No external dependencies or services required
- Works with any text editor or IDE
- Compatible with Obsidian for a nice UI

## Configuration

### Basic setup

```yaml
# .orch/config.yaml
issues:
  backend: local
  path: ~/orch-issues
```

Or use environment variable:

```bash
export ORCH_ISSUES_ROOT=~/orch-issues
```

### In-repo issues

Keep issues alongside your code:

```yaml
issues:
  backend: local
  path: ./issues
```

## Directory Structure

```
issues-root/
├── issues/
│   ├── fix-login-bug.md
│   ├── add-dark-mode.md
│   └── refactor-api/
│       ├── phase-1.md
│       └── phase-2.md
└── runs/
    ├── fix-login-bug/
    │   ├── 20260120-163045.md
    │   └── 20260120-171230.md
    └── add-dark-mode/
        └── 20260120-180000.md
```

## Issue File Format

Issues can be placed anywhere in the issues root. They're detected by the `type: issue` frontmatter.

### Required fields

```yaml
---
type: issue
---
```

### Recommended fields

```yaml
---
type: issue
id: fix-login-bug        # Explicit ID (default: filename)
title: Fix login timeout # Title (default: first heading)
status: open             # open, in_progress, closed, etc.
---
```

### Full example

```yaml
---
type: issue
id: fix-login-bug
title: Fix login timeout issue
status: open
priority: high
assignee: claude
tags:
  - bug
  - auth
created: 2026-01-20
---

# Fix login timeout issue

Users report being logged out after 5 minutes of inactivity.

## Problem

The session token expires too quickly. Current TTL is 300 seconds.

## Expected Behavior

Sessions should last 30 minutes (1800 seconds) of inactivity.

## Acceptance Criteria

- [ ] Update session TTL to 1800 seconds
- [ ] Add configuration option for session timeout
- [ ] Add tests for session expiry
- [ ] Update documentation

## Technical Notes

The session handling is in `src/auth/session.ts`.
```

## Run Files

Run files are created automatically by `orch run` and contain:

### Frontmatter

```yaml
---
issue_id: fix-login-bug
run_id: 20260120-163045
agent: claude
status: running
worktree: /Users/me/.orch/worktrees/fix-login-bug/abc123_claude_20260120-163045
branch: issue/fix-login-bug/run-20260120-163045
tmux_session: run-fix-login-bug-20260120-163045
created: 2026-01-20T16:30:45+09:00
---
```

### Event log (body)

```markdown
# Run: fix-login-bug#20260120-163045

## Events

- 2026-01-20T16:30:45+09:00 | status | queued |
- 2026-01-20T16:30:46+09:00 | status | booting | agent=claude
- 2026-01-20T16:30:50+09:00 | artifact | worktree | path=/Users/me/.orch/worktrees/...
- 2026-01-20T16:30:51+09:00 | artifact | branch | name=issue/fix-login-bug/run-20260120-163045
- 2026-01-20T16:30:52+09:00 | status | running |
- 2026-01-20T16:45:30+09:00 | artifact | pr | url=https://github.com/org/repo/pull/42
- 2026-01-20T16:45:31+09:00 | status | pr_open |
```

## Creating Issues

### Using the CLI

```bash
# Create with editor
orch issue create my-issue --title "My task" --edit

# Create from template
orch issue create my-issue --title "My task" --body "$(cat template.md)"
```

### Manual creation

Create a file at `issues-root/issues/my-issue.md`:

```yaml
---
type: issue
id: my-issue
title: My task
status: open
---

# My task

Description of what needs to be done.
```

## Listing Issues

```bash
# All issues
orch issue list

# Filtered by status
orch issue list --status open

# With run information
orch issue list --with-runs
```

## Best Practices

### File organization

- Use descriptive filenames: `fix-login-timeout.md` not `issue-42.md`
- Group related issues in subdirectories: `refactor-api/phase-1.md`
- Keep one issue per file

### Status workflow

```
open → in_progress → closed
```

Use issue status for your workflow state, and run status for execution state.

### Using with Obsidian

The file backend is fully compatible with [Obsidian](https://obsidian.md/):

1. Open your issues root as an Obsidian vault
2. Use Obsidian's editor for issue creation
3. Use Dataview plugin to query issues and runs
4. Benefit from backlinks between related issues

Example Dataview query:

```dataview
TABLE status, title
FROM "issues"
WHERE type = "issue" AND status = "open"
SORT priority DESC
```

### Gitignoring runs

If you keep issues in your repo but don't want to commit run logs:

```gitignore
# .gitignore
issues/runs/
```

## Migration from GitHub/Linear

If you're moving from an external backend to files:

```bash
# Export issues (hypothetical command)
orch issue export --backend github --output ./issues/

# Or manually create issue files for each ticket
```

## Troubleshooting

### Issue not detected

Check that your file has:
1. `.md` extension
2. `type: issue` in frontmatter
3. Valid YAML frontmatter (no tabs, proper indentation)

### Can't find runs

Runs are stored in `runs/<issue-id>/` within the issues root:

```bash
ls -la $(orch --print-issues-root)/runs/
```

### Permission errors

Ensure orch can write to the issues root:

```bash
chmod -R u+w ~/orch-issues
```
