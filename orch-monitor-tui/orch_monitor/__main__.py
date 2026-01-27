"""Entry point for orch monitor TUI."""

import argparse
import logging
import os
import subprocess
import sys
import time
from pathlib import Path
from typing import Protocol

import hy  # noqa: F401 - Enable Hy imports

from .app import IssuesDashboard, OrchMonitorApp, RunsDashboard, setup_logging
from .config import Config
from .proto_client import ProtoDaemonClient
from .multiplexer import (
    InvalidMultiplexerConfigError,
    MultiplexerType,
    get_default_multiplexer_type,
    get_multiplexer,
    validate_multiplexer_config,
)
from .orch_api import OrchAPI, create_orch_api
from .xdg import state_dir

_launcher_logger = logging.getLogger("orch_monitor.launcher")


def _setup_launcher_logging() -> None:
    log_dir = state_dir()
    log_dir.mkdir(parents=True, exist_ok=True)
    log_file = log_dir / "launcher.log"

    file_handler = logging.FileHandler(log_file)
    file_handler.setFormatter(
        logging.Formatter("%(asctime)s %(levelname)s [%(name)s] %(message)s")
    )
    _launcher_logger.addHandler(file_handler)
    _launcher_logger.setLevel(logging.DEBUG)


SESSION_NAME_PREFIX = "orch-monitor"
CONTROL_PROMPT_FILE = "ORCH_CONTROL_PROMPT.md"


MAX_SESSION_NAME_LEN = 28


def get_session_name(vault_path: Path | None = None) -> str:
    base_path = vault_path if vault_path else Path.cwd()
    repo_name = base_path.resolve().name
    safe_name = "".join(c if c.isalnum() or c in "-_" else "-" for c in repo_name)
    full_name = f"{SESSION_NAME_PREFIX}-{safe_name}"
    if len(full_name) > MAX_SESSION_NAME_LEN:
        import hashlib

        hash_suffix = hashlib.md5(safe_name.encode()).hexdigest()[:6]
        prefix_len = MAX_SESSION_NAME_LEN - len(SESSION_NAME_PREFIX) - 1 - 7
        truncated = safe_name[:prefix_len]
        full_name = f"{SESSION_NAME_PREFIX}-{truncated}-{hash_suffix}"
    return full_name


CONTROL_PROMPT_INSTRUCTION = f"ultrathink Please read '{CONTROL_PROMPT_FILE}' in the current directory and follow the instructions found there."


def _get_daemon_client(project_root: Path | None) -> "ProtoDaemonClient | None":
    try:
        config = Config.from_vault(project_root) if project_root else Config.load()
        daemon = ProtoDaemonClient(config.socket_path, config.issues_root)
        if daemon.is_available():
            return daemon
    except Exception as e:
        _launcher_logger.warning(f"Failed to get daemon client: {e}")
    return None


def _get_orch_api(project_root: Path | None) -> OrchAPI | None:
    try:
        config = Config.from_vault(project_root) if project_root else Config.load()
        api = create_orch_api(config.socket_path, config.issues_root)
        if api.is_available():
            return api
    except Exception as e:
        _launcher_logger.warning(f"Failed to create OrchAPI: {e}")
    return None


def load_control_session(project_root: Path | None, agent_type: str = "") -> str | None:
    """Load control session for the given agent type.

    Note: After protobuf migration (orch-368), session management is handled
    internally by get_control_agent_launch. This function is kept for API
    compatibility but always returns None.
    """
    _launcher_logger.debug(
        f"load_control_session called but session management is now internal to daemon"
    )
    return None


def save_control_session(
    project_root: Path | None, session_id: str, agent_type: str = ""
) -> bool:
    """Save control session.

    Note: After protobuf migration (orch-368), session management is handled
    internally by get_control_agent_launch. This function is kept for API
    compatibility but always returns False.
    """
    _launcher_logger.debug(
        f"save_control_session called but session management is now internal to daemon"
    )
    return False


def clear_control_session(project_root: Path | None) -> bool:
    """Clear control session.

    Note: After protobuf migration (orch-368), session management is handled
    internally by get_control_agent_launch. This function is kept for API
    compatibility but always returns False.
    """
    _launcher_logger.debug(
        f"clear_control_session called but session management is now internal to daemon"
    )
    return False


def query_latest_opencode_session(
    project_root: Path | None, server_url: str = "http://localhost:4096"
) -> str | None:
    import json
    import urllib.request

    directory = (
        str(project_root.resolve()) if project_root else str(Path.cwd().resolve())
    )
    one_day_ago_ms = int((time.time() - 86400) * 1000)

    try:
        url = f"{server_url}/session?start={one_day_ago_ms}"
        with urllib.request.urlopen(url, timeout=5) as response:
            sessions = json.loads(response.read().decode())

        matching = [
            s
            for s in sessions
            if s.get("directory") == directory and s.get("parentID") is None
        ]

        if not matching:
            _launcher_logger.warning(f"No sessions found for directory: {directory}")
            return None

        matching.sort(key=lambda s: s.get("time", {}).get("updated", 0), reverse=True)
        session_id = matching[0]["id"]
        _launcher_logger.info(f"Found latest session for {directory}: {session_id}")
        return session_id
    except Exception as e:
        _launcher_logger.error(f"Failed to query opencode sessions: {e}")
        return None


def get_issues_root(args) -> Path | None:
    if getattr(args, "issues_root", None):
        return args.issues_root
    if getattr(args, "vault", None):
        return args.vault
    env_issues_root = os.getenv("ORCH_ISSUES_ROOT") or os.getenv("ORCH_VAULT")
    if env_issues_root:
        return Path(env_issues_root)
    return None


def get_project_root(args) -> Path | None:
    """Get project root from args or environment."""
    if hasattr(args, "project_root") and args.project_root:
        return args.project_root
    project_root_env = os.getenv("ORCH_PROJECT_ROOT")
    if project_root_env:
        return Path(project_root_env)
    return None


DAEMON_STARTUP_TIMEOUT_SEC = 15
DAEMON_POLL_INTERVAL_SEC = 0.2


def ensure_daemon(
    project_root: Path | None, issues_root: Path | None
) -> tuple[bool, str]:
    if project_root:
        config = Config.from_project_root(project_root)
        if issues_root:
            config.issues_root = issues_root
    elif issues_root:
        config = Config.from_issues_root(issues_root)
    else:
        config = Config.load()
    daemon = ProtoDaemonClient(config.socket_path, config.issues_root)
    socket_path = config.socket_path

    if daemon.is_available():
        return True, ""

    print(f"Daemon socket not found at {socket_path}", file=sys.stderr)
    print("Starting orch daemon...", file=sys.stderr)

    project_root_arg = ["--project-root", str(project_root)] if project_root else []
    issues_root_arg = ["--issues-root", str(issues_root)] if issues_root else []
    try:
        result = subprocess.run(
            ["orch"] + project_root_arg + issues_root_arg + ["daemon", "start"],
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
        project_root: Path | None,
        vault_path: Path | None,
        agent: str,
        cwd: str,
        new_control_agent: bool = False,
    ) -> None:
        """Launch a new layout with runs, issues, and agent panes.

        Args:
            session_name: Name of the multiplexer session
            project_root: Path to project root
            vault_path: Path to issues root
            agent: Agent command to use
            cwd: Working directory
            new_control_agent: If True, start fresh control agent session.
                              If False, resume existing session using explicit session ID.
        """
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
        project_root: Path | None,
        vault_path: Path | None,
        agent: str,
        cwd: str,
        new_control_agent: bool = False,
    ) -> None:
        python_exec = sys.executable
        orch_args = ""
        if project_root:
            orch_args += f"--project-root {project_root} "
        if vault_path:
            orch_args += f"--vault {vault_path} "
        orch_args = orch_args.strip()

        env_export = ""
        if project_root:
            env_export += f"export ORCH_PROJECT_ROOT='{project_root}'; "
        if vault_path:
            env_export += f"export ORCH_VAULT='{vault_path}'; "

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
            f'{env_export}"{python_exec}" -m orch_monitor --runs {orch_args}'.strip()
        )
        issues_cmd = (
            f'{env_export}"{python_exec}" -m orch_monitor --issues {orch_args}'.strip()
        )

        # Use daemon API to get control agent launch command
        # The daemon handles: writing control prompt file, resolving agent config,
        # session management, and building the command
        agent_cmd = agent  # fallback to raw agent command
        need_capture_session = False

        daemon = _get_daemon_client(project_root)
        if daemon:
            project_str = str(project_root) if project_root else str(Path.cwd())
            ok, command, prompt_file, port, session_id, resolved_agent, err = (
                daemon.get_control_agent_launch(
                    project_str, agent_type=agent, new_session=new_control_agent
                )
            )
            if ok and command:
                agent_cmd = command
                _launcher_logger.info(
                    f"Using daemon launch: agent={resolved_agent}, command={command}, "
                    f"port={port}, session={session_id}"
                )
            else:
                _launcher_logger.warning(
                    f"Failed to get control agent launch from daemon: {err}"
                )
                # Fall back to simple command
                if agent in ("opencode", "claude", "codex", "gemini"):
                    agent_cmd = f'{agent} --prompt "{CONTROL_PROMPT_INSTRUCTION}"'
                    need_capture_session = True
        else:
            _launcher_logger.warning(
                "Daemon not available, using fallback agent command"
            )
            if agent in ("opencode", "claude", "codex", "gemini"):
                agent_cmd = f'{agent} --prompt "{CONTROL_PROMPT_INSTRUCTION}"'
                need_capture_session = True

        subprocess.run(
            ["tmux", "send-keys", "-t", f"{session_name}:0.0", runs_cmd, "Enter"]
        )
        subprocess.run(
            ["tmux", "send-keys", "-t", f"{session_name}:0.1", issues_cmd, "Enter"]
        )
        subprocess.run(
            ["tmux", "send-keys", "-t", f"{session_name}:0.2", agent_cmd, "Enter"]
        )

        # Capture session for fallback cases
        if need_capture_session:
            _launcher_logger.info(f"Waiting to capture {agent} session ID...")
            time.sleep(3)
            if agent == "opencode":
                session_id = query_latest_opencode_session(project_root)
                if session_id:
                    save_control_session(
                        project_root, session_id, agent_type="opencode"
                    )
            elif agent == "claude":
                session_id = load_control_session(project_root, agent_type="claude")
                if session_id:
                    _launcher_logger.info(f"Captured Claude session: {session_id}")

        subprocess.run(["tmux", "select-pane", "-t", f"{session_name}:0.2"])

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
        project_root: Path | None,
        vault_path: Path | None,
        agent: str,
        cwd: str,
        new_control_agent: bool = False,
    ) -> None:
        python_exec = sys.executable
        orch_args = ""
        if project_root:
            orch_args += f"--project-root {project_root} "
        if vault_path:
            orch_args += f"--vault {vault_path} "
        orch_args = orch_args.strip()

        runs_cmd = f"{python_exec} -m orch_monitor --runs {orch_args}".strip()
        issues_cmd = f"{python_exec} -m orch_monitor --issues {orch_args}".strip()

        # Use daemon API to get control agent launch command
        # The daemon handles: writing control prompt file, resolving agent config,
        # session management, and building the command
        agent_cmd = agent  # fallback to raw agent command
        need_capture_session = False

        daemon = _get_daemon_client(project_root)
        if daemon:
            project_str = str(project_root) if project_root else str(Path.cwd())
            ok, command, prompt_file, port, session_id, resolved_agent, err = (
                daemon.get_control_agent_launch(
                    project_str, agent_type=agent, new_session=new_control_agent
                )
            )
            if ok and command:
                agent_cmd = command
                _launcher_logger.info(
                    f"Using daemon launch: agent={resolved_agent}, command={command}, "
                    f"port={port}, session={session_id}"
                )
            else:
                _launcher_logger.warning(
                    f"Failed to get control agent launch from daemon: {err}"
                )
                # Fall back to simple command with escaped prompt for zellij
                prompt_escaped = CONTROL_PROMPT_INSTRUCTION.replace('"', '\\"')
                if agent in ("opencode", "claude", "codex", "gemini"):
                    agent_cmd = f'{agent} --prompt \\"{prompt_escaped}\\"'
                    need_capture_session = True
        else:
            _launcher_logger.warning(
                "Daemon not available, using fallback agent command"
            )
            prompt_escaped = CONTROL_PROMPT_INSTRUCTION.replace('"', '\\"')
            if agent in ("opencode", "claude", "codex", "gemini"):
                agent_cmd = f'{agent} --prompt \\"{prompt_escaped}\\"'
                need_capture_session = True

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
        import atexit
        import tempfile

        layout_fd, layout_path_str = tempfile.mkstemp(
            suffix=".kdl", prefix="orch-monitor-layout-"
        )
        layout_path = Path(layout_path_str)
        os.write(layout_fd, layout_content.encode())
        os.close(layout_fd)
        _launcher_logger.info(f"wrote layout to {layout_path}")

        atexit.register(lambda: layout_path.unlink(missing_ok=True))

        env = os.environ.copy()
        if project_root:
            env["ORCH_PROJECT_ROOT"] = str(project_root)
        if vault_path:
            env["ORCH_VAULT"] = str(vault_path)

        inside_zellij = os.environ.get("ZELLIJ_SESSION_NAME") is not None
        for var in ("ZELLIJ", "ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID"):
            removed = env.pop(var, None)
            if removed is not None:
                _launcher_logger.info(f"cleared env var {var}={removed}")

        cmd_str = (
            f"zellij --session {session_name} --new-session-with-layout {layout_path}"
        )
        _launcher_logger.info(f"launching: {cmd_str}, inside_zellij={inside_zellij}")

        for handler in _launcher_logger.handlers[:]:
            handler.flush()
            handler.close()
            _launcher_logger.removeHandler(handler)

        sys.stdout.flush()
        sys.stderr.flush()

        os.chdir(cwd)
        os.environ.clear()
        os.environ.update(env)

        if inside_zellij:
            devnull_r = open("/dev/null", "r")
            devnull_w = open("/dev/null", "w")
            subprocess.Popen(
                [
                    "nohup",
                    "zellij",
                    "--session",
                    session_name,
                    "--new-session-with-layout",
                    str(layout_path),
                ],
                stdin=devnull_r,
                stdout=devnull_w,
                stderr=devnull_w,
            )
            for _ in range(20):
                time.sleep(0.25)
                result = subprocess.run(
                    ["zellij", "list-sessions", "--short"],
                    capture_output=True,
                    text=True,
                )
                if session_name in result.stdout.split("\n"):
                    break

            # Capture session for fallback cases
            if need_capture_session:
                time.sleep(2)
                if agent == "opencode":
                    session_id = query_latest_opencode_session(project_root)
                    if session_id:
                        save_control_session(
                            project_root, session_id, agent_type="opencode"
                        )
                elif agent == "claude":
                    session_id = load_control_session(project_root, agent_type="claude")
                    if session_id:
                        _launcher_logger.info(f"Captured Claude session: {session_id}")

            os.execvp("zellij", ["zellij", "attach", session_name])
        else:
            os.execvp(
                "zellij",
                [
                    "zellij",
                    "--session",
                    session_name,
                    "--new-session-with-layout",
                    str(layout_path),
                ],
            )


# Registry of layout launchers
_LAUNCHERS: dict[MultiplexerType, LayoutLauncher] = {
    MultiplexerType.TMUX: TmuxLayoutLauncher(),
    MultiplexerType.ZELLIJ: ZellijLayoutLauncher(),
}


def get_layout_launcher(mux_type: MultiplexerType) -> LayoutLauncher:
    """Get layout launcher by multiplexer type (DI factory)."""
    return _LAUNCHERS[mux_type]


def launch_monitor_layout(
    project_root: Path | None,
    vault_path: Path | None,
    agent: str = "opencode",
    new: bool = False,
    new_control_agent: bool = False,
    multiplexer: MultiplexerType | None = None,
    log: callable = lambda msg: None,
) -> None:
    """Launch the orch-monitor layout in a terminal multiplexer.

    Args:
        project_root: Path to project root
        vault_path: Path to issues root
        agent: Agent command to use (default: opencode)
        new: If True, restart the layout (kill existing session)
        new_control_agent: If True, also restart the control agent session.
                          If False, preserve control agent session on layout restart.
        multiplexer: Which multiplexer to use (auto-detected if None)
        log: Logging function
    """
    _setup_launcher_logging()
    _launcher_logger.info(
        f"launch_monitor_layout: project_root={project_root}, vault_path={vault_path}, new={new}, new_control_agent={new_control_agent}"
    )

    if multiplexer is None:
        _launcher_logger.info("detecting multiplexer...")
        multiplexer = get_default_multiplexer_type()
        _launcher_logger.info(f"detected multiplexer: {multiplexer.value}")

    # Validate multiplexer config - reject zellij monitor + zellij agents
    try:
        validate_multiplexer_config(multiplexer)
    except InvalidMultiplexerConfigError as e:
        _launcher_logger.error(str(e))
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

    launcher = get_layout_launcher(multiplexer)
    cwd = os.getcwd()
    session_name = get_session_name(project_root or vault_path)
    _launcher_logger.info(f"session_name={session_name}, cwd={cwd}")

    _launcher_logger.info("checking has_session...")
    if launcher.has_session(session_name):
        _launcher_logger.info("session exists")
        if new or new_control_agent:
            _launcher_logger.info("killing existing session...")
            launcher.kill_session(session_name)
            _launcher_logger.info("session killed")
        else:
            _launcher_logger.info("attaching to existing session...")
            launcher.attach_session(session_name)
            return

    if (
        multiplexer == MultiplexerType.ZELLIJ
        and hasattr(launcher, "_healthy")
        and launcher._healthy is False
    ):
        _launcher_logger.error("Zellij is unresponsive (likely stale processes)")
        _launcher_logger.error("Fix: Run 'pkill -9 zellij' to kill stale processes")
        _launcher_logger.error("Or use: orch-monitor --new -m tmux")
        sys.exit(1)

    _launcher_logger.info("launching layout...")
    launcher.launch_layout(
        session_name, project_root, vault_path, agent, cwd, new_control_agent
    )
    _launcher_logger.info("launch complete")


def main():
    parser = argparse.ArgumentParser(description="Orch monitor TUI")
    parser.add_argument(
        "--issues-root",
        type=Path,
        dest="issues_root",
        help="Path to issues root directory for file-based issues",
    )
    parser.add_argument(
        "--vault",
        type=Path,
        help="(Deprecated, use --issues-root) Path to issues root directory",
    )
    parser.add_argument(
        "--project-root",
        type=Path,
        help="Path to project root (where .orch/ lives)",
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
        default=None,
        help="Control agent command (default: from config, or 'claude')",
    )
    parser.add_argument(
        "--new",
        action="store_true",
        help="Restart layout only, preserving control agent session",
    )
    parser.add_argument(
        "--new-control-agent",
        action="store_true",
        dest="new_control_agent",
        help="Also restart control agent session (implies --new for layout)",
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

    vault_path = get_issues_root(args)
    project_root = get_project_root(args)

    # Check configuration state before daemon - show onboarding if unconfigured
    from .config import detect_configuration_state

    config_state = detect_configuration_state()

    # Show onboarding if unconfigured (only for full layout mode, not single panes)
    if not args.runs and not args.issues:
        if not config_state.has_orch_dir or not config_state.has_issues_path:
            from .app import OnboardingApp

            _log("showing onboarding screen - config not found")
            app = OnboardingApp(config_state)
            result = app.run()
            if not result:
                print("Setup cancelled. Run 'orch tutorial' for help.", file=sys.stderr)
                sys.exit(0)
            # Refresh paths after onboarding
            config_state = detect_configuration_state()
            if config_state.issues_path:
                vault_path = config_state.issues_path

    _log("ensuring daemon...")
    success, error_msg = ensure_daemon(project_root, vault_path)
    if not success:
        print(f"Error: {error_msg}", file=sys.stderr)
        print("Try running 'orch repair' to fix.", file=sys.stderr)
        sys.exit(1)
    _log("daemon ready")

    _log("loading config...")
    if project_root:
        config = Config.from_project_root(project_root)
        if vault_path:
            config.issues_root = vault_path
    elif vault_path:
        config = Config.from_issues_root(vault_path)
    else:
        config = Config.load()
    setup_logging(config.log_path)
    _log("config loaded")

    if args.runs:
        app = RunsDashboard(issues_root=vault_path)
        app.run()
    elif args.issues:
        app = IssuesDashboard(issues_root=vault_path)
        app.run()
    else:
        mux_type = None
        if args.multiplexer:
            mux_type = MultiplexerType(args.multiplexer)
        _log("starting launch_monitor_layout")
        # --new-control-agent implies --new for layout restart
        new = args.new or args.new_control_agent
        # Use config.agent as default if --agent not specified
        agent = args.agent if args.agent is not None else config.agent
        _log(f"using agent: {agent}")
        launch_monitor_layout(
            project_root,
            vault_path,
            agent,
            new,
            args.new_control_agent,
            mux_type,
            log=_log,
        )


if __name__ == "__main__":
    main()
