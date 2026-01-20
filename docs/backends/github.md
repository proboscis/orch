# GitHub Backend

The GitHub backend allows orch to use GitHub Issues as the source of tasks for agents. This is ideal for teams already using GitHub for issue tracking.

## Overview

- Issues come from GitHub Issues
- No local issue files needed
- Integrates with your existing workflow
- Agents can reference and update GitHub issues
- Pull requests automatically link to issues

## Configuration

### Basic setup

```yaml
# .orch/config.yaml
issues:
  backend: github

github:
  owner: your-org
  repo: your-repo
  poll_interval: 300  # Check for updates every 5 minutes
```

### Authentication

The GitHub backend uses the `gh` CLI for authentication. Ensure you're logged in:

```bash
# Login to GitHub
gh auth login

# Verify authentication
gh auth status
```

Alternatively, set a personal access token:

```bash
export GITHUB_TOKEN=ghp_your_token_here
```

Required token permissions:
- `repo` - Full repository access
- `read:org` - Read organization data (if using org repos)

## Usage

### Starting a run from GitHub Issue

```bash
# Use the issue number
orch run 42

# Or the full issue reference
orch run my-org/my-repo#42
```

### Listing issues

```bash
# List open issues
orch issue list

# Filter by label
orch issue list --label bug

# Filter by assignee
orch issue list --assignee @me
```

### How it works

1. `orch run 42` fetches issue #42 from GitHub
2. Creates a worktree and branch as usual
3. Agent works on the issue
4. When agent creates a PR, it references the issue
5. Run status updates can optionally be posted as comments

## Issue Labels

You can use labels to organize and filter issues:

```bash
# Run issues with specific label
orch run --label orch-ready 42
```

Configure auto-labels in config:

```yaml
github:
  owner: my-org
  repo: my-repo
  labels:
    ready: "orch:ready"      # Issues ready for agents
    in_progress: "orch:wip"  # Issue has active run
    done: "orch:done"        # Completed by agent
```

## Pull Request Integration

When an agent creates a PR:

- PR description references the issue: `Closes #42`
- Run status links to the PR
- Issue can be auto-closed when PR merges

Configure PR behavior:

```yaml
github:
  owner: my-org
  repo: my-repo
  pr:
    auto_link: true      # Link PR to issue
    auto_close: true     # Close issue on PR merge
    target_branch: main  # Default PR target
```

## Status Updates

Optionally post run status as issue comments:

```yaml
github:
  owner: my-org
  repo: my-repo
  comments:
    enabled: true
    on_start: true      # Comment when run starts
    on_blocked: true    # Comment when input needed
    on_complete: true   # Comment with results
```

Example comment:

```markdown
🤖 **orch run started**

- Run ID: `20260120-163045`
- Agent: claude
- Branch: `issue/42/run-20260120-163045`

Status: running
```

## Filtering Issues

### By labels

```yaml
github:
  owner: my-org
  repo: my-repo
  filter:
    labels:
      - bug
      - enhancement
    exclude_labels:
      - wontfix
      - duplicate
```

### By milestone

```yaml
github:
  filter:
    milestone: "v2.0"
```

### By assignee

```yaml
github:
  filter:
    assignee: "@me"  # Issues assigned to authenticated user
```

## Run Storage

Even with GitHub backend, run logs are stored locally:

```
.orch/
├── config.yaml
├── daemon.log
└── runs/
    └── 42/
        └── 20260120-163045.md
```

This ensures:
- Fast access to run history
- Offline access to logs
- No GitHub API limits for log access

## Hybrid Mode

Use GitHub for issues but local files for additional context:

```yaml
issues:
  backend: github
  local_context: ./context/  # Additional local markdown files

github:
  owner: my-org
  repo: my-repo
```

Local context files can be referenced in issue prompts but aren't synced to GitHub.

## Best Practices

### Issue templates

Create GitHub issue templates that work well with agents:

```markdown
<!-- .github/ISSUE_TEMPLATE/agent-task.md -->
---
name: Agent Task
about: Task for orch agents
labels: orch:ready
---

## Summary
<!-- Brief description -->

## Requirements
<!-- Specific requirements -->

## Acceptance Criteria
- [ ] Criterion 1
- [ ] Criterion 2

## Technical Notes
<!-- Implementation hints for the agent -->
```

### Branch protection

Configure branch protection to require:
- PR reviews before merge
- Status checks passing
- Up-to-date branches

This ensures agent PRs go through proper review.

### Notifications

Combine with Slack notifications:

```yaml
github:
  owner: my-org
  repo: my-repo

slack:
  enabled: true
  webhook_url: ${SLACK_WEBHOOK}
  notify_on:
    - blocked
```

## Limitations

- Requires network access to GitHub
- Subject to GitHub API rate limits
- Issue content limited to GitHub's format
- No offline issue creation

## Troubleshooting

### Authentication errors

```bash
# Re-authenticate
gh auth login

# Check current status
gh auth status
```

### Rate limiting

If you hit rate limits:

```yaml
github:
  poll_interval: 600  # Reduce polling frequency
```

### Issue not found

Ensure:
1. Issue number is correct
2. You have access to the repository
3. Issue is not a pull request (PRs have different IDs)

```bash
# Verify issue exists
gh issue view 42 --repo my-org/my-repo
```
