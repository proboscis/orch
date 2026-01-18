"""XDG Base Directory Spec-compliant path helpers for orch.

Mirrors the Go implementation in internal/xdg/paths.go.
See: https://specifications.freedesktop.org/basedir-spec/latest/
"""

import os
import platform
import subprocess
from pathlib import Path
from typing import Optional

APP_NAME = "orch"


def runtime_dir() -> Path:
    """Return the XDG runtime directory for orch.
    
    Falls back to /tmp/orch-{uid} if XDG_RUNTIME_DIR is not set.
    On macOS, falls back to ~/Library/Caches/orch/run.
    """
    xdg_runtime = os.environ.get("XDG_RUNTIME_DIR")
    if xdg_runtime:
        return Path(xdg_runtime) / APP_NAME
    
    # macOS fallback
    if platform.system() == "Darwin":
        return Path.home() / "Library" / "Caches" / APP_NAME / "run"
    
    # Linux/Unix fallback
    return Path(f"/tmp/{APP_NAME}-{os.getuid()}")


def state_dir() -> Path:
    """Return the XDG state directory for orch.
    
    This is where daemon logs and other state files go.
    """
    xdg_state = os.environ.get("XDG_STATE_HOME")
    if xdg_state:
        return Path(xdg_state) / APP_NAME
    
    # macOS fallback
    if platform.system() == "Darwin":
        return Path.home() / "Library" / "Logs" / APP_NAME
    
    # Default: ~/.local/state/orch
    return Path.home() / ".local" / "state" / APP_NAME


def data_dir() -> Path:
    """Return the XDG data directory for orch.
    
    This is where per-repo run data goes.
    """
    xdg_data = os.environ.get("XDG_DATA_HOME")
    if xdg_data:
        return Path(xdg_data) / APP_NAME
    
    # macOS fallback
    if platform.system() == "Darwin":
        return Path.home() / "Library" / "Application Support" / APP_NAME
    
    # Default: ~/.local/share/orch
    return Path.home() / ".local" / "share" / APP_NAME


def config_dir() -> Path:
    """Return the XDG config directory for orch (global config)."""
    xdg_config = os.environ.get("XDG_CONFIG_HOME")
    if xdg_config:
        return Path(xdg_config) / APP_NAME
    
    # Default: ~/.config/orch
    return Path.home() / ".config" / APP_NAME


def socket_path() -> Path:
    """Return the path to the global daemon socket."""
    return runtime_dir() / "daemon.sock"


def pid_path() -> Path:
    """Return the path to the global daemon PID file."""
    return runtime_dir() / "daemon.pid"


def log_path() -> Path:
    """Return the path to the daemon log file."""
    return state_dir() / "daemon.log"


def legacy_orch_dir(project_root: Path) -> Path:
    """Return the legacy .orch directory path for a project."""
    return project_root / ".orch"


def legacy_socket_path(project_root: Path) -> Path:
    """Return the legacy socket path in .orch/."""
    return legacy_orch_dir(project_root) / "daemon.sock"


def has_legacy_daemon(project_root: Path) -> bool:
    """Check if a legacy per-project daemon socket exists."""
    return legacy_socket_path(project_root).exists()


def ensure_runtime_dir() -> None:
    """Create the runtime directory with appropriate permissions."""
    runtime_dir().mkdir(parents=True, exist_ok=True, mode=0o700)


def ensure_state_dir() -> None:
    """Create the state directory with appropriate permissions."""
    state_dir().mkdir(parents=True, exist_ok=True, mode=0o755)
