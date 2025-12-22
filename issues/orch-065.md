---
type: issue
id: orch-065
title: Fix orch send command for Gemini agent (missing enter key)
status: open
---

# Fix orch send command for Gemini agent (missing enter key)

The orch send command for the Gemini agent currently inputs the message into the chat prompt but does not send it because it's missing the final Enter key stroke. This requires the user to manually press Enter in the terminal to send the message.
