---
type: issue
id: orch-061
title: Detect Claude Code API rate limit in agent output
status: open
---

# Detect Claude Code API rate limit in agent output

## Goal

The system needs to detect when a running Claude Code agent is blocked by API rate limits. This state is indicated by specific text in the terminal output.

## Detection Pattern

We need to detect text similar to the following in the agent's output (tmux capture):

```text
  ⎿  You've hit your limit · resets 7pm (Asia/Tokyo)
     Opening your options…

> /rate-limit-options
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ What do you want to do?                                                                                                                                                                                                                                                                                                                                                                                                                  │
│                                                                                                                                                                                                                                                                                                                                                                                                                                          │
│ ❯ 1. Stop and wait for limit to reset                                                                                                                                                                                                                                                                                                                                                                                                    │
│   2. Request more                                                                                                                                                                                                                                                                                                                                                                                                                        │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

Key indicators:
- "You've hit your limit"
- "/rate-limit-options"
- "Stop and wait for limit to reset"

## Implementation

- Scan the captured output of the agent (likely from tmux).
- If this pattern is detected, the run status should probably be updated to indicate it is blocked or waiting (e.g., `blocked`, `rate_limited`).
- This allows the monitor dashboard to reflect the correct state.

## Relevant Locations

- `internal/agent/`: Agent status logic.
- `internal/tmux/`: Output capturing.
