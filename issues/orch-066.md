---
type: issue
id: orch-066
title: Support model selection for agents (Codex/Claude/Gemini) in CLI and Monitor
status: open
---

# Support model selection for agents (Codex/Claude/Gemini) in CLI and Monitor

When starting a run, the user should be able to specify which model to use for the selected agent (Codex, Claude, Gemini). All these agents support specifying a model.

Requirements:
1. CLI: Add support for specifying the model (e.g., --model flag) in 'orch run'.
2. Monitor UI: Add a dialog to select the model or enter a custom one when starting a run from the dashboard.
3. Update agent adapters to pass the model to the underlying tools.

## Research Needed

Investigate the CLI flags and environment variables for each tool to ensure the model can be correctly specified. Also, identify valid/available model names for each.

- **Codex (OpenAI CLI):** Find the correct flag (e.g., `--model`) and list current model names (e.g., `gpt-4`, `o1`, etc.).
- **Claude (Anthropic CLI):** Find the correct flag (e.g., `--model`) and list current model names (e.g., `claude-3-5-sonnet-20241022`, `claude-3-opus-latest`).
- **Gemini (Google CLI):** Find the correct flag (e.g., `--model`) and list current model names (e.g., `gemini-1.5-pro`, `gemini-2.0-flash-exp`).