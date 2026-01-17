"""Multiplexer abstraction for terminal session management using Strategy pattern."""

import os
import shutil
import subprocess
from abc import ABC, abstractmethod
from enum import Enum
from typing import Optional, Protocol


class MultiplexerType(str, Enum):
    """Supported terminal multiplexers."""

    TMUX = "tmux"
    ZELLIJ = "zellij"


class Multiplexer(Protocol):
    """Protocol defining terminal multiplexer operations."""

    @property
    def name(self) -> str:
        """Return the multiplexer name."""
        ...

    def is_available(self) -> bool:
        """Check if this multiplexer is available on the system."""
        ...

    def is_inside(self) -> bool:
        """Check if we're running inside this multiplexer."""
        ...

    def has_session(self, session_name: str) -> bool:
        """Check if a session exists."""
        ...

    def kill_session(self, session_name: str) -> bool:
        """Kill a session. Returns True if session was killed."""
        ...

    def attach_session(self, session_name: str) -> None:
        """Attach to a session."""
        ...

    def new_session(
        self,
        session_name: str,
        working_dir: str,
        width: int = 180,
        height: int = 50,
    ) -> bool:
        """Create a new detached session."""
        ...

    def split_horizontal(self, session_name: str, working_dir: str) -> bool:
        """Split the window horizontally (left/right)."""
        ...

    def split_vertical(
        self,
        session_name: str,
        pane_target: str,
        working_dir: str,
        percentage: int = 50,
    ) -> bool:
        """Split the window vertically (top/bottom)."""
        ...

    def send_keys(self, target: str, keys: str, enter: bool = True) -> bool:
        """Send keys to a pane."""
        ...

    def select_pane(self, target: str) -> bool:
        """Select/focus a pane."""
        ...


class TmuxMultiplexer:
    """Tmux implementation of Multiplexer."""

    @property
    def name(self) -> str:
        return "tmux"

    def is_available(self) -> bool:
        return shutil.which("tmux") is not None

    def is_inside(self) -> bool:
        return bool(os.environ.get("TMUX"))

    def has_session(self, session_name: str) -> bool:
        result = subprocess.run(
            ["tmux", "has-session", "-t", session_name],
            capture_output=True,
        )
        return result.returncode == 0

    def kill_session(self, session_name: str) -> bool:
        result = subprocess.run(
            ["tmux", "kill-session", "-t", session_name],
            capture_output=True,
        )
        return result.returncode == 0

    def attach_session(self, session_name: str) -> None:
        subprocess.run(["tmux", "attach-session", "-t", session_name])

    def new_session(
        self,
        session_name: str,
        working_dir: str,
        width: int = 180,
        height: int = 50,
    ) -> bool:
        result = subprocess.run(
            [
                "tmux",
                "new-session",
                "-d",
                "-s",
                session_name,
                "-x",
                str(width),
                "-y",
                str(height),
                "-c",
                working_dir,
            ]
        )
        return result.returncode == 0

    def split_horizontal(self, session_name: str, working_dir: str) -> bool:
        result = subprocess.run(
            ["tmux", "split-window", "-h", "-t", session_name, "-c", working_dir]
        )
        return result.returncode == 0

    def split_vertical(
        self,
        session_name: str,
        pane_target: str,
        working_dir: str,
        percentage: int = 50,
    ) -> bool:
        result = subprocess.run(
            [
                "tmux",
                "split-window",
                "-v",
                "-t",
                pane_target,
                "-p",
                str(percentage),
                "-c",
                working_dir,
            ]
        )
        return result.returncode == 0

    def send_keys(self, target: str, keys: str, enter: bool = True) -> bool:
        args = ["tmux", "send-keys", "-t", target, keys]
        if enter:
            args.append("Enter")
        result = subprocess.run(args)
        return result.returncode == 0

    def select_pane(self, target: str) -> bool:
        result = subprocess.run(["tmux", "select-pane", "-t", target])
        return result.returncode == 0


class ZellijMultiplexer:
    """Zellij implementation of Multiplexer."""

    @property
    def name(self) -> str:
        return "zellij"

    def is_available(self) -> bool:
        return shutil.which("zellij") is not None

    def is_inside(self) -> bool:
        return bool(os.environ.get("ZELLIJ"))

    def has_session(self, session_name: str) -> bool:
        result = subprocess.run(
            ["zellij", "list-sessions"],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            return False
        # zellij list-sessions outputs one session per line
        sessions = result.stdout.strip().split("\n")
        # Session names may have additional info after them
        return any(s.startswith(session_name) for s in sessions)

    def kill_session(self, session_name: str) -> bool:
        result = subprocess.run(
            ["zellij", "delete-session", session_name, "--force"],
            capture_output=True,
        )
        return result.returncode == 0

    def attach_session(self, session_name: str) -> None:
        subprocess.run(["zellij", "attach", session_name])

    def new_session(
        self,
        session_name: str,
        working_dir: str,
        width: int = 180,
        height: int = 50,
    ) -> bool:
        # Zellij creates session on attach, we use a layout file approach
        # For detached creation, we start zellij in background
        result = subprocess.run(
            ["zellij", "--session", session_name],
            cwd=working_dir,
            start_new_session=True,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        return True  # Zellij creates session on first attach

    def split_horizontal(self, session_name: str, working_dir: str) -> bool:
        result = subprocess.run(
            [
                "zellij",
                "--session",
                session_name,
                "action",
                "new-pane",
                "--direction",
                "right",
            ],
            cwd=working_dir,
        )
        return result.returncode == 0

    def split_vertical(
        self,
        session_name: str,
        pane_target: str,
        working_dir: str,
        percentage: int = 50,
    ) -> bool:
        result = subprocess.run(
            [
                "zellij",
                "--session",
                session_name,
                "action",
                "new-pane",
                "--direction",
                "down",
            ],
            cwd=working_dir,
        )
        return result.returncode == 0

    def send_keys(self, target: str, keys: str, enter: bool = True) -> bool:
        session_name = target.split(":")[0] if ":" in target else target
        cmd = keys + ("\n" if enter else "")
        result = subprocess.run(
            ["zellij", "--session", session_name, "action", "write-chars", cmd]
        )
        return result.returncode == 0

    def select_pane(self, target: str) -> bool:
        # Zellij doesn't support direct pane selection by ID
        # Would need focus-next-pane or similar
        return False


# Registry of multiplexer implementations
_MULTIPLEXERS: dict[MultiplexerType, Multiplexer] = {
    MultiplexerType.TMUX: TmuxMultiplexer(),
    MultiplexerType.ZELLIJ: ZellijMultiplexer(),
}


def get_multiplexer(mux_type: MultiplexerType) -> Multiplexer:
    """Get multiplexer implementation by type (DI factory)."""
    return _MULTIPLEXERS[mux_type]


def detect_current_multiplexer() -> Optional[MultiplexerType]:
    """Detect which multiplexer we're running inside, if any."""
    for mux_type, mux in _MULTIPLEXERS.items():
        if mux.is_inside():
            return mux_type
    return None


def get_default_multiplexer_type() -> MultiplexerType:
    """Get the default multiplexer based on environment and availability."""
    # Prefer the one we're inside of
    inside = detect_current_multiplexer()
    if inside:
        return inside

    # Check ORCH_MULTIPLEXER environment variable
    env_mux = os.environ.get("ORCH_MULTIPLEXER", "").lower()
    if env_mux == "zellij" and _MULTIPLEXERS[MultiplexerType.ZELLIJ].is_available():
        return MultiplexerType.ZELLIJ

    # Default to tmux if available
    if _MULTIPLEXERS[MultiplexerType.TMUX].is_available():
        return MultiplexerType.TMUX

    # Fall back to zellij
    if _MULTIPLEXERS[MultiplexerType.ZELLIJ].is_available():
        return MultiplexerType.ZELLIJ

    # Default to tmux even if not available (will fail later with clear error)
    return MultiplexerType.TMUX


# Convenience functions for working with Run objects


def get_session_name(run) -> Optional[str]:
    """Get the session name from a run."""
    return run.tmux_session if run.tmux_session else None


def get_multiplexer_type_from_run(run) -> MultiplexerType:
    """Get the multiplexer type from a run's metadata."""
    mux = run.multiplexer.lower() if run.multiplexer else "tmux"
    if mux == "zellij":
        return MultiplexerType.ZELLIJ
    return MultiplexerType.TMUX


def get_multiplexer_for_run(run) -> Multiplexer:
    """Get the appropriate multiplexer implementation for a run."""
    mux_type = get_multiplexer_type_from_run(run)
    return get_multiplexer(mux_type)
