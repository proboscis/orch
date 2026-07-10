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

### Full reference

The `github` section supports exactly these keys:

```yaml
github:
  owner: your-org        # GitHub repository owner (required)
  repo: your-repo        # GitHub repository name (required)
  label_filter: orch     # Only sync issues carrying this label (optional)
  poll_interval: 300     # Seconds between GitHub polls (default: 300)
  status_labels:         # Map GitHub labels to orch issue statuses (optional)
    "status:resolved": resolved
```

See [Label Filtering](#label-filtering) and [Status Labels](#status-labels)
below for what `label_filter` and `status_labels` do.

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

Issues synced from GitHub get orch IDs of the form `gh-<number>`:

```bash
# Run GitHub issue #42
orch run gh-42
```

### Listing issues

```bash
# List synced issues
orch issue list

# Filter by status
orch issue list --status open

# Force a refresh from GitHub
orch issue sync
```

### How it works

1. The daemon polls GitHub every `poll_interval` seconds (default: 300) and caches issues locally
2. `orch run gh-42` resolves issue #42 from the local cache
3. Creates a worktree and branch as usual
4. Agent works on the issue
5. When the agent creates a PR, it can reference the issue (e.g. `Closes #42`)

## Label Filtering

`label_filter` restricts which GitHub issues orch sees. It is a single label
name:

```yaml
github:
  owner: my-org
  repo: my-repo
  label_filter: orch   # Only sync issues carrying the "orch" label
```

When set:

- Syncing (the daemon's polling loop, `orch issue sync`) only fetches issues
  that carry this label
- `orch issue create` automatically adds this label to new GitHub issues, so
  issues created through orch stay visible to orch

## Status Labels

`status_labels` maps GitHub label names to orch issue statuses (`open`,
`resolved`, `closed`). During sync, an issue carrying a mapped label takes
the mapped status instead of the plain GitHub open/closed state:

```yaml
github:
  owner: my-org
  repo: my-repo
  status_labels:
    "status:resolved": resolved
    "status:archived": closed
```

With this config, a GitHub issue labeled `status:resolved` shows up as
`resolved` in `orch issue list` even while it is still open on GitHub.
Issues with no mapped label fall back to `open` (GitHub open) or `closed`
(GitHub closed).

## Pull Request Integration

When an agent creates a PR whose description references the issue (e.g.
`Closes #42`), GitHub automatically closes the issue when the PR merges.
The PR itself is tracked on the run — `orch ps` shows it in the PR column.

## Run Storage

Even with GitHub backend, run records are stored locally in the issues root
(`issues.path`, or `~/.local/share/orch/<owner>-<repo>` by default):

```
<issues root>/
└── runs/
    └── gh-42/
        └── 20260120-163045.md
```

This ensures:
- Fast access to run history
- Offline access to logs
- No GitHub API limits for log access

## Best Practices

### Issue templates

Create GitHub issue templates that work well with agents:

```markdown
<!-- .github/ISSUE_TEMPLATE/agent-task.md -->
---
name: Agent Task
about: Task for orch agents
labels: orch  # Match your label_filter so orch picks the issue up
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
  notify_on:
    - waiting
```

Set `ORCH_SLACK_WEBHOOK_URL` in the orch process environment; YAML
`${...}` interpolation is not supported:

```bash
export ORCH_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/XXX/YYY/ZZZ
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
