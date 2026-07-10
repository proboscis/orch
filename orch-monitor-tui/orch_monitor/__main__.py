"""Entry point for orch monitor TUI."""

import argparse
import json
import logging
import os
import shlex
import shutil
import subprocess
import sys
import time
from contextlib import contextmanager
from pathlib import Path
from typing import Protocol

import hy  # noqa: F401 - Enable Hy imports
from rich.console import Console
from rich.markup import escape as rich_escape

# Shared console for startup output
_console = Console(stderr=True)


@contextmanager
def _spinner_context(message: str, enabled: bool = True):
    """Create a Rich spinner context if enabled, otherwise a no-op context.

    Usage:
        with _spinner_context("Loading...", enabled=True) as status:
            do_slow_thing()
            status.update("Still loading...")
    """
    if enabled:
        with _console.status(message, spinner="dots") as status:
            yield status
    else:

        class NoOpStatus:
            def update(self, msg: str, spinner: str | None = None) -> None:
                pass

        yield NoOpStatus()


from .app import IssuesDashboard, OrchMonitorApp, RunsDashboard, setup_logging
from .client_bootstrap import (
    ClientBootstrap,
    ClientBootstrapError,
    load_client_bootstrap,
)
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


def _escape_kdl_string(s: str) -> str:
    """Escape a string for use inside KDL double-quoted string literals.

    KDL strings require escaping of backslashes and double quotes.
    See: https://kdl.dev/#string
    """
    # First escape backslashes, then double quotes
    return s.replace("\\", "\\\\").replace('"', '\\"')


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


CONTROL_PROMPT_FILE = "ORCH_CONTROL_PROMPT.md"


# Real OS limit for sockaddr_un.sun_path: macOS = 104, Linux = 108. zellij validates
# its --session name against 108, but on macOS the zellij *server* still panics with
# "local socket name length exceeds capacity of sun_path" when the actual socket path
# exceeds 104. Use the smaller value so generated names are safe on both platforms.
ZELLIJ_SOCK_MAX_LENGTH = 104
# zellij appends this contract dir to the socket base (CLIENT_SERVER_CONTRACT_DIR =
# "contract_version_<N>"); 18 chars on current releases.
ZELLIJ_CONTRACT_DIR = "contract_version_1"
def _ensure_short_zellij_socket_dir() -> str:
    """Point zellij at a short, stable per-user socket dir (macOS sun_path workaround).

    The default <tempdir>/zellij-<uid> path on macOS — a long /var/folders TMPDIR plus
    a (potentially 10-digit) uid and the 18-char contract subdir — can leave too few
    characters for ANY session name, making the zellij server panic on socket creation.
    A short ZELLIJ_SOCKET_DIR reclaims that budget. Respects an existing value if the
    user already set one. Sets os.environ so every child zellij invocation (new session,
    list, attach, kill) agrees on the same dir. Returns the base (no contract subdir).
    """
    existing = os.environ.get("ZELLIJ_SOCKET_DIR")
    if existing:
        return existing
    try:
        uid = os.getuid()
    except AttributeError:  # non-Unix
        uid = 0
    short = f"/tmp/zlj-{uid}"
    os.environ["ZELLIJ_SOCKET_DIR"] = short
    return short


CONTROL_PROMPT_INSTRUCTION = f"ultrathink Please read '{CONTROL_PROMPT_FILE}' in the current directory and follow the instructions found there."


def _build_fallback_control_agent_command(agent: str) -> str:
    """Build a local fallback command when daemon launch API is unavailable."""
    prompt = shlex.quote(CONTROL_PROMPT_INSTRUCTION)
    if agent == "opencode":
        return f"opencode --prompt {prompt}"
    if agent == "claude":
        return f"claude --dangerously-skip-permissions {prompt}"
    if agent == "codex":
        return f"codex --yolo {prompt}"
    if agent == "gemini":
        return f"gemini --yolo --prompt-interactive {prompt}"
    return agent


def _normalize_codex_model(model: str) -> str:
    model = model.strip()
    if not model:
        return ""
    if "/" in model:
        return model.split("/", 1)[1].strip()
    return model


class ControlAgentLaunchError(Exception):
    """Raised when the daemon refuses to launch the control agent (policy).

    The daemon is the source of truth for the codex profile's allowed_targets
    constraint, enforced against THIS client host (sent as client_host on the
    config RPC — the control agent executes here, not on the daemon host).
    When this host is not allowed for the resolved profile, the daemon fails
    fast and the TUI must surface that error instead of silently launching a
    fallback command on the wrong host.
    """


# Stable marker present in the daemon's codex-profile host-constraint error. Used
# to distinguish an authoritative policy denial (must surface, never launch) from
# a generic/transient config RPC failure (tolerated → local fallback).
_CODEX_PROFILE_DENIAL_MARKER = "may only run on targets"


def _is_codex_profile_denial(message: str) -> bool:
    return _CODEX_PROFILE_DENIAL_MARKER in (message or "")


def _codex_home_env_prefix(codex_home: str) -> str:
    """Return a shell `export CODEX_HOME=...; ` prefix, or "" when unset.

    The daemon returns codex_home VERBATIM (no ~ expansion on the daemon host,
    which may be a remote master with a different HOME). A leading ~ is
    expanded HERE, against the local HOME of the host that executes the
    control agent. This mirrors the Go `orch agent` path, which expands ~ in
    LaunchConfig.CodexHomeEnv on the executing host.
    """
    codex_home = (codex_home or "").strip()
    if not codex_home:
        return ""
    codex_home = str(Path(codex_home).expanduser())
    return f"export CODEX_HOME={shlex.quote(codex_home)}; "


def _build_local_control_agent_command(
    agent: str,
    model: str = "",
    model_variant: str = "",
    extra_args: list[str] | None = None,
    codex_home: str = "",
) -> str:
    prompt = CONTROL_PROMPT_INSTRUCTION
    args: list[str] = []
    extras = [a for a in (extra_args or []) if a]
    env_prefix = _codex_home_env_prefix(codex_home)

    if agent == "opencode":
        args = ["opencode"]
        if extras:
            args.extend(extras)
        args.extend(["--prompt", prompt])
        if model:
            args.extend(["--model", model])
        if model_variant:
            args.extend(["--model-variant", model_variant])
        return env_prefix + shlex.join(args)

    if agent == "claude":
        args = ["claude"]
        if extras:
            args.extend(extras)
        else:
            args.append("--dangerously-skip-permissions")
        args.append(prompt)
        return env_prefix + shlex.join(args)

    if agent == "codex":
        args = ["codex"]
        if extras:
            args.extend(extras)
        else:
            args.append("--yolo")
        normalized_model = _normalize_codex_model(model)
        if normalized_model:
            args.extend(["--model", normalized_model])
        args.append(prompt)
        return env_prefix + shlex.join(args)

    if agent == "gemini":
        args = ["gemini"]
        if extras:
            args.extend(extras)
        else:
            args.append("--yolo")
        args.extend(["--prompt-interactive", prompt])
        return env_prefix + shlex.join(args)

    return env_prefix + _build_fallback_control_agent_command(agent)


def _get_daemon_client(project_root: Path | None) -> "ProtoDaemonClient | None":
    try:
        config = (
            Config.from_project_root(project_root) if project_root else Config.load()
        )
        bootstrap = load_client_bootstrap()
        daemon = ProtoDaemonClient(
            config.socket_path,
            config.project_root,
            bootstrap.remote_addr,
            project_id=bootstrap.project_id,
        )
        if daemon.is_available():
            return daemon
    except ClientBootstrapError:
        raise
    except Exception as e:
        _launcher_logger.warning(f"Failed to get daemon client: {e}")
    return None


def _get_orch_api(project_root: Path | None) -> OrchAPI | None:
    try:
        config = (
            Config.from_project_root(project_root) if project_root else Config.load()
        )
        api = create_orch_api(config.socket_path, config.project_root)
        if api.is_available():
            return api
    except ClientBootstrapError:
        raise
    except Exception as e:
        _launcher_logger.warning(f"Failed to create OrchAPI: {e}")
    return None


def load_control_session(project_root: Path | None, agent_type: str = "") -> str | None:
    """Load control session for the given agent type."""
    root = (project_root or Path.cwd()).resolve()
    session_path = root / ".orch" / "control-session.json"
    if not session_path.exists():
        return None
    try:
        data = json.loads(session_path.read_text())
    except Exception as e:
        _launcher_logger.warning(f"Failed reading control session file: {e}")
        return None

    session_id = str(data.get("session_id", "")).strip()
    stored_agent = str(data.get("agent_type", "")).strip()
    if not session_id:
        return None
    if agent_type and stored_agent and stored_agent != agent_type:
        return None
    return session_id


def save_control_session(
    project_root: Path | None, session_id: str, agent_type: str = ""
) -> bool:
    """Save control session."""
    root = (project_root or Path.cwd()).resolve()
    session_path = root / ".orch" / "control-session.json"
    try:
        session_path.parent.mkdir(parents=True, exist_ok=True)
        payload = {
            "session_id": session_id,
            "agent_type": agent_type,
        }
        session_path.write_text(json.dumps(payload, indent=2) + "\n")
        return True
    except Exception as e:
        _launcher_logger.warning(f"Failed saving control session file: {e}")
        return False


def clear_control_session(project_root: Path | None) -> bool:
    """Clear control session."""
    root = (project_root or Path.cwd()).resolve()
    session_path = root / ".orch" / "control-session.json"
    try:
        session_path.unlink(missing_ok=True)
        return True
    except Exception as e:
        _launcher_logger.warning(f"Failed clearing control session file: {e}")
        return False


def _resolve_local_control_agent_command(
    daemon: "ProtoDaemonClient | None",
    project_root: Path | None,
    cwd: str,
    fallback_agent: str,
    agent_override: str = "",
) -> tuple[str, str, bool]:
    """Resolve control agent command from config RPC and build command locally."""
    fallback_cmd = _build_fallback_control_agent_command(fallback_agent)
    if not daemon:
        _launcher_logger.warning(
            "Daemon not available, using fallback local control agent command"
        )
        return fallback_cmd, fallback_agent, True

    from returns.result import Failure

    project_str = str(project_root) if project_root else str(Path.cwd())
    config_result = daemon.get_control_agent_config(project_str)
    if isinstance(config_result, Failure):
        failure_msg = str(config_result.failure())
        # The daemon enforces the codex profile's allowed_targets against THIS
        # client host (the control agent executes here). A policy denial (e.g.
        # a company control agent on a disallowed host) must be surfaced and
        # must NOT fall back to launching on the wrong host.
        # Generic/transient RPC failures remain fallback-eligible for resilience.
        if _is_codex_profile_denial(failure_msg):
            raise ControlAgentLaunchError(failure_msg)
        _launcher_logger.warning(
            f"Failed to get control agent config from daemon: {failure_msg}"
        )
        return fallback_cmd, fallback_agent, True

    cfg = config_result.unwrap()
    if cfg.prompt_content:
        prompt_path = Path(cwd) / CONTROL_PROMPT_FILE
        try:
            prompt_path.write_text(cfg.prompt_content)
        except Exception as prompt_err:
            _launcher_logger.warning(
                f"Failed to write local control prompt file: {prompt_err}"
            )

    resolved_agent = (
        agent_override.strip() or cfg.agent.strip() or fallback_agent
    ).strip()
    command = _build_local_control_agent_command(
        agent=resolved_agent,
        model=cfg.model,
        model_variant=cfg.model_variant,
        extra_args=list(cfg.extra_args or []),
        codex_home=getattr(cfg, "codex_home", "") or "",
    )
    _launcher_logger.info(
        "Using local control launch from config: "
        f"agent={resolved_agent}, model={cfg.model}, variant={cfg.model_variant}, "
        f"extra_args={len(cfg.extra_args or [])}, codex_home={'set' if getattr(cfg, 'codex_home', '') else 'unset'}"
    )
    return command, resolved_agent, False


def get_project_root(bootstrap: ClientBootstrap) -> Path | None:
    return bootstrap.project_root


def get_project_scope(bootstrap: ClientBootstrap) -> str | None:
    return bootstrap.project_id or None


DAEMON_STARTUP_TIMEOUT_SEC = 15
DAEMON_POLL_INTERVAL_SEC = 0.2
DAEMON_START_COMMAND_TIMEOUT_SEC = 10
DAEMON_REPAIR_TIMEOUT_SEC = 60


def _resolve_orch_binary() -> str:
    env_orch = os.getenv("ORCH_BIN")
    if env_orch:
        return env_orch

    local_orch = Path.home() / ".local" / "bin" / "orch"
    if local_orch.exists() and os.access(local_orch, os.X_OK):
        return str(local_orch)

    found = shutil.which("orch")
    if found:
        return found

    return "orch"


def _orch_scope_args(project_scope: str | None) -> list[str]:
    args: list[str] = []
    if project_scope:
        args.extend(["--project", project_scope])
    return args


def _start_daemon_process(scope_args: list[str]) -> tuple[bool, str]:
    orch_bin = _resolve_orch_binary()
    try:
        result = subprocess.run(
            [orch_bin] + scope_args + ["daemon", "start"],
            capture_output=True,
            text=True,
            timeout=DAEMON_START_COMMAND_TIMEOUT_SEC,
        )
        if result.returncode != 0:
            error = result.stderr.strip() or result.stdout.strip()
            return False, error or "daemon start failed"
        return True, ""
    except subprocess.TimeoutExpired:
        return False, "daemon start command timed out"
    except FileNotFoundError:
        return False, "'orch' command not found. Is it installed?"


def _run_auto_repair(scope_args: list[str]) -> tuple[bool, str]:
    orch_bin = _resolve_orch_binary()
    try:
        result = subprocess.run(
            [orch_bin] + scope_args + ["repair", "--force"],
            capture_output=True,
            text=True,
            timeout=DAEMON_REPAIR_TIMEOUT_SEC,
        )
    except subprocess.TimeoutExpired:
        return False, "auto-repair command timed out"
    except FileNotFoundError:
        return False, "'orch' command not found. Is it installed?"

    # `orch repair` exits non-zero when it fixes problems; treat that as success.
    if result.returncode in (0, 1):
        return True, ""

    error = result.stderr.strip() or result.stdout.strip()
    return False, error or "auto-repair failed"


def _wait_for_daemon(daemon: ProtoDaemonClient) -> bool:
    polls_per_second = int(1 / DAEMON_POLL_INTERVAL_SEC)
    total_polls = DAEMON_STARTUP_TIMEOUT_SEC * polls_per_second
    for poll_count in range(total_polls):
        time.sleep(DAEMON_POLL_INTERVAL_SEC)
        if daemon.is_available():
            return True
        elapsed_seconds = (poll_count + 1) // polls_per_second
        if (poll_count + 1) % polls_per_second == 0:
            print(f"  Waiting for daemon... ({elapsed_seconds}s)", file=sys.stderr)
    return False


def ensure_daemon(
    project_root: Path | None,
    project_scope: str | None = None,
    bootstrap: ClientBootstrap | None = None,
) -> tuple[bool, str]:
    if project_root:
        config = Config.from_project_root(project_root)
    else:
        config = Config.load()

    bootstrap = bootstrap or load_client_bootstrap()
    remote_addr = bootstrap.remote_addr
    if remote_addr:
        # Remote master: connect over TCP; never start a local daemon.
        daemon = ProtoDaemonClient(config.socket_path, config.project_root, remote_addr)
        if daemon.is_available():
            return True, ""
        return False, (
            f"Remote orch daemon not reachable at {remote_addr} "
            "(from --remote/ORCH_REMOTE/client.yaml remote.default)"
        )

    daemon = ProtoDaemonClient(config.socket_path, config.project_root)
    socket_path = config.socket_path

    if daemon.is_available():
        return True, ""

    print(f"Daemon socket not found at {socket_path}", file=sys.stderr)
    print("Starting orch daemon...", file=sys.stderr)

    scope_args = _orch_scope_args(project_scope)

    started, start_error = _start_daemon_process(scope_args)
    if started and _wait_for_daemon(daemon):
        print("Daemon started.", file=sys.stderr)
        return True, ""

    if not started:
        print(f"Daemon start failed: {start_error}", file=sys.stderr)

    print(
        "Daemon startup stalled. Running automatic repair (orch repair --force)...",
        file=sys.stderr,
    )
    repaired, repair_error = _run_auto_repair(scope_args)
    if not repaired:
        return (
            False,
            "Automatic daemon repair failed: "
            + repair_error
            + f". Socket: {socket_path}",
        )

    if daemon.is_available():
        print("Daemon became available after repair.", file=sys.stderr)
        return True, ""

    print("Retrying orch daemon start after repair...", file=sys.stderr)
    started_after_repair, retry_error = _start_daemon_process(scope_args)
    if not started_after_repair:
        if daemon.is_available():
            print("Daemon became available after repair.", file=sys.stderr)
            return True, ""
        return (
            False,
            "Failed to start daemon after auto-repair: "
            + retry_error
            + f". Socket: {socket_path}",
        )

    if _wait_for_daemon(daemon):
        print("Daemon started after repair.", file=sys.stderr)
        return True, ""

    return (
        False,
        f"Daemon did not become available within {DAEMON_STARTUP_TIMEOUT_SEC}s after auto-repair. Socket: {socket_path}",
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
        agent_override: str = "",
        project_scope: str | None = None,
    ) -> None:
        """Launch a new layout with runs, issues, and agent panes.

        Args:
            session_name: Name of the multiplexer session
            project_root: Path to project root
            vault_path: Deprecated and unused issues root path
            agent: Fallback agent command to use
            cwd: Working directory
            new_control_agent: If True, start fresh control agent session.
                              If False, resume existing session using explicit session ID.
            agent_override: Explicit agent override for daemon config resolution.
                            Empty string means "let daemon choose from config".
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
        agent_override: str = "",
        project_scope: str | None = None,
    ) -> None:
        python_exec = sys.executable
        orch_args = ""
        if project_scope:
            orch_args += f"--project {shlex.quote(project_scope)} "
        orch_args = orch_args.strip()

        env_export = ""
        if project_scope:
            env_export += f"export ORCH_PROJECT={shlex.quote(project_scope)}; "
        remote_env = os.environ.get("ORCH_REMOTE")
        if remote_env is not None:
            env_export += f"export ORCH_REMOTE={shlex.quote(remote_env)}; "

        # Resolve control config from daemon BEFORE creating any tmux panes, so a
        # policy denial (codex profile allowed_targets) fails fast without leaving
        # a half-built session. May raise ControlAgentLaunchError.
        agent_cmd = agent
        need_capture_session = False

        daemon = _get_daemon_client(project_root)
        agent_cmd, resolved_agent, used_fallback = _resolve_local_control_agent_command(
            daemon=daemon,
            project_root=project_root,
            cwd=cwd,
            fallback_agent=agent,
            agent_override=agent_override,
        )
        if used_fallback and agent_cmd != agent:
            need_capture_session = True

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
            _launcher_logger.info(f"Waiting to capture {resolved_agent} session ID...")
            time.sleep(3)
            if resolved_agent == "opencode":
                session_id = query_latest_opencode_session(project_root)
                if session_id:
                    save_control_session(
                        project_root, session_id, agent_type="opencode"
                    )
            elif resolved_agent == "claude":
                session_id = load_control_session(project_root, agent_type="claude")
                if session_id:
                    _launcher_logger.info(f"Captured Claude session: {session_id}")

        subprocess.run(["tmux", "select-pane", "-t", f"{session_name}:0.2"])

        subprocess.run(["tmux", "attach-session", "-t", session_name])


class ZellijLayoutLauncher:
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
        # Count only LIVE sessions. zellij marks serialized/dead sessions as
        # "EXITED - attach to resurrect"; `--short` lists them indistinguishably
        # from live ones. If we treated a resurrectable session as present we would
        # attach and zellij would RESURRECT command panes in a held
        # ("Waiting to run") state instead of running the layout fresh.
        import re

        try:
            result = subprocess.run(
                ["zellij", "list-sessions"],
                capture_output=True,
                text=True,
                timeout=self.ZELLIJ_TIMEOUT_SEC,
            )
        except subprocess.TimeoutExpired:
            self._healthy = False
            return False
        if result.returncode != 0:
            return False
        ansi = re.compile(r"\x1b\[[0-9;]*m")
        for raw in result.stdout.splitlines():
            line = ansi.sub("", raw).strip()
            if not line:
                continue
            name = line.split()[0]
            if name == session_name and "EXITED" not in line:
                return True
        return False

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
        agent_override: str = "",
        project_scope: str | None = None,
    ) -> None:
        python_exec = sys.executable
        orch_args = ""
        if project_scope:
            orch_args += f"--project {shlex.quote(project_scope)} "
        orch_args = orch_args.strip()

        runs_cmd = f"{python_exec} -m orch_monitor --runs {orch_args}".strip()
        issues_cmd = f"{python_exec} -m orch_monitor --issues {orch_args}".strip()

        daemon = _get_daemon_client(project_root)
        agent_cmd, _resolved_agent, _used_fallback = _resolve_local_control_agent_command(
            daemon=daemon,
            project_root=project_root,
            cwd=cwd,
            fallback_agent=agent,
            agent_override=agent_override,
        )

        # Escape commands for KDL string literals (backslashes and double quotes)
        runs_cmd_escaped = _escape_kdl_string(runs_cmd)
        issues_cmd_escaped = _escape_kdl_string(issues_cmd)
        agent_cmd_escaped = _escape_kdl_string(agent_cmd)

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
                args "-c" "{runs_cmd_escaped}"
                start_suspended false
            }}
            pane split_direction="vertical" size="65%" {{
                pane size="35%" command="bash" {{
                    args "-c" "{issues_cmd_escaped}"
                    start_suspended false
                }}
                pane size="65%" focus=true command="bash" {{
                    args "-c" "{agent_cmd_escaped}"
                    start_suspended false
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
        if project_scope:
            env["ORCH_PROJECT"] = project_scope
        inside_zellij = os.environ.get("ZELLIJ_SESSION_NAME") is not None
        for var in ("ZELLIJ", "ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID"):
            removed = env.pop(var, None)
            if removed is not None:
                _launcher_logger.info(f"cleared env var {var}={removed}")

        # We only reach a fresh launch when no LIVE session exists. Any same-named
        # session here is serialized/resurrectable ("EXITED - attach to resurrect");
        # delete it so the layout runs FRESH. Otherwise zellij resurrects the
        # command panes in a held "Waiting to run" state (ignoring start_suspended).
        try:
            subprocess.run(
                ["zellij", "delete-session", session_name, "--force"],
                env=env,
                capture_output=True,
                timeout=self.ZELLIJ_TIMEOUT_SEC,
            )
            _launcher_logger.info(f"cleared resurrectable session {session_name}")
        except Exception as e:
            _launcher_logger.info(f"delete-session pre-clean skipped: {e}")

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
    project_scope: str | None = None,
    monitor_session_name: str = "orch-monitor",
    vault_path: Path | None = None,
    agent: str = "opencode",
    new: bool = False,
    new_control_agent: bool = False,
    agent_override: str = "",
    multiplexer: MultiplexerType | None = None,
    log: callable = lambda msg: None,
    show_spinner: bool = True,
) -> None:
    """Launch the orch-monitor layout in a terminal multiplexer.

    Args:
        project_root: Path to project root
        project_scope: Project identity for orch CLI scoping
        monitor_session_name: Multiplexer session name resolved by orch
        vault_path: Deprecated, unused compatibility argument
        agent: Agent command to use (default: opencode)
        new: If True, restart the layout (kill existing session)
        new_control_agent: If True, also restart the control agent session.
                          If False, preserve control agent session on layout restart.
        agent_override: Explicit control agent override.
                        Empty string means daemon resolves from repo config.
        multiplexer: Which multiplexer to use (auto-detected if None)
        log: Logging function
        show_spinner: Whether to show visual spinner during slow operations
    """
    _setup_launcher_logging()
    _launcher_logger.info(
        f"launch_monitor_layout: project_root={project_root}, project_scope={project_scope}, new={new}, new_control_agent={new_control_agent}"
    )

    # Phase 1: Detect and validate multiplexer
    with _spinner_context(
        "Detecting terminal multiplexer...", enabled=show_spinner
    ) as status:
        if multiplexer is None:
            _launcher_logger.info("detecting multiplexer...")
            multiplexer = get_default_multiplexer_type()
            _launcher_logger.info(f"detected multiplexer: {multiplexer.value}")

        # Validate multiplexer config - reject zellij monitor + zellij agents
        try:
            validate_multiplexer_config(multiplexer)
        except InvalidMultiplexerConfigError as e:
            _launcher_logger.error(str(e))
            # rich_escape: daemon/exception text may contain [bracketed] segments
            # (e.g. the allowed-targets list) that Rich would parse as markup
            # tags and silently drop (fail-clearly violation).
            _console.print(f"[red]Error:[/red] {rich_escape(str(e))}")
            sys.exit(1)

        launcher = get_layout_launcher(multiplexer)
        cwd = str(project_root) if project_root else os.getcwd()
        # For zellij, ensure a short ZELLIJ_SOCKET_DIR before any zellij call so the
        # socket path stays within the OS sun_path limit (macOS 104).
        if multiplexer == MultiplexerType.ZELLIJ:
            sock_dir = _ensure_short_zellij_socket_dir()
            _launcher_logger.info(f"using ZELLIJ_SOCKET_DIR={sock_dir}")
        session_name = monitor_session_name
        _launcher_logger.info(f"session_name={session_name}, cwd={cwd}")

        # Phase 2: Check for existing session
        status.update(f"Checking for existing {multiplexer.value} session...")
        _launcher_logger.info("checking has_session...")
        session_exists = launcher.has_session(session_name)

    # Handle existing session (outside spinner context for clean output)
    if session_exists:
        _launcher_logger.info("session exists")
        if new or new_control_agent:
            if new_control_agent:
                clear_control_session(project_root)

            # Pre-flight: when --new (layout restart only), verify control agent
            # session is recoverable BEFORE destroying the existing layout.
            if new and not new_control_agent:
                project_str = str(project_root) if project_root else str(Path.cwd())
                session_file = Path(project_str) / ".orch" / "control-session.json"
                _launcher_logger.info(
                    "--new: checking if control agent session is recoverable..."
                )
                _launcher_logger.info(
                    f"  project_root={project_str}, agent={agent_override or '(auto)'}, "
                    f"session_file={session_file}"
                )
                session_id = load_control_session(project_root)
                if not session_id:
                    _launcher_logger.error(
                        "no previous control agent session found to resume"
                    )
                    _launcher_logger.error(
                        f"  project_root={project_str}, agent={agent_override or '(auto)'}, "
                        f"session_file={session_file}, exists={session_file.exists()}"
                    )
                    _console.print(
                        "[red]Error:[/red] --new requires an existing control agent session to resume, "
                        "but none was found."
                    )
                    _console.print("")
                    _console.print("[dim]Diagnostic details:[/dim]")
                    _console.print(
                        f"[dim]  project_root: {rich_escape(project_str)}[/dim]"
                    )
                    _console.print(
                        f"[dim]  agent: {rich_escape(agent_override or '(auto)')}[/dim]"
                    )
                    _console.print(
                        f"[dim]  session_file: {rich_escape(str(session_file))}[/dim]"
                    )
                    _console.print(
                        f"[dim]  session_file exists: {session_file.exists()}[/dim]"
                    )
                    if session_file.exists():
                        try:
                            _console.print(
                                f"[dim]  session_file content: {rich_escape(session_file.read_text().strip())}[/dim]"
                            )
                        except Exception as read_err:
                            _console.print(
                                f"[dim]  session_file read error: {rich_escape(str(read_err))}[/dim]"
                            )
                    _console.print("")
                    _console.print(
                        "[dim]To start fresh:  orch-monitor --new-control-agent[/dim]"
                    )
                    _console.print(
                        "[dim]To attach as-is: orch-monitor  (no flags)[/dim]"
                    )
                    sys.exit(1)

                _console.print(
                    f"[dim]Resuming control agent session: {rich_escape(session_id)}[/dim]"
                )

            with _spinner_context("Restarting session...", enabled=show_spinner):
                _launcher_logger.info("killing existing session...")
                launcher.kill_session(session_name)
                _launcher_logger.info("session killed")
        else:
            if show_spinner:
                _console.print(
                    f"[dim]Attaching to existing session:[/dim] {rich_escape(session_name)}"
                )
            _launcher_logger.info("attaching to existing session...")
            launcher.attach_session(session_name)
            return

    # Check for unhealthy Zellij
    if (
        multiplexer == MultiplexerType.ZELLIJ
        and hasattr(launcher, "_healthy")
        and launcher._healthy is False
    ):
        _launcher_logger.error("Zellij is unresponsive (likely stale processes)")
        _console.print(
            "[red]Error:[/red] Zellij is unresponsive (likely stale processes)"
        )
        _console.print("[dim]Fix: Run 'pkill -9 zellij' to kill stale processes[/dim]")
        _console.print("[dim]Or use: orch-monitor --new -m tmux[/dim]")
        sys.exit(1)

    # Phase 3: Launch layout
    if show_spinner:
        _console.print(f"[dim]Starting orch-monitor in {multiplexer.value}...[/dim]")
    _launcher_logger.info("launching layout...")
    try:
        launcher.launch_layout(
            session_name,
            project_root,
            None,
            agent,
            cwd,
            new_control_agent,
            agent_override=agent_override,
            project_scope=project_scope,
        )
    except ControlAgentLaunchError as e:
        # Daemon refused the control-agent launch (codex profile allowed_targets
        # constraint). Surface the fail-fast error rather than launching on a
        # disallowed host. rich_escape: the denial names the allowed targets as
        # "[mac]", which Rich would otherwise parse as a markup tag and drop.
        _launcher_logger.error(f"control agent launch refused by daemon: {e}")
        _console.print(f"[red]Error:[/red] {rich_escape(str(e))}")
        sys.exit(1)
    _launcher_logger.info("launch complete")


def main():
    parser = argparse.ArgumentParser(description="Orch monitor TUI")
    parser.add_argument(
        "--project",
        type=str,
        help="Project identity (git repo URL or normalized repo ID)",
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
    parser.add_argument(
        "--remote",
        default=None,
        help="Remote orch daemon/master address (host:port or alias). "
        "Overrides ORCH_REMOTE / client.yaml remote.default. "
        'Use --remote="" to force local.',
    )

    args = parser.parse_args()

    if args.project:
        os.environ["ORCH_PROJECT"] = args.project

    # Make the remote selection visible to child panes (which re-run this module)
    # and to all client resolution within this process.
    if args.remote is not None:
        os.environ["ORCH_REMOTE"] = args.remote

    if args.verbose:
        import time as _time

        _start = _time.time()

        def _log(msg: str) -> None:
            elapsed = _time.time() - _start
            print(f"[{elapsed:.2f}s] {msg}", file=sys.stderr)
    else:

        def _log(msg: str) -> None:
            pass

    try:
        bootstrap = load_client_bootstrap()
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

    project_root = get_project_root(bootstrap)
    project_scope = get_project_scope(bootstrap)
    if project_scope:
        os.environ["ORCH_PROJECT"] = project_scope

    # Determine if we should show spinners (only for layout mode, not single panes)
    show_spinner = not args.runs and not args.issues

    # Check configuration state before daemon - show onboarding if unconfigured
    from .config import detect_configuration_state

    with _spinner_context("Checking configuration...", enabled=show_spinner):
        config_state = detect_configuration_state()

    # Show onboarding if unconfigured (only for full layout mode, not single panes)
    if show_spinner:
        if not config_state.has_orch_dir or not config_state.has_config_file:
            from .app import OnboardingApp

            _log("showing onboarding screen - config not found")
            app = OnboardingApp(config_state)
            result = app.run()
            if not result:
                print("Setup cancelled. Run 'orch tutorial' for help.", file=sys.stderr)
                sys.exit(0)
            # Refresh paths after onboarding
            config_state = detect_configuration_state()

    _log("ensuring daemon...")
    with _spinner_context("Connecting to daemon...", enabled=show_spinner):
        success, error_msg = ensure_daemon(project_root, project_scope, bootstrap)
    if not success:
        print(f"Error: {error_msg}", file=sys.stderr)
        print("Try running 'orch repair' to fix.", file=sys.stderr)
        sys.exit(1)
    _log("daemon ready")

    _log("loading config...")
    if project_root:
        config = Config.from_project_root(project_root)
    else:
        config = Config.load()
    setup_logging(config.log_path)
    _log("config loaded")

    if args.runs:
        app = RunsDashboard(project_root=project_root)
        app.run()
    elif args.issues:
        app = IssuesDashboard(project_root=project_root)
        app.run()
    else:
        mux_type = None
        if args.multiplexer:
            mux_type = MultiplexerType(args.multiplexer)
        _log("starting launch_monitor_layout")
        # --new-control-agent implies --new for layout restart
        new = args.new or args.new_control_agent
        # Use config.agent only as fallback command. Let daemon resolve control agent
        # from repo config unless --agent is explicitly provided.
        default_agent = config.control_agent or config.agent or "claude"
        agent = args.agent if args.agent is not None else default_agent
        agent_override = args.agent if args.agent is not None else ""
        _log(f"using agent: {agent}")
        launch_monitor_layout(
            project_root=project_root,
            project_scope=project_scope,
            monitor_session_name=bootstrap.monitor_session_name,
            vault_path=None,
            agent=agent,
            new=new,
            new_control_agent=args.new_control_agent,
            agent_override=agent_override,
            multiplexer=mux_type,
            log=_log,
        )


if __name__ == "__main__":
    main()
