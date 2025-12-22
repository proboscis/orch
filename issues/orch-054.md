---
type: issue
id: orch-054
title: Add command to capture all agents' output with state for LLM consumption
status: open
---

# Add command to capture all agents' output with state for LLM consumption

## Problem

Currently, we have a command to check an individual agent's capture. However, there's no easy way to get a consolidated view of all running agents' captures along with their states.

## Proposed Solution

Add a new command (e.g., `orch capture-all` or `orch status-all`) that:

1. Iterates through all running agents
2. Captures each agent's current terminal output
3. Returns the output agent-by-agent with their state (running, blocked, idle, etc.)

## Use Case

This command would be particularly useful for an LLM control agent to:
- Quickly understand what all agents are doing
- Identify which agents need attention or are blocked
- Make informed decisions about which agents to interact with
- Provide a summary of overall progress across multiple runs

## Expected Output Format

```
=== orch-052#3220fe [running] ===
<captured terminal output>

=== orch-053#20251222-170402 [idle] ===
<captured terminal output>

...
```

## Benefits

- Single command to get full visibility into all agent activity
- Machine-readable output suitable for LLM processing
- Enables smarter orchestration decisions by control agents
