---
type: issue
id: orch-046
title: orch send command doesn't send final Enter key to submit message
status: open
---

# orch send command doesn't send final Enter key to submit message

## Bug

The `orch send` command sends the message text to the agent's tmux session but doesn't send a final Enter key to actually submit the message.

## Current Behavior

Message is typed into the agent's input but not submitted - requires manual Enter key press.

## Expected Behavior

The `orch send` command should automatically send an Enter key after the message to submit it to the agent.

## Likely Fix

Add a final Enter key send after sending the message text in the tmux send-keys implementation.
