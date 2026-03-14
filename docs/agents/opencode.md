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

When starting a run, orch:

1. Creates a tmux session
2. Changes to the worktree directory
3. Launches opencode with model and prompt:
   ```bash
   opencode --model anthropic/claude-opus-4-5 --variant max "prompt..."
   ```

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
  thorough:
    agent: opencode
    model: anthropic/claude-opus-4-5
    model_variant: max
  
  fast:
    agent: opencode
    model: anthropic/claude-sonnet-4
    model_variant: fast
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

### Custom environment

```yaml
opencode:
  env:
    ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
    OPENCODE_CONFIG: ~/.opencode/config.yaml
```

### Model-specific settings

Configure different prompts for different models:

```yaml
opencode:
  default_model: anthropic/claude-opus-4-5
  
  model_settings:
    "anthropic/claude-opus-4-5":
      prompt_template: |
        ultrawork Be thorough and comprehensive.
        {{issue}}
    
    "anthropic/claude-sonnet-4":
      prompt_template: |
        Be concise but complete.
        {{issue}}
```

## Comparison with Claude

| Feature | Claude Code | OpenCode |
|---------|-------------|----------|
| Multi-provider | No (Anthropic only) | Yes |
| Model selection | Automatic | Configurable |
| Open source | No | Yes |
| MCP support | Yes | Yes |
| Custom prompts | Via templates | Via templates |
