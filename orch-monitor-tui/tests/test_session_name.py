"""Tests for daemon-resolved monitor bootstrap values."""

import json
import os
from pathlib import Path
from subprocess import CompletedProcess

import pytest

from orch_monitor import client_bootstrap
from orch_monitor.__main__ import (
    ZELLIJ_CONTRACT_DIR,
    ZELLIJ_SOCK_MAX_LENGTH,
    _ensure_short_zellij_socket_dir,
)
from orch_monitor.client_bootstrap import ClientBootstrapError, load_client_bootstrap


def test_bootstrap_uses_go_resolved_monitor_session_name(monkeypatch):
    load_client_bootstrap.cache_clear()
    payload = {
        "project_root": "/repo/orch",
        "project_id": "repoid:owner-orch",
        "remote_addr": "127.0.0.1:9000",
        "socket_path": "/tmp/orch.sock",
        "monitor_session_name": "orch-monitor-owner-orch",
    }

    def fake_run(*args, **kwargs):
        return CompletedProcess(args[0], 0, stdout=json.dumps(payload), stderr="")

    monkeypatch.setattr(client_bootstrap.subprocess, "run", fake_run)

    bootstrap = load_client_bootstrap()

    assert bootstrap.project_root == Path("/repo/orch")
    assert bootstrap.project_id == "repoid:owner-orch"
    assert bootstrap.remote_addr == "127.0.0.1:9000"
    assert bootstrap.socket_path == Path("/tmp/orch.sock")
    assert bootstrap.monitor_session_name == "orch-monitor-owner-orch"


@pytest.mark.parametrize(
    "remote_override",
    ["", "master.example:7777"],
)
def test_bootstrap_passes_explicit_remote_override(
    monkeypatch, remote_override: str
) -> None:
    load_client_bootstrap.cache_clear()
    monkeypatch.setenv("ORCH_REMOTE", remote_override)
    payload = {
        "project_root": "/repo/orch",
        "project_id": "repoid:owner-orch",
        "remote_addr": remote_override,
        "socket_path": "/tmp/orch.sock",
        "monitor_session_name": "orch-monitor-owner-orch",
    }
    captured_commands: list[list[str]] = []

    def fake_run(command: list[str], **kwargs):
        captured_commands.append(command)
        return CompletedProcess(command, 0, stdout=json.dumps(payload), stderr="")

    monkeypatch.setattr(client_bootstrap.subprocess, "run", fake_run)

    load_client_bootstrap()

    assert len(captured_commands) == 1
    assert captured_commands[0][1:] == [
        "--remote",
        remote_override,
        "debug",
        "client-bootstrap",
        "--json",
    ]


def test_bootstrap_fails_fast_on_bad_cli(monkeypatch):
    load_client_bootstrap.cache_clear()

    def fake_run(*args, **kwargs):
        return CompletedProcess(args[0], 1, stdout="", stderr="boom")

    monkeypatch.setattr(client_bootstrap.subprocess, "run", fake_run)

    with pytest.raises(ClientBootstrapError, match="boom"):
        load_client_bootstrap()


def test_bootstrap_failure_does_not_fallback_to_env(monkeypatch):
    load_client_bootstrap.cache_clear()
    monkeypatch.setenv("ORCH_PROJECT", "repoid:fallback")
    monkeypatch.setenv("ORCH_REMOTE", "127.0.0.1:7777")

    def fake_run(*args, **kwargs):
        return CompletedProcess(args[0], 1, stdout="", stderr="unsupported")

    monkeypatch.setattr(client_bootstrap.subprocess, "run", fake_run)

    with pytest.raises(ClientBootstrapError, match="unsupported"):
        load_client_bootstrap()


def test_bootstrap_cache_has_single_required_path(monkeypatch):
    load_client_bootstrap.cache_clear()
    calls = 0
    payload = {
        "project_root": "/repo/orch",
        "project_id": "repoid:owner-orch",
        "remote_addr": "",
        "socket_path": "/tmp/orch.sock",
        "monitor_session_name": "orch-monitor-owner-orch",
    }

    def fake_run(*args, **kwargs):
        nonlocal calls
        calls += 1
        return CompletedProcess(args[0], 0, stdout=json.dumps(payload), stderr="")

    monkeypatch.setattr(client_bootstrap.subprocess, "run", fake_run)

    assert load_client_bootstrap() is load_client_bootstrap()
    assert calls == 1


def test_ensure_short_socket_dir_sets_when_unset(monkeypatch):
    monkeypatch.delenv("ZELLIJ_SOCKET_DIR", raising=False)
    base = _ensure_short_zellij_socket_dir()
    assert base == os.environ["ZELLIJ_SOCKET_DIR"]
    assert base.startswith("/tmp/zlj-")
    assert len(base) + 1 + len(ZELLIJ_CONTRACT_DIR) + 1 < ZELLIJ_SOCK_MAX_LENGTH


def test_ensure_short_socket_dir_respects_existing(monkeypatch):
    monkeypatch.setenv("ZELLIJ_SOCKET_DIR", "/custom/sock")
    assert _ensure_short_zellij_socket_dir() == "/custom/sock"
    assert os.environ["ZELLIJ_SOCKET_DIR"] == "/custom/sock"
