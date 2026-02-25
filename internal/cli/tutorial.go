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
  - orch-monitor TUI workflow and keybindings`,
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
    orch continue <run>     Resume a paused or waiting run
    orch capture <run>      Capture agent session to markdown

--------------------------------------------------------------------------------
5. RUN STATUSES
--------------------------------------------------------------------------------

    Status      Meaning                   Action
    ------      -------                   ------
    running     Agent is working          Wait or 'attach' to watch
    waiting     Agent needs input         'attach' to help
    done        Work complete             Review the PR
    failed      Error occurred            Check logs, retry
    resolved    Run completed and acked   No action needed

To check why a run is waiting:

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
8. ORCH-MONITOR TUI
--------------------------------------------------------------------------------

orch-monitor is a terminal UI for managing issue-driven development.

Launch the TUI:

    orch-monitor --new      # Start fresh session

Quick Start:
  1. Navigate with arrow keys, Tab to switch panels
  2. Press 'n' on an issue to start a new run
  3. Select agent (claude/opencode/codex)
  4. Monitor progress in the Runs panel
  5. Press 'a' or Enter to attach to a run
  6. Press '?' for help anytime

Keybindings:

    Key     Action
    ---     ------
    ?       Show help screen
    q       Quit
    r       Refresh data
    Tab     Switch between Runs/Issues tabs
    f       Filter runs/issues
    n       New run for selected issue
    a       Attach to selected run
    s       Stop selected run
    o       Open issue in editor

Workflow with Control Agent:
  1. Talk to control agent (bottom pane) to create issues
  2. Select issue and press 'n' to start a run
  3. Monitor status: queued → booting → running → pr_open → done
   4. Attach with 'a' when status is 'waiting'
  5. Review PR when status is 'pr_open'

Status Legend:

    Status      Meaning
    ------      -------
    queued      Run waiting to start
    booting     Agent starting up
    running     Agent actively working
    waiting     Agent needs input - attach!
    pr_open     PR created - review it
    done        Work completed

The Development Loop:

    Create Issue → Start Run → Monitor → Review PR → Merge → Repeat

Run multiple issues in parallel - each gets its own git worktree!

================================================================================
For more information, see: https://github.com/s22625/orch
================================================================================
`
