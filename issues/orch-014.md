---
type: issue
id: orch-014
title: Add support for Codex agent
summary: Finalize and verify support for OpenAI Codex CLI agent
status: open
priority: medium
---

# Add support for Codex agent

Enable `orch` to use the OpenAI Codex CLI as an agent.

## Context

A basic skeleton for `CodexAdapter` exists in `internal/agent/codex.go`, but it needs verification and likely expansion to match the capabilities of the Claude adapter.

## Requirements

1. **Verify CLI Interface**: Confirm the `codex` CLI arguments. The current code assumes `codex --full-auto`.
2. **Prompt Handling**: Ensure the prompt is correctly passed to the agent.
3. **Session Management**: Determine if Codex CLI supports stateful sessions (like Claude's `-p` or `--print`) and implement if so.
4. **Environment Propagation**: Verify `ORCH_*` variables are accessible to the agent.
5. **Testing**: Add unit tests in `internal/agent/codex_test.go`.

## Implementation Tasks

- [ ] Review `internal/agent/codex.go` implementation
- [ ] Implement `LaunchCommand` with correct flags
- [ ] Add error handling for missing CLI
- [ ] Add unit tests
- [ ] (Optional) Add integration test if CLI is available in CI
