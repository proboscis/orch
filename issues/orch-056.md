---
type: issue
id: orch-056
title: Add --agent option to orch monitor
status: resolved
---

# Add --agent option to orch monitor

## Goal

Allow users to specify which control agent to use in the `orch monitor` dashboard via a CLI flag. Currently, it defaults to the agent specified in the configuration or "claude".

## Requirements

1. **CLI Flag Implementation**
   - Update `internal/cli/monitor.go` to add an `--agent` flag (e.g., `-a`) to the `monitor` command.
   - The flag should accept agent names like `claude`, `gemini`, `codex`, etc.

2. **Monitor Package Update**
   - Update `internal/monitor/monitor.go`'s `Options` struct to include an `Agent` field.
   - Ensure the `New` constructor and the `Monitor` struct handle this new field.

3. **Agent Selection Logic**
   - Modify `agentChatCommand()` in `internal/monitor/monitor.go` to prioritize the agent specified via the CLI flag.
   - If no flag is provided, fall back to the agent configured in `config.yaml`, and finally to "claude" as the default.

4. **Verification**
   - Run `orch monitor --agent gemini` (or another available agent) and verify that the control agent pane launches the correct agent.

## Relevant Locations

- `internal/cli/monitor.go`: CLI flag definition.
- `internal/monitor/monitor.go`: Agent launching logic in `agentChatCommand`.
- `internal/agent/adapter.go`: Agent factory logic.
