# Orch Toolset Skill Plugin

Agent Skills for Claude Code that teach the orch CLI workflow: issue management, run orchestration, monitoring, and agent communication.

## Installation

### Local development

Load the plugin directly while testing:

```bash
claude --plugin-dir /path/to/orch/claude-plugins/orch-toolset
```

### Marketplace install

1. Publish this plugin in a Claude Code marketplace.
2. Install it by name:

```bash
claude plugin install orch-toolset@<marketplace>
```

For marketplace setup and publishing guidelines, see:
https://code.claude.com/docs/en/plugin-marketplaces

## Usage

Skills are model-invoked. Ask for help with orch commands or orchestration workflows, for example:

- "How do I start a new run for issue orch-055?"
- "Show me how to monitor multiple runs and send guidance."
- "What is the control agent workflow for orch?"

## Contents

- `skills/orch-toolset/SKILL.md`: Core orchestration guidance and best practices
- `skills/orch-toolset/reference.md`: Command quick reference and options
