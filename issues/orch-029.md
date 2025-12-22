---
type: issue
id: orch-029
title: Fix Gemini agent idle detection in daemon
status: resolved
---

# Fix Gemini agent idle detection in daemon

The orch daemon is not correctly detecting when the Gemini agent is idle (waiting for user input/new prompt). This needs to be fixed so the daemon properly recognizes the Gemini agent's idle state.
