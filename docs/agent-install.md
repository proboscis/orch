# orch — Agent Install Runbook

**Audience: you, an AI coding agent** (Claude Code, Codex, OpenCode, or
similar). Your user pasted a one-liner asking you to set up orch. Follow the
steps below in order. Run the commands yourself, show the user what happened,
and ask before anything irreversible. The end state is:

1. the `orch` binary installed and working,
2. the orch skill installed into **this agent's** skills directory so every
   future session knows how to drive orch,
3. the user has completed one real orch run, guided by you.

Do NOT run `orch attach` or `orch-monitor` yourself — they are interactive
and will hang a non-interactive agent. Use `orch capture` and `orch send`
instead; offer `attach` to the user as a command *they* can type.

---

## Step 0 — Detect the environment

- OS: macOS or Linux (this runbook covers both).
- Which agent are you? This decides the skill directory in Step 3.
- Ask the user which agent CLI orch should launch for runs (claude / codex /
  gemini / opencode). It must already be logged in — orch reuses its
  credential state. Never handle raw API keys.

## Step 1 — Check prerequisites

```bash
git --version        # required
tmux -V              # required (agent sessions run inside tmux)
gh auth status       # optional; needed only for the PR workflow
<agent-cli> --version   # the CLI chosen above, e.g. codex --version
```

Report anything missing to the user and offer to install it
(`brew install tmux gh` / `apt install tmux gh`).

## Step 2 — Install the orch binary

```bash
curl -sSL https://raw.githubusercontent.com/proboscis/orch/main/install.sh | bash
orch --help   # verify; if "command not found", ensure ~/.local/bin is on PATH
```

## Step 3 — Install the orch skill into this agent

The skill teaches future sessions the orch execution model (daemon/worker,
project registration, run lifecycle, troubleshooting). Install it for the
agent you are running as — ask the user if they also want it installed for
their other agents.

```bash
git clone --depth 1 https://github.com/proboscis/orch /tmp/orch-skill-install
SKILL_SRC=/tmp/orch-skill-install/claude-plugins/orch-toolset/skills/orch-toolset

# Claude Code
mkdir -p ~/.claude/skills && cp -r "$SKILL_SRC" ~/.claude/skills/orch-toolset

# Codex (same SKILL.md format)
mkdir -p ~/.codex/skills && cp -r "$SKILL_SRC" ~/.codex/skills/orch-toolset

rm -rf /tmp/orch-skill-install
```

For an agent without a skills directory (e.g. OpenCode), append the
"Core Workflow" and "Quick Reference" sections of `SKILL.md` to the agent's
global instructions file instead (e.g. `~/.config/opencode/AGENTS.md`),
wrapped in `<!-- orch-skill start/end -->` markers so a re-install can
replace the block idempotently.

Verify: list the target directory and confirm `SKILL.md` is present. Note to
the user that the skill loads in **new** sessions, not the current one.

## Step 4 — Guided first run (do this together with the user)

Ask the user: try orch on an existing repo, or on a throwaway sandbox?
For a sandbox:

```bash
mkdir ~/orch-sandbox && cd ~/orch-sandbox && git init -b main
echo "# orch sandbox" > README.md
git remote add origin https://github.com/<user>/orch-sandbox.git   # identity only; needn't exist
mkdir .orch && printf 'agent: %s\nbase_branch: main\n' "<chosen-agent>" > .orch/config.yaml
git add -A && git commit -m "init orch sandbox"
```

Then walk through this sequence, explaining each step:

```bash
# 1. One-time wiring (REQUIRED — the most common first-run failure)
orch daemon repo register "$(pwd)"   # maps project identity -> this checkout
# (daemon and worker auto-start on demand; `orch worker status` shows them)

# 2. Describe a task. THE ISSUE BODY IS THE WORKER AGENT'S ONLY CONTEXT —
#    write it as a complete brief (goal, constraints, acceptance criteria,
#    how to verify). Never cram it into a one-line --body. As the agent,
#    author it with a heredoc; tell the user that when THEY write issues by
#    hand, `orch issue create <id> --title "..." --edit` (or
#    `orch issue edit <id>` later) opens their $EDITOR instead.
orch issue create hello-world --title "Add a hello world script" <<'EOF'
## Goal
Create hello.py at the repository root that prints exactly "Hello, World!".

## Constraints
- Python 3, standard library only, a few lines.
- Do not create or modify anything else.

## Acceptance criteria
- `python3 hello.py` outputs `Hello, World!` and exits 0.

## Verification
Run `python3 hello.py` and show the output in your final report.
EOF

orch run hello-world --no-pr          # drop --no-pr when the repo really exists on GitHub

# 3. Observe
orch ps                               # booting -> running -> waiting
orch capture hello-world              # read the agent's screen
```

**Expect a trust prompt on the first run**: the agent CLI asks
"Do you trust the contents of this directory?" and the run parks at
`waiting`. Show the user the captured screen, then answer it:

```bash
orch send hello-world ""              # empty message = press Enter (accept default)
```

Wait for the turn to finish and show the user the result:

```bash
orch wait hello-world --timeout 600
orch capture hello-world              # the agent's report
ls ~/.orch/worktrees/hello-world/*/   # the isolated worktree with the changes
```

Finish: `orch stop hello-world` then `orch resolve hello-world`. Explain
that with a real GitHub repo (and `gh` logged in), dropping `--no-pr` makes
the agent commit, push, and open a PR — the run then parks at `pr_open`.

### If something fails

| Error | Fix |
|-------|-----|
| `project identity required` | repo has no `origin` remote, or you are outside the repo |
| `unknown project_id "…"` | run `orch daemon repo register "$(pwd)"` |
| `no active workers available` | should not happen locally (workers auto-start); check `ORCH_WORKER_AUTOSTART`, or run `orch worker start` |
| `store_error` on issue commands | a malformed issue file; frontmatter `status` must be open/closed/resolved |

## Step 5 — Wrap up

Tell the user:

- new agent sessions will load the orch skill automatically,
- `orch tutorial` shows the built-in reference anytime,
- deeper docs: [Getting Started](./getting-started.md),
  [ローカルクイックスタート (日本語)](./local-quickstart.ja.md),
  [Daily Workflow](./daily-workflow.md), and
  [Remote Usage](./remote-usage.md) is the next milestone (multi-host —
  out of scope for the current beta).
