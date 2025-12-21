---
type: issue
id: orch-007
title: Prompt agent to create PR at end of run
status: resolved
priority: high
---

# Prompt agent to create PR at end of run

When launching an agent via `orch run`, the prompt should instruct the agent to create a pull request upon completing the work.

## Current Behavior

Agent is launched with just the issue body as prompt.

## Desired Behavior

Append instructions to the prompt telling the agent to:
1. Complete the implementation
2. Run tests if applicable
3. Create a PR with a descriptive title and body
4. Include the issue ID in the PR description

## Prompt Template

```
<issue>
{issue_body}
</issue>

Instructions:
- Implement the changes described in the issue above
- Run tests to verify your changes work correctly
- When complete, create a pull request:
  - Title should summarize the change
  - Body should reference issue: {issue_id}
  - Include a summary of changes made
```

## Configuration

Allow customization via:
- `.orch/config.yaml` - default prompt template
- `--prompt-template <file>` flag
- `--no-pr` flag to skip PR instruction

## PR Event

When agent creates a PR, daemon should detect and record:
```
- <ts> | artifact | pr | url=https://github.com/...
- <ts> | status | pr_open
```
