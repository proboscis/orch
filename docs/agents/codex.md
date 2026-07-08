# Codex Agent

Codex is OpenAI's coding-focused AI assistant. orch supports Codex as a first-class agent.

## Prerequisites

Install the Codex CLI:

```bash
# Installation instructions depend on OpenAI's official distribution
# Check OpenAI's documentation for the latest installation method

# Verify installation
codex --version
```

## Configuration

### Basic setup

```yaml
# .orch/config.yaml
agent: codex
```

### With model configuration

```yaml
agent: codex

codex:
  default_model: gpt-5.3-codex
  default_variant: xhigh  # reasoning effort: low|medium|high|xhigh
```

### With custom prompt

```yaml
agent: codex

codex:
  prompt_template: |
    Think step by step. Follow best practices.
    
    {{issue}}
```

## Running with Codex

```bash
# Use default agent (if configured as codex)
orch run my-issue

# Explicitly specify codex
orch run --agent codex my-issue

# With specific model and reasoning effort
orch run --agent codex --model gpt-5.3-codex --model-variant high my-issue
```

## How orch uses Codex

When starting a run, orch:

1. Creates a tmux session
2. Changes to the worktree directory  
3. Launches codex in yolo mode with the prompt:
   ```bash
   codex --yolo [--model <model>] [-c model_reasoning_effort=<variant>] 'Your prompt here...'
   ```

`--yolo` is the default for autonomous operation (replaced entirely if
`codex.extra_args` is configured). When a model is configured, orch passes it as
`--model`, stripping any `provider/` prefix from orch-style model IDs
(`openai/gpt-5.3-codex` becomes `gpt-5.3-codex`). When a variant is configured,
orch passes it as the reasoning-effort config override
`-c model_reasoning_effort=<level>` (`low|medium|high|xhigh`).

## Prompt Best Practices

### Step-by-step reasoning

Codex works well with explicit reasoning instructions:

```yaml
codex:
  prompt_template: |
    Think step by step about this problem.
    
    Task: {{issue_title}}
    
    Details:
    {{issue}}
    
    Instructions:
    1. Analyze the requirements
    2. Plan your approach
    3. Implement the solution
    4. Test thoroughly
    5. Create a PR
```

### Code-focused prompts

```yaml
codex:
  prompt_template: |
    You are a coding assistant working on: {{issue_title}}
    
    Requirements:
    {{issue}}
    
    Guidelines:
    - Write clean, readable code
    - Follow existing patterns in the codebase
    - Add comments for complex logic
    - Include tests for new functionality
```

## Profiles

The codex CLI is not passed a profile flag. orch implements profiles by setting
the `CODEX_HOME` environment variable for the agent process: each entry in
`codex.profiles` binds a profile name to a `CODEX_HOME` directory, isolating
authentication (separate Codex accounts).

```yaml
codex:
  # Profile selected when --codex-profile is not given
  default_profile: company
  profiles:
    company:
      codex_home: ~/.codex/profiles/company
    personal:
      codex_home: ~/.codex
```

Select a profile per run with the `--codex-profile` flag:

```bash
orch run --agent codex --codex-profile personal my-issue
```

Profiles can also pin execution targets (see `.orch/config.example.yaml`):

```yaml
codex:
  profiles:
    company:
      target: remote-worker            # config.targets name; omit to run locally
      allowed_targets: [remote-worker] # restrict where this profile may run
      codex_home: ~/.codex/profiles/company
```

## Environment Variables

Configure OpenAI API access:

```bash
export OPENAI_API_KEY=sk-...
```

## Interacting with Codex

### Attach to session

```bash
orch attach my-issue
```

### Send a message

```bash
orch send my-issue "Also add error handling"

orch send my-issue <<'EOF'
Also add error handling.
Cover the new path with tests.
EOF
```

## Troubleshooting

### API authentication

Verify your API key:

```bash
echo $OPENAI_API_KEY
```

### Model access

Ensure your OpenAI account has access to the Codex model.

### Rate limits

OpenAI has rate limits that may affect long-running tasks. Monitor for rate limit errors in the session output.

## Comparison with Other Agents

| Feature | Claude | OpenCode | Codex |
|---------|--------|----------|-------|
| Provider | Anthropic | Multi | OpenAI |
| Focus | General + coding | General + coding | Code-specialized |
| Context | Large | Variable | Variable |
| Best for | Complex tasks | Flexibility | Code generation |
