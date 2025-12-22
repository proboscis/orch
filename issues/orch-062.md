---
type: issue
id: orch-062
title: Gemini agent doesn't start automatically (missing final Enter key)
status: open
---

# Gemini agent doesn't start automatically (missing final Enter key)

## Goal

Ensure that when a run is launched with the agent set to `gemini`, it starts automatically by sending the necessary final Enter key to the tmux session.

## Problem

Currently, when launching a run with `agent: gemini`, the command is typed into the tmux session but the final Enter key is not sent, leaving the agent waiting for manual interaction to start.

## Requirements

1. **Automatic Start**
   - The Gemini agent should begin execution immediately after the run is launched.
   - Investigate the launch sequence in `internal/agent` and `internal/tmux`.

2. **Fix**
   - Ensure a newline or Enter key is sent at the end of the command string for Gemini runs.

## Relevant Locations

- `internal/agent/`: Logic for preparing and launching agent commands.
- `internal/tmux/`: Low-level tmux interaction (e.g., `SendKeys`).
