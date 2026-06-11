"""Client bootstrap values resolved by the Go orch CLI."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path
from typing import Optional


class ClientBootstrapError(RuntimeError):
    pass


@dataclass(frozen=True)
class ClientBootstrap:
    project_root: Optional[Path]
    project_id: str
    remote_addr: Optional[str]
    socket_path: Path
    monitor_session_name: str


def _resolve_orch_binary() -> str:
    return os.getenv("ORCH_BIN") or shutil.which("orch") or "orch"


@lru_cache(maxsize=1)
def load_client_bootstrap() -> ClientBootstrap:
    orch_bin = _resolve_orch_binary()
    try:
        result = subprocess.run(
            [orch_bin, "debug", "client-bootstrap", "--json"],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ClientBootstrapError(
            f"failed to run orch client bootstrap via {orch_bin!r}: {exc}"
        ) from exc

    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        raise ClientBootstrapError(
            "orch client bootstrap failed"
            + (f": {detail}" if detail else f" with exit {result.returncode}")
        )

    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise ClientBootstrapError("orch client bootstrap returned invalid JSON") from exc

    project_root = str(data.get("project_root") or "").strip()
    project_id = str(data.get("project_id") or "").strip()
    remote_addr = str(data.get("remote_addr") or "").strip()
    socket_path = str(data.get("socket_path") or "").strip()
    monitor_session_name = str(data.get("monitor_session_name") or "").strip()
    if not project_root:
        raise ClientBootstrapError("orch client bootstrap returned empty project_root")
    if not project_id:
        raise ClientBootstrapError("orch client bootstrap returned empty project_id")
    if not socket_path:
        raise ClientBootstrapError("orch client bootstrap returned empty socket_path")
    if not monitor_session_name:
        raise ClientBootstrapError(
            "orch client bootstrap returned empty monitor_session_name"
        )

    return ClientBootstrap(
        project_root=Path(project_root),
        project_id=project_id,
        remote_addr=remote_addr or None,
        socket_path=Path(socket_path),
        monitor_session_name=monitor_session_name,
    )
