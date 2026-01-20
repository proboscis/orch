# Codex Agent

Codex is OpenAI's coding-focused AI assistant. orch supports Codex as an agent option.

> **Note:** Codex support is currently in development. Some features may not be fully implemented.

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
```

## How orch uses Codex

When starting a run, orch:

1. Creates a tmux session
2. Changes to the worktree directory  
3. Launches codex with the prompt:
   ```bash
   codex "Your prompt here..."
   ```

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
