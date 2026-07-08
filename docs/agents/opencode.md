# OpenCode Agent

OpenCode is an open-source AI coding assistant that supports multiple LLM providers. It offers flexibility in model selection and configuration.

## Prerequisites

Install OpenCode:

```bash
# Install via go
go install github.com/sst/opencode@latest

# Verify installation
opencode --version
```

## Configuration

### Basic setup

```yaml
# .orch/config.yaml
agent: opencode
```

### With model configuration

```yaml
agent: opencode

opencode:
  default_model: anthropic/claude-opus-4-5
  default_variant: max
```

### With custom prompt

```yaml
agent: opencode

opencode:
  default_model: anthropic/claude-opus-4-5
  default_variant: max
  prompt_template: |
    ultrawork Please read 'ORCH_PROMPT.md' in the current directory.
    
    {{issue}}
```

## Running with OpenCode

```bash
# Use default agent (if configured as opencode)
orch run my-issue

# Explicitly specify opencode
orch run --agent opencode my-issue

# With specific model
orch run --agent opencode --model anthropic/claude-opus-4-5 my-issue

# With model variant
orch run --agent opencode --model anthropic/claude-opus-4-5 --model-variant max my-issue
```

## Available Models

List available models:

```bash
orch models
```

Common models:
- `anthropic/claude-opus-4-5` - Most capable
- `anthropic/claude-sonnet-4` - Balanced
- `openai/gpt-4o` - OpenAI's flagship
- `openai/o3` - OpenAI reasoning model

### Model Variants

Some models support variants for different configurations:

| Variant | Description |
|---------|-------------|
| `default` | Standard configuration |
| `max` | Maximum thinking/capability |
| `fast` | Optimized for speed |

## How orch uses OpenCode

Unlike other agents, OpenCode runs as a headless HTTP server — the prompt is
never passed on the command line, and there is no `--variant` CLI flag. When
starting a run, orch:

1. Creates the worktree and writes `ORCH_PROMPT.md` into it
2. Starts an OpenCode server in the worktree:
   ```bash
   opencode serve --port <port> --hostname 0.0.0.0
   ```
3. Waits for the server's health endpoint, creates a session over the HTTP API,
   and injects the prompt — together with the resolved model and variant — via
   an HTTP message

`orch attach` opens the OpenCode TUI connected to the running server
(`opencode attach http://127.0.0.1:<port>`).

## Agent Detection

The orch daemon detects OpenCode's state similarly to Claude:

### Running indicators
- Active output
- OpenCode UI elements

### Blocked indicators
- Input prompt visible
- Waiting for user

### Exit indicators
- Shell prompt visible
- OpenCode exited

## Presets

Configure presets for different use cases:

```yaml
agent: opencode

opencode:
  default_model: anthropic/claude-sonnet-4
  default_variant: default

presets:
  - name: thorough
    backend: opencode
    model: anthropic/claude-opus-4-5
    variant: max

  - name: fast
    backend: opencode
    model: anthropic/claude-sonnet-4
    variant: fast
```

Use presets:

```bash
# Complex task requiring deep thinking
orch run --preset thorough complex-issue

# Quick fix
orch run --preset fast simple-bug
```

## Environment Variables

OpenCode uses environment variables for API keys:

```bash
# Anthropic
export ANTHROPIC_API_KEY=sk-ant-...

# OpenAI
export OPENAI_API_KEY=sk-...

# Other providers
export GOOGLE_API_KEY=...
```

## Prompt Templates

### Using ultrawork

OpenCode works well with the `ultrawork` prefix for complex tasks:

```yaml
opencode:
  prompt_template: |
    ultrawork Please read 'ORCH_PROMPT.md' in the current directory.
    
    {{issue}}
```

### Direct instructions

```yaml
opencode:
  prompt_template: |
    # Task: {{issue_title}}
    
    ## Requirements
    {{issue}}
    
    ## Guidelines
    - Follow existing code patterns
    - Write comprehensive tests
    - Create a PR when complete
```

## Interacting with OpenCode

### Attach to session

```bash
orch attach my-issue
```

### Send a message

```bash
orch send my-issue "Also update the documentation"

orch send my-issue <<'EOF'
Also update the documentation.
Call out any new config requirements.
EOF
```

### Capture output

```bash
orch capture my-issue
```

## Provider Configuration

### Anthropic

```yaml
opencode:
  default_model: anthropic/claude-opus-4-5
  # Requires ANTHROPIC_API_KEY
```

### OpenAI

```yaml
opencode:
  default_model: openai/gpt-4o
  # Requires OPENAI_API_KEY
```

### Google

```yaml
opencode:
  default_model: google/gemini-pro
  # Requires GOOGLE_API_KEY
```

## Troubleshooting

### Model not available

Check available models:

```bash
orch models
```

Ensure your API key has access to the requested model.

### API errors

Check environment variables:

```bash
echo $ANTHROPIC_API_KEY
echo $OPENAI_API_KEY
```

### Slow responses

If using `max` variant, responses may take longer. Consider using `default` variant for faster responses:

```yaml
opencode:
  default_variant: default
```

### OpenCode crashes

Check system resources and try with a smaller model variant.

## Advanced Configuration

### Extra CLI arguments

Pass additional flags to the `opencode serve` command:

```yaml
opencode:
  extra_args:
    - --log-level
    - DEBUG
```

## Comparison with Claude

| Feature | Claude Code | OpenCode |
|---------|-------------|----------|
| Multi-provider | No (Anthropic only) | Yes |
| Model selection | Automatic | Configurable |
| Open source | No | Yes |
| MCP support | Yes | Yes |
| Custom prompts | Via templates | Via templates |
