"""Entry point for orch monitor TUI."""

import argparse
import os
import subprocess
import sys
from pathlib import Path

from .app import IssuesDashboard, OrchMonitorApp, RunsDashboard


SESSION_NAME = "orch-monitor-tui"


def get_vault_path(args) -> Path | None:
    if args.vault:
        return args.vault
    vault_env = os.getenv("ORCH_VAULT")
    if vault_env:
        return Path(vault_env)
    return None


def launch_tmux_layout(vault_path: Path | None, agent: str = "opencode"):
    vault_arg = f"--vault {vault_path}" if vault_path else ""

    existing = subprocess.run(
        ["tmux", "has-session", "-t", SESSION_NAME],
        capture_output=True,
    )

    if existing.returncode == 0:
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

    args = parser.parse_args()
    vault_path = get_vault_path(args)

    if args.runs:
        app = RunsDashboard(vault_path=vault_path)
        app.run()
    elif args.issues:
        app = IssuesDashboard(vault_path=vault_path)
        app.run()
    else:
        launch_tmux_layout(vault_path, args.agent)


if __name__ == "__main__":
    main()
