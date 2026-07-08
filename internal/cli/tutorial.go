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
  - Monitoring options and TUI keybindings`,
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

Prerequisite: your repo needs an 'origin' remote — orch derives the project
identity from its URL (normalized to <owner>-<repo>). Without one, every
command fails with 'project identity required'.

Create config directory in your repo root:

    mkdir -p .orch

Set up issues directory (separate from repo) in .orch/config.yaml:

    issues:
      path: ~/my-project-issues

This creates the structure:

    ~/my-project-issues/
    ├── issues/       # issue markdown files
    └── runs/         # run logs (auto-created)

Register the project with the daemon (required — maps the identity to this
checkout, stored in ~/.config/orch/projects/<project_id>.yaml, effective
immediately):

    orch daemon repo register "$(pwd)"

Start the local worker (required even for purely local runs — the daemon
starts automatically on your first command, the worker does not, and it does
not survive reboots):

    orch worker start
    orch worker status

--------------------------------------------------------------------------------
2. CONFIGURE DEFAULT AGENT AND MODEL
--------------------------------------------------------------------------------

Create .orch/config.yaml:

    agent: codex

    codex:
      default_model: openai/gpt-5.5
      default_variant: xhigh

    opencode:
      default_model: anthropic/claude-opus-4-6
      default_variant: max

Agents: codex, opencode, claude, gemini (override per run with 'orch run --agent').
Available model format: <provider>/<model-id>

Providers: anthropic, openai, google, etc.
Use 'orch models' to list available models (requires opencode server).

--------------------------------------------------------------------------------
3. FIRST RUN
--------------------------------------------------------------------------------

Create an issue:

    orch issue create my-001 --title "My first task" --body "Description here"

Or manually create ~/my-project-issues/issues/my-001.md — YAML frontmatter is
required, and the store is strict about it (one malformed file breaks every
issue command; status must be open/closed/resolved):

    ---
    type: issue
    id: my-001
    title: My first task
    status: open
    ---

    Description here.

Start a run:

    orch run my-001

Check status:

    orch ps

First run on a fresh machine: the agent CLI shows a one-time trust prompt
("Do you trust the contents of this directory?") and the run parks at
'waiting'. This is the normal interaction loop, not a failure:

    orch capture my-001     # see what the agent is asking
    orch send my-001 ""     # empty message = press Enter (accept the default)

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
	orch restart-from <run> Restart from a failed/canceled/unknown run
    orch capture <run>      Capture agent session to markdown

Note: 'orch monitor' is the built-in Go TUI shipped with the orch CLI.
'orch-monitor' is the optional standalone Python TUI installed from
orch-monitor-tui; both inspect the same daemon state, but they are separate
frontends.

--------------------------------------------------------------------------------
5. RUN STATUSES
--------------------------------------------------------------------------------

    Status      Meaning                   Action
    ------      -------                   ------
    running     Agent is working          Wait or 'attach' to watch
    waiting     Input box is free: needs  'capture' to read, then
                input OR finished a turn  'send'/'attach' to answer
    pr_open     PR created                Review the PR
    done        Work complete             'orch resolve <issue>' to ack
    failed      Error occurred            Check logs, 'restart-from'

To check why a run is waiting:

    orch capture <run>
    orch show <run>

--------------------------------------------------------------------------------
6. WHEN THINGS GO WRONG
--------------------------------------------------------------------------------

'no active workers available' — the worker is not running (it does not
survive reboots):

    orch worker start

'project identity required' — no 'origin' remote, or you are outside the
repo (commands are project-scoped by your current directory).

'unknown project_id "..." (register daemon project mapping)':

    orch daemon repo register "$(pwd)"

Fix daemon, orphaned sessions, stale states:

    orch repair

List running daemons:

    orch daemon list

Kill a stuck daemon (it restarts on demand; 'orch repair' also covers this):

    orch daemon kill

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

orch-monitor is the optional standalone Python terminal UI for managing
issue-driven development. It is separate from the built-in 'orch monitor'
command.

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

--------------------------------------------------------------------------------
9. REMOTE MASTER (MULTI-HOST)
--------------------------------------------------------------------------------

Point every command at a shared master daemon via client.yaml
(global: ~/.config/orch/client.yaml, or per-repo: .orch/client.yaml):

    remote:
      default: "<master-host>:7777"

As with local use, each host that should execute runs needs its own worker —
started on that host, registered to the master:

    orch worker start
    orch worker status

Notes for remote mode:

  - 'orch run --on <target>' names are resolved by the MASTER's config.targets,
    and the resolved host is baked into the run at creation. Keep hostnames
    current and restart the master daemon after changing them.
  - Pass --project <origin URL> explicitly (or set ORCH_PROJECT); 'issue create'
    does not infer it from your CWD.
  - With the file backend, issue files are created on the master's checkout of
    the project.

================================================================================
For more information, see: https://github.com/proboscis/orch
================================================================================
`
