---
type: issue
id: orch-028
title: Improve orch control agent starting prompt
status: open
---

# Improve orch control agent starting prompt

The current orch control agent prompt is too basic and has several issues:

## Problems
1. Does not respect issue ID creation rules/conventions in the repo
2. Agent is not aware of repo-specific context and patterns
3. No guidance on how to discover existing conventions
4. Uses awkward ORCH_CMD: protocol - should just run commands directly

## Proposed Solution
- Create orch control agent using a tmp prompt file approach
- Agent should be given a prompt to read the tmp file containing:
  - Issue ID naming conventions
  - Repo-specific rules and patterns
  - Available orch commands with proper usage
  - Context about the current state of the system
- This allows dynamic prompt generation based on repo state
- Remove ORCH_CMD: protocol - agent should execute orch commands directly via bash

## Benefits
- More context-aware issue creation
- Consistent with repo conventions
- Easier to update/maintain the agent's knowledge
- Cleaner interaction without protocol prefix
