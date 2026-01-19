package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTutorialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tutorial",
		Short: "Show orch setup guide and usage reference",
		Long: `Display a practical setup guide and usage reference for orch.

This tutorial covers:
  - Initial setup (config directory and issues root)
  - Agent and model configuration
  - Basic workflow commands
  - Run statuses and what they mean
  - Troubleshooting tips
  - LLM agent skill installation`,
		Run: func(cmd *cobra.Command, args []string) {
			printTutorial()
		},
	}

	return cmd
}

func printTutorial() {
	fmt.Print(tutorialText)
}

const tutorialText = `
================================================================================
                           ORCH TUTORIAL
================================================================================

orch is an orchestrator for managing LLM agent runs on issues.
This guide covers setup and basic usage.

--------------------------------------------------------------------------------
1. INITIAL SETUP
--------------------------------------------------------------------------------

Create config directory in your repo root:

    mkdir -p .orch

Set up issues directory (separate from repo):

    export ORCH_ISSUES_ROOT=~/my-project-issues  # add to shell profile

This creates the structure:

    ~/my-project-issues/
    ├── issues/       # issue markdown files
    └── runs/         # run logs (auto-created)

Or specify issues.path in .orch/config.yaml:

    issues:
      path: ~/my-project-issues

--------------------------------------------------------------------------------
2. CONFIGURE DEFAULT AGENT AND MODEL
--------------------------------------------------------------------------------

Create .orch/config.yaml:

    agent: opencode

    opencode:
      default_model: anthropic/claude-opus-4-5
      default_variant: max

Available model format: <provider>/<model-id>

Providers: anthropic, openai, google, etc.
Use 'orch models' to list available models (requires opencode server).

--------------------------------------------------------------------------------
3. FIRST RUN
--------------------------------------------------------------------------------

Create an issue:

    orch issue create my-001 --title "My first task" --body "Description here"

Or manually create ~/my-project-issues/issues/my-001.md:

    # My first task

    Description here.

Start a run:

    orch run my-001

Check status:

    orch ps

Interact with the agent:

    orch attach my-001

--------------------------------------------------------------------------------
4. KEY COMMANDS
--------------------------------------------------------------------------------

    Command                 Purpose
    -------                 -------
    orch run <issue>        Start work on an issue
    orch ps                 List all runs and their status
    orch attach <run>       Interact with running agent
    orch stop <issue>       Stop all runs for an issue
    orch send <run> "msg"   Send message to running agent
    orch monitor            TUI dashboard for all runs
    orch show <issue>       Show issue details
    orch continue <run>     Resume a paused or blocked run
    orch capture <run>      Capture agent session to markdown

--------------------------------------------------------------------------------
5. RUN STATUSES
--------------------------------------------------------------------------------

    Status      Meaning                   Action
    ------      -------                   ------
    running     Agent is working          Wait or 'attach' to watch
    blocked     Agent needs input         'attach' to help
    done        Work complete             Review the PR
    failed      Error occurred            Check logs, retry
    resolved    Run completed and acked   No action needed

To check why a run is blocked:

    orch show <run>

--------------------------------------------------------------------------------
6. WHEN THINGS GO WRONG
--------------------------------------------------------------------------------

Fix daemon, orphaned sessions, stale states:

    orch repair

Check daemon status:

    orch daemon status

Restart daemon:

    orch daemon restart

View run logs:

    orch log <run>

Debug mode (shows internal state):

    orch debug <run>

--------------------------------------------------------------------------------
7. WORKFLOW TIPS
--------------------------------------------------------------------------------

Use short IDs for convenience:

    orch attach a3      # Matches run starting with 'a3'
    orch stop a3b4      # Matches run starting with 'a3b4'

Query runs with SQL:

    orch query "SELECT * FROM runs WHERE status = 'running'"
    orch query "SELECT issue_id, count(*) FROM runs GROUP BY issue_id"

Capture all completed runs:

    orch capture-all

--------------------------------------------------------------------------------
8. LLM AGENT SKILL INSTALLATION
--------------------------------------------------------------------------------

Export orch skill package so your LLM agents know how to use orch:

    orch skill export <directory>

OPENCODE INSTALLATION:

    Global:  orch skill export ~/.config/opencode/skills/orch/
    Project: orch skill export .opencode/skills/orch/

    Discovery paths (searched upward from cwd):
    - .opencode/skills/<name>/SKILL.md
    - .claude/skills/<name>/SKILL.md (Claude-compat)
    - ~/.config/opencode/skills/<name>/SKILL.md
    - ~/.claude/skills/<name>/SKILL.md (Claude-compat)

CLAUDE CODE INSTALLATION:

    Global:  orch skill export ~/.claude/skills/orch/
    Project: orch skill export .claude/skills/orch/

    Discovery paths:
    - .claude/skills/<name>/SKILL.md
    - ~/.claude/skills/<name>/SKILL.md

    Also auto-loads CLAUDE.md from project root.

CODEX (OPENAI) INSTALLATION:

    Global:  orch skill export ~/.codex/skills/orch/
    Project: orch skill export .codex/skills/orch/

    Discovery paths:
    - .codex/skills/<name>/SKILL.md
    - ~/.codex/skills/<name>/SKILL.md

    Also auto-loads AGENTS.md from project root.

EXPORTED FILES:

    orch/
    ├── SKILL.md              # Main entry (frontmatter + overview)
    ├── commands.md           # All orch commands reference
    ├── workflows.md          # Common workflow patterns
    ├── troubleshooting.md    # Repair, debugging, common issues
    └── config.md             # Configuration options (includes current repo config)

EXPORT TO STDOUT:

    orch skill export --stdout | pbcopy  # Copy to clipboard
    orch skill export --stdout > orch-skill.md  # Save to file

================================================================================
For more information, see: https://github.com/s22625/orch
================================================================================
`
