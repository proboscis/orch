---
type: issue
id: orch-050
title: "orch run starts agent in orch repo instead of project directory"
status: resolved
---

# orch run starts agent in orch repo instead of project directory

## Problem

`orch run` starts the agent (e.g., codex) in `~/repos/orch/` instead of the current project directory where the command was invoked.

## Expected Behavior

When running `orch run ISSUE-PLC-142 --agent codex` from `/Users/s22625/repos/manga/placement`, the agent should:
1. Create worktree in the project directory (`.git-worktrees/...`)
2. Start the agent with cwd set to that project worktree

## Actual Behavior

The agent starts in `~/repos/orch/` directory, not the project where the issue belongs.

## Related

This is related to orch-049 (issue creation path bug). Both indicate that orch commands are not respecting the current project context.

## Reproduction

```bash
cd ~/repos/manga/placement
orch run ISSUE-PLC-142 --agent codex
# Agent starts in ~/repos/orch instead of ~/repos/manga/placement/.git-worktrees/...
```
