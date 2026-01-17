"""Entry point for orch monitor TUI."""

import argparse
import os
import subprocess
import sys
from pathlib import Path

from .app import IssuesDashboard, OrchMonitorApp, RunsDashboard
from .config import Config
from .daemon import DaemonClient


SESSION_NAME = "orch-monitor-tui"
CONTROL_PROMPT_FILE = "ORCH_CONTROL_PROMPT.md"
CONTROL_PROMPT_INSTRUCTION = f"ultrathink Please read '{CONTROL_PROMPT_FILE}' in the current directory and follow the instructions found there."


def write_control_prompt(vault_path: Path | None) -> bool:
    try:
        config = Config.from_vault(vault_path) if vault_path else Config.load()
        daemon = DaemonClient(config.socket_path)

        if not daemon.is_available():
            return False

        cwd = os.getcwd()

        issues_text = "No issues found."
        runs_text = "No active runs."

        try:
            issues_resp = daemon.list_issues()
            if issues_resp.issues:
                lines = ["| ID | Status | Title |", "|----|--------|-------|"]
                for issue in issues_resp.issues[:20]:
                    title = (issue.title or issue.summary or "-")[:50]
                    lines.append(f"| {issue.id} | {issue.status.value} | {title} |")
                issues_text = "\n".join(lines)
        except Exception:
            pass

        try:
            runs_resp = daemon.list_runs()
            active = [
                r
                for r in runs_resp.runs
                if r.status.value
                in ("running", "blocked", "blocked_api", "booting", "queued")
            ]
            if active:
                lines = ["| Issue | Run ID | Status |", "|-------|--------|--------|"]
                for run in active[:10]:
                    lines.append(
                        f"| {run.issue_id} | {run.short_id()} | {run.status.value} |"
                    )
                runs_text = "\n".join(lines)
        except Exception:
            pass

        prompt = f"""You are the orch control agent for this repository.
You can run orch commands directly via bash to manage issues and runs.

## Repository Context

- Vault: {config.vault_path}
- Working directory: {cwd}

## Existing Issues

{issues_text}

## Active Runs

{runs_text}

## Available Orch Commands

Run these commands directly using bash:

### Issue Management
- Create issue: `orch issue create <id> --title "<title>" --body "<body>"`
- List issues: `orch issue list`
- Open issue in editor: `orch open <issue-id>`

### Run Management
- Start a run: `orch run <issue-id>`
- List runs: `orch ps`
- Attach to run: `orch attach <run-ref>`
- Stop a run: `orch stop <issue-id>#<run-id>`
- Resolve a run: `orch resolve <issue-id>#<run-id>`

## Instructions

- Execute orch commands directly via bash
- Use `orch ps` to check run status
- Use `orch attach` to interact with blocked runs
"""

        prompt_path = Path(cwd) / CONTROL_PROMPT_FILE
        prompt_path.write_text(prompt)
        return True
    except Exception:
        return False


def get_vault_path(args) -> Path | None:
    if args.vault:
        return args.vault
    vault_env = os.getenv("ORCH_VAULT")
    if vault_env:
        return Path(vault_env)
    return None


def launch_tmux_layout(
    vault_path: Path | None, agent: str = "opencode", new: bool = False
):
    vault_arg = f"--vault {vault_path}" if vault_path else ""

    existing = subprocess.run(
        ["tmux", "has-session", "-t", SESSION_NAME],
        capture_output=True,
    )

    if existing.returncode == 0:
        if new:
            subprocess.run(["tmux", "kill-session", "-t", SESSION_NAME])
        else:
            subprocess.run(["tmux", "attach-session", "-t", SESSION_NAME])
            return

    python_exec = sys.executable
    cwd = os.getcwd()
    env_export = f"export ORCH_VAULT='{vault_path}'; " if vault_path else ""

    subprocess.run(
        [
            "tmux",
            "new-session",
            "-d",
            "-s",
            SESSION_NAME,
            "-x",
            "180",
            "-y",
            "50",
            "-c",
            cwd,
        ]
    )

    subprocess.run(["tmux", "split-window", "-h", "-t", SESSION_NAME, "-c", cwd])
    subprocess.run(
        [
            "tmux",
            "split-window",
            "-v",
            "-t",
            f"{SESSION_NAME}:0.0",
            "-p",
            "70",
            "-c",
            cwd,
        ]
    )

    runs_cmd = f'{env_export}"{python_exec}" -m orch_monitor --runs {vault_arg}'.strip()
    issues_cmd = (
        f'{env_export}"{python_exec}" -m orch_monitor --issues {vault_arg}'.strip()
    )

    write_control_prompt(vault_path)
    if agent == "opencode":
        agent_cmd = f'opencode --prompt "{CONTROL_PROMPT_INSTRUCTION}"'
    else:
        agent_cmd = agent

    subprocess.run(
        ["tmux", "send-keys", "-t", f"{SESSION_NAME}:0.0", runs_cmd, "Enter"]
    )
    subprocess.run(
        ["tmux", "send-keys", "-t", f"{SESSION_NAME}:0.1", issues_cmd, "Enter"]
    )
    subprocess.run(
        ["tmux", "send-keys", "-t", f"{SESSION_NAME}:0.2", agent_cmd, "Enter"]
    )

    subprocess.run(["tmux", "select-pane", "-t", f"{SESSION_NAME}:0.2"])

    subprocess.run(["tmux", "attach-session", "-t", SESSION_NAME])


def main():
    parser = argparse.ArgumentParser(description="Orch monitor TUI")
    parser.add_argument(
        "--vault",
        type=Path,
        help="Path to orch vault directory",
    )
    parser.add_argument(
        "--runs",
        action="store_true",
        help="Show runs dashboard only (for tmux pane)",
    )
    parser.add_argument(
        "--issues",
        action="store_true",
        help="Show issues dashboard only (for tmux pane)",
    )
    parser.add_argument(
        "--agent",
        default="opencode",
        help="Control agent command (default: opencode)",
    )
    parser.add_argument(
        "--new",
        action="store_true",
        help="Kill existing session and start fresh",
    )

    args = parser.parse_args()
    vault_path = get_vault_path(args)

    if args.runs:
        app = RunsDashboard(vault_path=vault_path)
        app.run()
    elif args.issues:
        app = IssuesDashboard(vault_path=vault_path)
        app.run()
    else:
        launch_tmux_layout(vault_path, args.agent, args.new)


if __name__ == "__main__":
    main()
