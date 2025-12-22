---
type: issue
id: orch-025
title: Fix Gemini CLI interactive session termination
summary: Modify Gemini agent integration to keep the interactive session open after the initial prompt
status: resolved
priority: high
---

# Fix Gemini CLI interactive session termination

The `gemini` CLI tool currently terminates after processing the initial prompt provided via `-p`, which prevents subsequent interactive chat in the tmux session. We need to modify the invocation to ensure the session remains open.

## Context

Current invocation:
```go
args = append(args, "gemini", "--yolo")
// ...
args = append(args, "-p", prompt)
```

The user reports that this command exits after the prompt.

## Potential Solutions

1.  **Check for `--interactive` or similar flag**: Some CLI tools have a flag to force interactive mode even when a prompt is passed.
2.  **Pipe/Input Redirection**: Instead of `-p`, pipe the prompt to stdin?
    *   `echo "prompt" | gemini --yolo` might also exit after EOF.
    *   But `(echo "prompt"; cat) | gemini --yolo` might keep stdin open?
3.  **No `-p` flag**:
    *   Start `gemini --yolo` without `-p`.
    *   Use `tmux send-keys` to send the prompt *after* the session starts.
    *   This is robust and works for any REPL.

## Chosen Approach: `tmux send-keys`

This approach is the most reliable for generic CLI tools that drop into a REPL:
1.  Launch the agent command *without* the initial prompt argument.
2.  Wait for the session to start (or just send immediately since tmux buffers input).
3.  Use `tmux send-keys` to type the prompt into the running session.

## Implementation Plan

1.  **Modify `internal/agent/gemini.go`**:
    *   Remove `-p` flag logic from `LaunchCommand`.
    *   Return the prompt string separately? Or handle it in the caller?
    *   Actually, `LaunchCommand` returns the command string. It cannot execute side effects like `tmux send-keys` easily because `orch run` handles the tmux creation *after* getting the command.

2.  **Refactor `LaunchCommand` contract**:
    *   Currently: `LaunchCommand(cfg) -> (cmdString, error)`
    *   Problem: The prompt is baked into `cmdString`.
    *   Solution:
        *   If the agent requires "type-in" interaction (like Gemini apparently does for persistence), we need a way to signal this.
        *   OR, we can change `orch run` logic to handle "prompt injection".

3.  **Alternative: Wrapper Script**
    *   Create a temporary wrapper script that does:
        ```bash
        #!/bin/bash
        # Start gemini in background or expect it to read from specific fd?
        # No, simpler:
        { echo "prompt"; cat; } | gemini --yolo
        ```
    *   This only works if `gemini` reads from stdin. If it strictly requires `-p` for the first prompt, this fails.
    *   If `gemini` *has* a `--chat` flag or similar, use that.

    *   **Research**: Does `gemini` CLI support stdin?
    *   If yes, `{ echo "prompt"; cat; } | gemini --yolo` is the cleanest shell-only solution.

4.  **Verification**:
    *   Try `echo "hello" | gemini --yolo` manually to see behavior. (Cannot do this here, I am the agent).
    *   Assumption: The user says "gemini cmd with prompt terminates".
    *   Let's try the `tmux send-keys` approach as it mimics human behavior perfectly.

5.  **Refactoring `orch run`**:
    *   In `internal/cli/run.go`:
        *   After `tmux.NewSession`, checks if there is a pending prompt that wasn't part of the CLI args?
        *   We need `LaunchConfig` to indicate "how to send prompt".
        *   Add `PromptInjectionMethod` enum to `LaunchConfig`: `Arg` (default), `Stdin`, `TmuxSendKeys`.
        *   Update `GeminiAdapter` to use `TmuxSendKeys`.
        *   Update `runRun` to handle `TmuxSendKeys`.

## Revised Plan (Minimal Impact)

1.  **Update `internal/agent/adapter.go`**:
    *   Add field `PromptInjection string` to `LaunchConfig` or return value of `LaunchCommand`?
    *   Better: Add `PromptInjection()` method to `Adapter` interface. Default returns `Arg`.
    *   `GeminiAdapter` returns `TmuxSendKeys`.

2.  **Update `internal/agent/gemini.go`**:
    *   Implement `PromptInjection()`.
    *   In `LaunchCommand`, *do not* append `-p` if injection is `TmuxSendKeys`.

3.  **Update `internal/cli/run.go`**:
    *   Check `adapter.PromptInjection()`.
    *   If `Arg` (default), proceed as before (prompt is in `agentCmd`).
    *   If `TmuxSendKeys`:
        *   Get `agentCmd` (without prompt).
        *   Start tmux session.
        *   Run `tmux.SendKeys(session, prompt)`.

4.  **Wait**: `LaunchCommand` interface returns `(string, error)`.
    *   We need to change `Adapter` interface or how we use it.
    *   Let's add `GetPromptInjectionMethod() InjectionMethod` to `Adapter`.

## Enums
*   `InjectionArg` (default)
*   `InjectionTmux`

## Tasks
1.  Define `InjectionMethod` in `internal/agent/adapter.go`.
2.  Update `Adapter` interface.
3.  Update `Claude` and `Codex` to return `InjectionArg`.
4.  Update `Gemini` to return `InjectionTmux` and remove `-p` logic.
5.  Update `internal/cli/run.go` to handle the injection.
6.  Add `internal/tmux/tmux.go` function `SendKeys(session, text string)`.
