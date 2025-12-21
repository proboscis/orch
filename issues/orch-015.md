---
type: issue
id: orch-015
title: Add support for Gemini agent
summary: Finalize and verify support for Google Gemini CLI agent
status: open
priority: medium
---

# Add support for Gemini agent

Enable `orch` to use the Google Gemini CLI as an agent.

## Context

A basic skeleton for `GeminiAdapter` exists in `internal/agent/gemini.go`. It currently assumes a `gemini` CLI exists and follows a specific pattern.

## Requirements

1. **Verify CLI Interface**: Confirm the `gemini` CLI arguments. The current code assumes `gemini -p "prompt"`.
2. **Prompt Handling**: Ensure proper escaping and passing of the initial prompt.
3. **Session Management**: Investigate if the Gemini CLI supports resuming sessions or persistent context.
4. **Environment Propagation**: Ensure environment variables are passed correctly.
5. **Testing**: Add unit tests in `internal/agent/gemini_test.go`.

## Implementation Tasks

- [ ] Review `internal/agent/gemini.go` implementation
- [ ] Implement `LaunchCommand` with correct flags
- [ ] Add error handling for missing CLI
- [ ] Add unit tests
- [ ] (Optional) Add integration test if CLI is available in CI
