---
type: issue
id: orch-016
title: Detect API usage limit block state
summary: Add detection for API usage limit block and specific status
status: resolved
priority: medium
---

# Detect API usage limit block state

Currently, when an agent hits an API usage limit, it might be detected as `blocked` (waiting for input) or `failed`. We need a specific state to indicate this condition so the user knows why the agent is stalled.

## Requirements

1.  **New Status**: Add `StatusBlockedAPI` (or similar) to `Status` enum in `internal/model/event.go`.
2.  **Detection Logic**: Update `internal/daemon/monitor.go`'s `detectStatus` or `isFailed` logic to recognize API limit messages.
3.  **Patterns**: Identify common API limit messages for supported agents (Claude, Codex, Gemini).
    *   Claude: "Cost limit reached", "Rate limit exceeded" (sometimes these are fatal, sometimes temporary)
    *   Gemini: "Quota exceeded", "Resource exhausted"
    *   Codex/OpenAI: "Rate limit reached", "Insufficient quota"

## Implementation Plan

1.  **Update Model**:
    *   Modify `internal/model/event.go` to include `StatusBlockedAPI` (e.g., `blocked_api`).

2.  **Update Monitor**:
    *   In `internal/daemon/monitor.go`, add `isAPILimited(output string) bool`.
    *   Update `detectStatus` to check `isAPILimited` before `isFailed` or `StatusBlocked`.
    *   If `isAPILimited` is true, return `StatusBlockedAPI`.

3.  **Patterns to Match**:
    *   Need to collect specific strings. For now, start with common ones:
        *   "cost limit reached"
        *   "rate limit exceeded"
        *   "quota exceeded"
        *   "insufficient quota"

## verification

*   Simulate an API limit by mocking the output in a test case.
*   Verify the status transitions to `blocked_api`.
