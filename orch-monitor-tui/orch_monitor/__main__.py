"""Entry point for orch monitor TUI."""

import argparse
import os
import subprocess
import sys
import time
from pathlib import Path
from typing import Protocol

from .app import IssuesDashboard, OrchMonitorApp, RunsDashboard, setup_logging
from .config import Config
from .daemon import DaemonClient
from .multiplexer import (
    MultiplexerType,
    get_default_multiplexer_type,
    get_multiplexer,
)


SESSION_NAME_PREFIX = "orch-monitor"
CONTROL_PROMPT_FILE = "ORCH_CONTROL_PROMPT.md"


def get_session_name(vault_path: Path | None = None) -> str:
    base_path = vault_path if vault_path else Path.cwd()
    repo_name = base_path.resolve().name
    safe_name = "".join(c if c.isalnum() or c in "-_" else "-" for c in repo_name)
    return f"{SESSION_NAME_PREFIX}-{safe_name}"


CONTROL_PROMPT_INSTRUCTION = f"ultrathink Please read '{CONTROL_PROMPT_FILE}' in the current directory and follow the instructions found there."
CONTROL_SESSION_FILE = "control-session.json"


def load_control_session(vault_path: Path | None) -> str | None:
    if vault_path:
        session_file = vault_path / ".orch" / CONTROL_SESSION_FILE
    else:
        session_file = Path.cwd() / ".orch" / CONTROL_SESSION_FILE

    if not session_file.exists():
        return None

    try:
        import json

        data = json.loads(session_file.read_text())
        return data.get("session_id")
    except Exception:
        return None


def write_control_prompt(vault_path: Path | None) -> bool:
    """Write control prompt file for the agent."""
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
    """Get vault path from args or environment."""
    if args.vault:
        return args.vault
    vault_env = os.getenv("ORCH_VAULT")
    if vault_env:
        return Path(vault_env)
    return None


DAEMON_STARTUP_TIMEOUT_SEC = 15
DAEMON_POLL_INTERVAL_SEC = 0.2


def ensure_daemon(vault_path: Path | None) -> tuple[bool, str]:
    """Ensure daemon is running. Returns (success, message)."""
    config = Config.from_vault(vault_path) if vault_path else Config.load()
    daemon = DaemonClient(config.socket_path)
    socket_path = config.socket_path

    if daemon.is_available():
        return True, ""

    print(f"Daemon socket not found at {socket_path}", file=sys.stderr)
    print("Starting orch daemon...", file=sys.stderr)

    vault_arg = ["--vault", str(vault_path)] if vault_path else []
    try:
        result = subprocess.run(
            ["orch"] + vault_arg + ["daemon", "start"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            return False, f"Failed to start daemon: {result.stderr.strip()}"
    except subprocess.TimeoutExpired:
        return False, "Daemon start command timed out"
    except FileNotFoundError:
        return False, "'orch' command not found. Is it installed?"

    polls_per_second = int(1 / DAEMON_POLL_INTERVAL_SEC)
    total_polls = DAEMON_STARTUP_TIMEOUT_SEC * polls_per_second
    for poll_count in range(total_polls):
        time.sleep(DAEMON_POLL_INTERVAL_SEC)
        if daemon.is_available():
            print("Daemon started.", file=sys.stderr)
            return True, ""
        elapsed_seconds = (poll_count + 1) // polls_per_second
        if (poll_count + 1) % polls_per_second == 0:
            print(f"  Waiting for daemon... ({elapsed_seconds}s)", file=sys.stderr)

    return (
        False,
        f"Daemon did not become available within {DAEMON_STARTUP_TIMEOUT_SEC}s. Socket: {socket_path}",
    )


class LayoutLauncher(Protocol):
    """Protocol for launching multiplexer layouts."""

    def has_session(self, session_name: str) -> bool:
        """Check if session exists."""
        ...

    def kill_session(self, session_name: str) -> None:
        """Kill existing session."""
        ...

    def attach_session(self, session_name: str) -> None:
        """Attach to existing session."""
        ...

    def launch_layout(
        self,
        session_name: str,
        vault_path: Path | None,
        agent: str,
        cwd: str,
    ) -> None:
        """Launch a new layout with runs, issues, and agent panes."""
        ...


class TmuxLayoutLauncher:
    """Tmux implementation of layout launcher."""

    def has_session(self, session_name: str) -> bool:
        result = subprocess.run(
            ["tmux", "has-session", "-t", session_name],
            capture_output=True,
        )
        return result.returncode == 0

    def kill_session(self, session_name: str) -> None:
        subprocess.run(["tmux", "kill-session", "-t", session_name])

    def attach_session(self, session_name: str) -> None:
        subprocess.run(["tmux", "attach-session", "-t", session_name])

    def launch_layout(
        self,
        session_name: str,
        vault_path: Path | None,
        agent: str,
        cwd: str,
    ) -> None:
        python_exec = sys.executable
        vault_arg = f"--vault {vault_path}" if vault_path else ""
        env_export = f"export ORCH_VAULT='{vault_path}'; " if vault_path else ""

        subprocess.run(
            [
                "tmux",
                "new-session",
                "-d",
                "-s",
                session_name,
                "-x",
                "180",
                "-y",
                "50",
                "-c",
                cwd,
            ]
        )

        subprocess.run(
            ["tmux", "split-window", "-v", "-t", session_name, "-p", "65", "-c", cwd]
        )
        subprocess.run(
            [
                "tmux",
                "split-window",
                "-h",
                "-t",
                f"{session_name}:0.1",
                "-p",
                "65",
                "-c",
                cwd,
            ]
        )
        # Split bottom pane vertically: pane 1 = issues (left), pane 2 = agent (right)
        subprocess.run(
            [
                "tmux",
                "split-window",
                "-h",
                "-t",
                f"{session_name}:0.1",
                "-p",
                "65",
                "-c",
                cwd,
            ]
        )

        runs_cmd = (
            f'{env_export}"{python_exec}" -m orch_monitor --runs {vault_arg}'.strip()
        )
        issues_cmd = (
            f'{env_export}"{python_exec}" -m orch_monitor --issues {vault_arg}'.strip()
        )

        write_control_prompt(vault_path)
        if agent == "opencode":
            agent_cmd = f'opencode --prompt "{CONTROL_PROMPT_INSTRUCTION}"'
        else:
            agent_cmd = agent

        subprocess.run(
            ["tmux", "send-keys", "-t", f"{session_name}:0.0", runs_cmd, "Enter"]
        )
        subprocess.run(
            ["tmux", "send-keys", "-t", f"{session_name}:0.1", issues_cmd, "Enter"]
        )
        subprocess.run(
            ["tmux", "send-keys", "-t", f"{session_name}:0.2", agent_cmd, "Enter"]
        )

        subprocess.run(["tmux", "select-pane", "-t", f"{session_name}:0.2"])

        # Attach to session
        subprocess.run(["tmux", "attach-session", "-t", session_name])


class ZellijLayoutLauncher:
    """Zellij implementation of layout launcher."""

    ZELLIJ_TIMEOUT_SEC = 5

    def __init__(self):
        self._healthy: bool | None = None

    def is_healthy(self) -> bool:
        if self._healthy is not None:
            return self._healthy
        try:
            subprocess.run(
                ["zellij", "list-sessions", "--short"],
                capture_output=True,
                timeout=self.ZELLIJ_TIMEOUT_SEC,
            )
            self._healthy = True
        except subprocess.TimeoutExpired:
            self._healthy = False
        return self._healthy

    def has_session(self, session_name: str) -> bool:
        try:
            result = subprocess.run(
                ["zellij", "list-sessions", "--short"],
                capture_output=True,
                text=True,
                timeout=self.ZELLIJ_TIMEOUT_SEC,
            )
        except subprocess.TimeoutExpired:
            self._healthy = False
            return False
        if result.returncode != 0:
            return False
        sessions = result.stdout.strip().split("\n")
        return session_name in sessions

    def kill_session(self, session_name: str) -> None:
        try:
            subprocess.run(
                ["zellij", "delete-session", "--force", session_name],
                timeout=self.ZELLIJ_TIMEOUT_SEC,
            )
        except subprocess.TimeoutExpired:
            self._healthy = False

    def attach_session(self, session_name: str) -> None:
        subprocess.run(["zellij", "attach", session_name])

    def launch_layout(
        self,
        session_name: str,
        vault_path: Path | None,
        agent: str,
        cwd: str,
    ) -> None:
        python_exec = sys.executable
        vault_arg = f"--vault {vault_path}" if vault_path else ""

        runs_cmd = f"{python_exec} -m orch_monitor --runs {vault_arg}".strip()
        issues_cmd = f"{python_exec} -m orch_monitor --issues {vault_arg}".strip()

        write_control_prompt(vault_path)
        if agent == "opencode":
            prompt_escaped = CONTROL_PROMPT_INSTRUCTION.replace('"', '\\"')
            agent_cmd = f'opencode --prompt \\"{prompt_escaped}\\"'
        else:
            agent_cmd = agent

        layout_content = f'''
layout {{
    default_tab_template {{
        pane size=1 borderless=true {{
            plugin location="tab-bar"
        }}
        children
        pane size=2 borderless=true {{
            plugin location="status-bar"
        }}
    }}
    tab name="monitor" {{
        pane split_direction="horizontal" {{
            pane size="35%" command="bash" {{
                args "-c" "{runs_cmd}"
            }}
            pane split_direction="vertical" size="65%" {{
                pane size="35%" command="bash" {{
                    args "-c" "{issues_cmd}"
                }}
                pane size="65%" focus=true command="bash" {{
                    args "-c" "{agent_cmd}"
                }}
            }}
        }}
    }}
}}
'''
        # Write temporary layout file
        layout_path = Path(cwd) / ".orch-monitor-layout.kdl"
        layout_path.write_text(layout_content)

        try:
            # Set ORCH_VAULT for child processes
            env = os.environ.copy()
            if vault_path:
                env["ORCH_VAULT"] = str(vault_path)

            # Launch zellij with layout
            # Use --new-session-with-layout to force new session creation
            # (--layout alone would try to add tabs to current session if inside zellij)
            subprocess.run(
                [
                    "zellij",
                    "--session",
                    session_name,
                    "--new-session-with-layout",
                    str(layout_path),
                ],
                cwd=cwd,
                env=env,
            )
        finally:
            # Clean up layout file
            if layout_path.exists():
                layout_path.unlink()


# Registry of layout launchers
_LAUNCHERS: dict[MultiplexerType, LayoutLauncher] = {
    MultiplexerType.TMUX: TmuxLayoutLauncher(),
    MultiplexerType.ZELLIJ: ZellijLayoutLauncher(),
}


def get_layout_launcher(mux_type: MultiplexerType) -> LayoutLauncher:
    """Get layout launcher by multiplexer type (DI factory)."""
    return _LAUNCHERS[mux_type]


def launch_monitor_layout(
    vault_path: Path | None,
    agent: str = "opencode",
    new: bool = False,
    multiplexer: MultiplexerType | None = None,
    log: callable = lambda msg: None,
) -> None:
    log("launch_monitor_layout: start")
    if multiplexer is None:
        log("detecting multiplexer...")
        multiplexer = get_default_multiplexer_type()
        log(f"detected multiplexer: {multiplexer.value}")

    launcher = get_layout_launcher(multiplexer)
    cwd = os.getcwd()
    session_name = get_session_name(vault_path)
    log(f"session_name: {session_name}")

    log(f"checking has_session...")
    if launcher.has_session(session_name):
        log(f"session exists")
        if new:
            log("killing existing session...")
            launcher.kill_session(session_name)
            log("session killed")
        else:
            log("attaching to existing session...")
            launcher.attach_session(session_name)
            return

    if multiplexer == MultiplexerType.ZELLIJ and hasattr(launcher, '_healthy') and launcher._healthy is False:
        print("Error: Zellij is unresponsive (likely stale processes)", file=sys.stderr)
        print("Fix: Run 'pkill -9 zellij' to kill stale processes", file=sys.stderr)
        print("Or use: orch-monitor --new -m tmux", file=sys.stderr)
        sys.exit(1)

    log("launching layout...")
    launcher.launch_layout(session_name, vault_path, agent, cwd)
    log("launch complete")


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
        help="Show runs dashboard only (for multiplexer pane)",
    )
    parser.add_argument(
        "--issues",
        action="store_true",
        help="Show issues dashboard only (for multiplexer pane)",
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
    parser.add_argument(
        "--multiplexer",
        "-m",
        choices=["tmux", "zellij"],
        help="Terminal multiplexer to use (default: auto-detect or tmux)",
    )
    parser.add_argument(
        "--verbose",
        "-v",
        action="store_true",
        help="Enable verbose logging to stderr",
    )

    args = parser.parse_args()
    
    if args.verbose:
        import time as _time
        _start = _time.time()
        def _log(msg: str) -> None:
            elapsed = _time.time() - _start
            print(f"[{elapsed:.2f}s] {msg}", file=sys.stderr)
    else:
        def _log(msg: str) -> None:
            pass
    vault_path = get_vault_path(args)

    _log("ensuring daemon...")
    success, error_msg = ensure_daemon(vault_path)
    if not success:
        print(f"Error: {error_msg}", file=sys.stderr)
        print("Try running 'orch repair' to fix.", file=sys.stderr)
        sys.exit(1)
    _log("daemon ready")

    _log("loading config...")
    config = Config.from_vault(vault_path) if vault_path else Config.load()
    setup_logging(config.log_path)
    _log("config loaded")

    if args.runs:
        app = RunsDashboard(vault_path=vault_path)
        app.run()
    elif args.issues:
        app = IssuesDashboard(vault_path=vault_path)
        app.run()
    else:
        mux_type = None
        if args.multiplexer:
            mux_type = MultiplexerType(args.multiplexer)
        _log("starting launch_monitor_layout")
        launch_monitor_layout(vault_path, args.agent, args.new, mux_type, log=_log)


if __name__ == "__main__":
    main()
