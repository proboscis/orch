# Linear Backend

The Linear backend allows orch to use Linear issues as the source of tasks for agents. This is ideal for teams using Linear for project management.

## Overview

- Issues come from Linear
- Integrates with Linear's workflow states
- Supports Linear's rich issue format
- Agents can update issue status automatically
- Links PRs to Linear issues

> **Note:** The Linear backend is currently in development. Some features described here may not be fully implemented yet.

## Configuration

### Basic setup

```yaml
# .orch/config.yaml
issues:
  backend: linear

linear:
  team_id: YOUR_TEAM_ID
  api_key: ${LINEAR_API_KEY}
```

### Authentication

Set your Linear API key:

```bash
export LINEAR_API_KEY=lin_api_xxxxxxxxxxxxx
```

To get an API key:
1. Go to Linear Settings → API
2. Create a new Personal API Key
3. Copy the key

## Usage

### Starting a run from Linear issue

```bash
# Use the issue identifier (e.g., ENG-123)
orch run ENG-123

# Or use the issue UUID
orch run 550e8400-e29b-41d4-a716-446655440000
```

### Listing issues

```bash
# List issues from your team
orch issue list

# Filter by state
orch issue list --status "In Progress"

# Filter by label
orch issue list --label bug
```

## Linear Workflow Integration

Map Linear states to orch statuses:

```yaml
linear:
  team_id: YOUR_TEAM_ID
  states:
    # Linear state → orch behavior
    "Backlog": ignore       # Don't show in orch issue list
    "Todo": ready           # Ready for agents
    "In Progress": running  # Has active run
    "In Review": pr_open    # PR created
    "Done": done            # Completed
```

### Auto-update Linear states

```yaml
linear:
  team_id: YOUR_TEAM_ID
  auto_update_state: true
  state_transitions:
    on_run_start: "In Progress"
    on_pr_created: "In Review"
    on_done: "Done"
```

## Issue Format

Linear issues are converted to a standard format for agents:

```markdown
# ENG-123: Fix authentication bug

## Description
Users can't log in after password reset.

## Labels
- bug
- auth

## Priority
High

## Acceptance Criteria
(From Linear sub-issues or checklist)
- [ ] Fix password reset flow
- [ ] Add tests
```

## Project and Cycle Support

Filter issues by project or cycle:

```yaml
linear:
  team_id: YOUR_TEAM_ID
  filter:
    project: "Q1 Goals"
    cycle: current  # "current", "next", or specific cycle ID
```

## Comments and Updates

Post updates to Linear issues:

```yaml
linear:
  team_id: YOUR_TEAM_ID
  comments:
    enabled: true
    on_start: true
    on_waiting: true
    on_complete: true
    include_run_link: true
```

Example comment:

```
🤖 orch run started

Run ID: 20260120-163045
Agent: claude
Branch: issue/ENG-123/run-20260120-163045

View run: [link to local run file]
```

## Attachments

Linear attachments are downloaded and included in the agent context:

```yaml
linear:
  team_id: YOUR_TEAM_ID
  attachments:
    download: true
    max_size_mb: 10
    include_images: true
```

## Sub-issues

Handle Linear sub-issues:

```yaml
linear:
  team_id: YOUR_TEAM_ID
  sub_issues:
    include_in_prompt: true  # Add sub-issues to agent prompt
    mark_complete: true      # Mark sub-issues done when checked
```

## Labels for Filtering

Use Linear labels to control which issues agents can work on:

```yaml
linear:
  team_id: YOUR_TEAM_ID
  filter:
    labels:
      include:
        - "agent-ready"
      exclude:
        - "manual-only"
        - "blocked"
```

## Run Storage

Run logs are stored locally (same as file backend):

```
.orch/
├── config.yaml
├── daemon.log
└── runs/
    └── ENG-123/
        └── 20260120-163045.md
```

## Best Practices

### Issue templates

Create Linear issue templates with agent-friendly structure:

- Clear description
- Explicit acceptance criteria
- Technical context in description
- Appropriate labels

### Workflow states

Configure your Linear workflow to include agent-specific states:

```
Backlog → Ready for Agent → In Progress → In Review → Done
```

### Label conventions

Use consistent labels:
- `agent-ready` - Issue is ready for agents
- `agent-blocked` - Agent needs help
- `agent-complete` - Agent finished work

## Limitations

- Requires network access to Linear
- Subject to Linear API rate limits
- Some Linear features (relations, SLA) not fully supported
- No offline access to issues

## Troubleshooting

### Authentication errors

```bash
# Verify API key is set
echo $LINEAR_API_KEY

# Test API access
curl -H "Authorization: $LINEAR_API_KEY" \
  https://api.linear.app/graphql \
  -d '{"query": "{ viewer { id } }"}'
```

### Team not found

Ensure the team ID is correct:

```bash
# List your teams
curl -H "Authorization: $LINEAR_API_KEY" \
  https://api.linear.app/graphql \
  -d '{"query": "{ teams { nodes { id name } } }"}'
```

### Issue not found

Check:
1. Issue identifier is correct (e.g., `ENG-123` not `123`)
2. Issue belongs to configured team
3. Issue is not archived
