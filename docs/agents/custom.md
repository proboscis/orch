# Custom Agents

orch supports custom agents, allowing you to use any CLI tool as an agent backend. This is useful for proprietary tools, self-hosted models, or specialized workflows.

## Overview

A custom agent is an arbitrary shell command that orch runs inside the run's
multiplexer session, with the run worktree as the working directory:

1. orch creates the worktree and writes the task prompt to `ORCH_PROMPT.md`
2. orch starts the session and runs your command in the worktree
3. Your command reads `ORCH_PROMPT.md` (and the `ORCH_*` environment
   variables), works on the task, and exits

There is no `custom:` section in `.orch/config.yaml`. The command is provided
per run with the `--agent-cmd` flag.

## Usage

```bash
orch run my-issue --agent custom --agent-cmd "my-agent run"
```

`--agent-cmd` is required when `--agent custom` is used; orch fails fast
without it.

## Task Input: ORCH_PROMPT.md

orch does not pass the prompt to your command as an argument or via stdin.
Before launching the command, orch writes the rendered prompt (issue content
plus instructions) to `ORCH_PROMPT.md` in the worktree root. Since your command
starts with the worktree as its working directory, read it from the current
directory:

```bash
#!/bin/bash
# my-agent.sh - Simple custom agent wrapper

PROMPT="$(cat ORCH_PROMPT.md)"

# Your custom logic here
echo "Working on: $ORCH_ISSUE_ID"

# Call your actual agent/tool
my-internal-tool --prompt "$PROMPT"

# Exit with appropriate code
exit $?
```

Usage:

```bash
orch run my-issue --agent custom --agent-cmd "/path/to/my-agent.sh"
```

## Environment Variables

Custom agents receive these environment variables from orch:

| Variable | Description |
|----------|-------------|
| `ORCH_ISSUE_ID` | Issue ID |
| `ORCH_RUN_ID` | Run ID |
| `ORCH_RUN_PATH` | Path to run document |
| `ORCH_WORKTREE_PATH` | Git worktree path |
| `ORCH_BRANCH` | Git branch name |

### Using in your agent

```bash
#!/bin/bash
echo "Working in: $ORCH_WORKTREE_PATH"
echo "Issue: $ORCH_ISSUE_ID"
echo "Branch: $ORCH_BRANCH"
```

## Creating a Custom Agent

A custom agent is any CLI tool that can read the task input and work on it.

### Basic requirements

1. Read the task from `ORCH_PROMPT.md` in the working directory
2. Run in a terminal (tmux compatible)
3. Exit with code 0 on success
4. Exit with non-zero code on failure

### Example: Python agent wrapper

```python
#!/usr/bin/env python3
"""Custom agent wrapper for orch."""

import os
import sys
from pathlib import Path

def main():
    prompt = Path("ORCH_PROMPT.md").read_text()
    branch = os.environ["ORCH_BRANCH"]

    # Your custom agent logic
    result = call_your_model(prompt)

    # Apply changes, commit to branch, etc.
    apply_result(result, branch)

    sys.exit(0)

if __name__ == "__main__":
    main()
```

## Examples

### Local LLM wrapper

```bash
orch run code-issue --agent custom --agent-cmd "/path/to/ollama-agent.sh"
```

```bash
#!/bin/bash
# ollama-agent.sh
ollama run codellama "$(cat ORCH_PROMPT.md)"
```

### Specialized domain agents

Use different commands for different kinds of issues:

```bash
orch run infra-issue --agent custom --agent-cmd "terraform-agent"
orch run k8s-issue --agent custom --agent-cmd "k8s-agent"
```

## State Detection

orch observes the session's terminal output the same way as built-in agents.
Completion, error, and blocked patterns are not configurable for custom agents,
so:

1. Make your agent output clear status messages
2. Use proper exit codes (0 = success)
3. Check daemon logs if state looks wrong: `tail -f ~/Library/Logs/orch/daemon.log` (Linux: `~/.local/state/orch/daemon.log`)

## Troubleshooting

### Agent not starting

Check:
1. Command path is correct
2. Script has execute permissions
3. Dependencies are available

Debug:

```bash
orch run --verbose my-issue --agent custom --agent-cmd "my-agent"
```

### Environment issues

Verify environment variables are passed:

```bash
# Add to your agent script
env | grep ORCH
```

## Best Practices

1. **Clear output**: Make your agent output clear status messages
2. **Exit codes**: Use proper exit codes (0 = success)
3. **Logging**: Log to stderr for debugging, stdout for user-visible output
4. **Graceful shutdown**: Handle SIGTERM for clean stopping
5. **Progress indicators**: Output periodic progress to avoid "stalled" detection
