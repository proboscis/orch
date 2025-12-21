---
type: issue
id: orch-033
title: Add tmux-based agent communication commands
status: open
---

# Add tmux-based agent communication commands

## Summary

Add commands to send messages to agents and capture their output via tmux, enabling programmatic agent control without direct tmux interaction.

## Motivation

Currently, interacting with running agents requires direct access to tmux sessions. By adding orch commands for sending messages and capturing output, we can:
1. Allow users to control agents purely through orch CLI
2. Enable the orch controller agent to monitor and guide other agents
3. Provide a foundation for multi-agent orchestration

## Proposed Commands

### Send message to agent
```bash
orch send <issue-id>#<run-id> "<message>"
```
- Uses tmux send-keys to inject text into the agent's session
- Should handle special characters and newlines properly

### Capture agent output
```bash
orch capture <issue-id>#<run-id> [--lines N]
```
- Captures the latest output from the agent's tmux pane
- Optional --lines flag to specify how many lines to capture (default: 100)
- Returns the captured text to stdout

## Implementation Notes

- Both commands should work with the existing run identification (issue#run format)
- The send command should support sending Enter key (to submit prompts)
- The capture command should use tmux capture-pane
- Consider adding a --follow or --watch mode for capture

## Use Cases

1. User checks agent status: `orch capture orch-023#5b21ef`
2. User sends instruction: `orch send orch-023#5b21ef "Please focus on the UI tests first"`
3. Controller agent monitors blocked runs and provides guidance
4. Automated scripts can interact with agents programmatically

## Future Integration

Once implemented, the orch controller agent can use these commands to:
- Check why agents are blocked
- Send guidance or unblock instructions
- Coordinate multiple agents working on related issues
