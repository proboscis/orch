# Custom Agents

orch supports custom agents, allowing you to use any CLI tool as an agent backend. This is useful for proprietary tools, self-hosted models, or specialized workflows.

## Overview

Custom agents are external processes that:
1. Accept a prompt as input
2. Work on the task
3. Exit when done (or wait for input)

orch manages the tmux session and monitors the agent's state.

## Configuration

### Command-line usage

```bash
orch run my-issue --agent custom --agent-cmd "my-agent run"
```

### Config file

```yaml
# .orch/config.yaml
agent: custom

custom:
  command: my-agent run
  prompt_template: "{{issue}}"
```

## Creating a Custom Agent

A custom agent is any CLI tool that can accept a prompt and work on it.

### Basic requirements

1. Accept prompt as command-line argument or stdin
2. Run in a terminal (tmux compatible)
3. Exit with code 0 on success
4. Exit with non-zero code on failure

### Example: Simple wrapper script

```bash
#!/bin/bash
# my-agent.sh - Simple custom agent wrapper

PROMPT="$1"

# Your custom logic here
echo "Working on: $PROMPT"

# Call your actual agent/tool
my-internal-tool --prompt "$PROMPT"

# Exit with appropriate code
exit $?
```

Usage:

```yaml
custom:
  command: /path/to/my-agent.sh
```

### Example: Python agent wrapper

```python
#!/usr/bin/env python3
"""Custom agent wrapper for orch."""

import sys
import subprocess

def main():
    if len(sys.argv) < 2:
        print("Usage: my-agent <prompt>")
        sys.exit(1)
    
    prompt = sys.argv[1]
    
    # Your custom agent logic
    result = call_your_model(prompt)
    
    # Apply changes, etc.
    apply_result(result)
    
    sys.exit(0)

if __name__ == "__main__":
    main()
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

## Prompt Handling

### As command argument

Most agents accept the prompt as a command-line argument:

```yaml
custom:
  command: my-agent
  # orch runs: my-agent "the prompt"
```

### As stdin

For agents that read from stdin:

```yaml
custom:
  command: my-agent --stdin
  prompt_via_stdin: true
  # orch pipes prompt to stdin
```

### As file

For agents that read from a file:

```yaml
custom:
  command: my-agent --prompt-file ${PROMPT_FILE}
  prompt_via_file: true
  # orch writes prompt to PROMPT_FILE and sets environment variable
```

## State Detection

orch monitors custom agents the same way as built-in agents, looking for:

### Completion patterns

Configure patterns that indicate success:

```yaml
custom:
  command: my-agent
  completion_patterns:
    - "Task completed successfully"
    - "Done!"
    - "PR created:"
```

### Error patterns

Configure patterns that indicate failure:

```yaml
custom:
  command: my-agent
  error_patterns:
    - "Error:"
    - "Failed:"
    - "Exception:"
```

### Blocked patterns

Configure patterns that indicate the agent needs input:

```yaml
custom:
  command: my-agent
  blocked_patterns:
    - "Waiting for input"
    - "Please provide:"
    - "?"
```

## Examples

### Local LLM wrapper

```yaml
custom:
  command: ollama run codellama
  prompt_template: "{{issue}}"
```

### API-based agent

```yaml
custom:
  command: ./scripts/api-agent.sh
  env:
    API_KEY: ${MY_API_KEY}
    API_URL: https://my-api.example.com
```

### Multi-tool agent

```yaml
custom:
  command: ./scripts/multi-tool-agent.sh
  prompt_template: |
    Task: {{issue_title}}
    
    {{issue}}
    
    Available tools: git, make, docker
```

### Specialized domain agent

```yaml
custom:
  command: terraform-agent
  prompt_template: |
    Infrastructure task: {{issue_title}}
    
    {{issue}}
    
    Use Terraform to implement this infrastructure change.
```

## Advanced Configuration

### Multiple custom agents

Define different custom agents for different purposes:

```yaml
# In presets
presets:
  terraform:
    agent: custom
    agent_cmd: terraform-agent
  
  kubernetes:
    agent: custom
    agent_cmd: k8s-agent
  
  local-llm:
    agent: custom
    agent_cmd: ollama run codellama
```

Use with:

```bash
orch run --preset terraform infra-issue
orch run --preset local-llm code-issue
```

### Startup script

Run setup before the agent:

```yaml
custom:
  setup_script: ./scripts/agent-setup.sh
  command: my-agent
```

### Cleanup script

Run cleanup after the agent:

```yaml
custom:
  command: my-agent
  cleanup_script: ./scripts/agent-cleanup.sh
```

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

### State not detected

If orch can't detect your agent's state:
1. Configure explicit completion/error patterns
2. Ensure agent outputs recognizable messages
3. Check daemon logs: `tail -f ~/Library/Logs/orch/daemon.log   # Linux: ~/.local/state/orch/daemon.log`

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
