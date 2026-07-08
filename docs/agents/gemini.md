# Gemini Agent

Gemini is Google's AI assistant. orch supports Gemini CLI as an agent option.

## Prerequisites

Install the Gemini CLI:

```bash
# Installation instructions depend on Google's official distribution
# Check Google's documentation for the latest installation method

# Verify installation
gemini --version
```

## Configuration

### Basic setup

```yaml
# .orch/config.yaml
agent: gemini
```

### With custom prompt

```yaml
agent: gemini

gemini:
  prompt_template: "{{issue}}"
```

## Running with Gemini

```bash
# Use default agent (if configured as gemini)
orch run my-issue

# Explicitly specify gemini
orch run --agent gemini my-issue
```

## How orch uses Gemini

When starting a run, orch:

1. Creates a tmux session
2. Changes to the worktree directory
3. Launches gemini in yolo mode with the prompt:
   ```bash
   gemini --yolo --prompt-interactive "Your prompt here..."
   ```

`--yolo` is the default for autonomous operation (replaced entirely if
`gemini.extra_args` is configured), and the prompt is passed via
`--prompt-interactive` so the session stays interactive after the initial task.

## Environment Variables

Configure Google API access:

```bash
export GOOGLE_API_KEY=...
# or
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
```

## Prompt Configuration

### Simple pass-through

```yaml
gemini:
  prompt_template: "{{issue}}"
```

### Structured prompt

```yaml
gemini:
  prompt_template: |
    Task: {{issue_title}}
    
    Requirements:
    {{issue}}
    
    Please implement this task following best practices.
```

## Interacting with Gemini

### Attach to session

```bash
orch attach my-issue
```

### Send a message

```bash
orch send my-issue "Add more tests"

orch send my-issue <<'EOF'
Add more tests.
Focus on the regression scenario first.
EOF
```

## Troubleshooting

### Authentication

Verify API credentials:

```bash
echo $GOOGLE_API_KEY
```

### Model availability

Gemini models may vary by region and account type. Check your Google Cloud Console for available models.

## Comparison with Other Agents

| Feature | Claude | OpenCode | Codex | Gemini |
|---------|--------|----------|-------|--------|
| Provider | Anthropic | Multi | OpenAI | Google |
| Multi-modal | Limited | Via model | Limited | Strong |
| Context window | Large | Variable | Variable | Large |
| Best for | General | Flexibility | Code | Multi-modal |
