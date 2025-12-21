---
type: issue
id: orch-026
title: Pass agent prompt via temporary file
summary: Avoid shell escaping issues by writing prompt to a file and asking agent to read it
status: open
priority: high
---

# Pass agent prompt via temporary file

Passing large prompts with complex characters (newlines, quotes) as command-line arguments to agent CLIs is causing shell corruption and escaping failures. It is not scalable.

## Solution

Instead of passing the full prompt text in the launch command:
1.  Write the full prompt content to a temporary file within the run's worktree (e.g., `.orch/prompt.md` or just `ORCH_PROMPT.md`).
2.  Launch the agent with a minimal instruction: "Read the file ORCH_PROMPT.md and follow the instructions within."

## Implementation Plan

1.  **Modify `internal/cli/run.go`**:
    *   In `runRun`, before launching the agent:
        *   Generate the full prompt string using `buildAgentPrompt`.
        *   Write this string to `filepath.Join(worktreePath, "ORCH_PROMPT.md")`.
    *   Update the `LaunchConfig`:
        *   Change `Prompt` field to be the static instruction: "Please read 'ORCH_PROMPT.md' in the current directory and follow the instructions found there."
        *   (Maybe keep the original prompt available in `LaunchConfig` if some adapters still want it? No, standardizing on file is safer for all).

2.  **Verify Adapters**:
    *   Ensure `claude`, `codex`, `gemini` adapters just take the string in `LaunchConfig.Prompt` and pass it.
    *   Since the new prompt is simple alphanumeric text, escaping issues should vanish.

3.  **Cleanup**:
    *   Should we delete the file? Probably not immediately, useful for debugging. It's in the worktree, so it's ephemeral-ish.
    *   Maybe add it to `.gitignore` if we don't want it committed? Or just name it `.ORCH_PROMPT.md`? The agent needs to see it. `ORCH_PROMPT.md` is explicit.

## Tasks
- [ ] Update `runRun` to write prompt file.
- [ ] Update prompt string passed to adapter.
