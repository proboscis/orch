"""Tests for zellij-safe monitor session name generation.

Regression coverage for the bug where a fixed 28-char cap produced names that
overflowed zellij's socket-path limit, surfacing as the misleading
``session name must be less than 0 characters`` error.
"""

from pathlib import Path

import pytest

from orch_monitor.__main__ import (
    SESSION_NAME_PREFIX,
    ZELLIJ_CONTRACT_DIR,
    ZELLIJ_SOCK_MAX_LENGTH,
    get_session_name,
)


def _socket_path_len(name: str, sock_base: str) -> int:
    # Mirror zellij: len(<base>/<contract_dir>/<name>)
    return len(sock_base) + 1 + len(ZELLIJ_CONTRACT_DIR) + 1 + len(name)


def test_short_dir_keeps_readable_name(tmp_path, monkeypatch):
    monkeypatch.setenv("ZELLIJ_SOCKET_DIR", "/tmp/zj")
    repo = tmp_path / "myrepo"
    repo.mkdir()
    name = get_session_name(repo)
    assert name == f"{SESSION_NAME_PREFIX}-myrepo"


def test_long_name_truncates_with_hash(tmp_path, monkeypatch):
    monkeypatch.setenv("ZELLIJ_SOCKET_DIR", "/tmp/zj")
    repo = tmp_path / "a-very-long-repository-name-that-exceeds-the-budget-by-far"
    repo.mkdir()
    name = get_session_name(repo)
    assert name.startswith(SESSION_NAME_PREFIX + "-")
    # 6-char md5 suffix preserved for uniqueness
    assert len(name.rsplit("-", 1)[-1]) == 6


def test_fits_default_macos_style_socket_dir(monkeypatch):
    # Emulate a default macOS TMPDIR-derived socket dir (~length that previously
    # made a 28-char name overflow once the 18-char contract subdir is added).
    sock_base = "/var/folders/q2/8x7k2j9d5cl0abcd1234efgh5678/T/zellij-501"
    monkeypatch.setenv("ZELLIJ_SOCKET_DIR", sock_base)
    # The classic failing repo name.
    name = get_session_name(Path("/work/agent-control-plane"))
    assert _socket_path_len(name, sock_base) < ZELLIJ_SOCK_MAX_LENGTH


@pytest.mark.parametrize(
    "repo_name",
    [
        "x",
        "agent-control-plane",
        "this-is-a-really-really-really-long-monorepo-name",
    ],
)
def test_always_within_socket_limit(repo_name, monkeypatch):
    sock_base = "/var/folders/q2/8x7k2j9d5cl0abcd1234efgh5678/T/zellij-501"
    monkeypatch.setenv("ZELLIJ_SOCKET_DIR", sock_base)
    name = get_session_name(Path("/work") / repo_name)
    assert _socket_path_len(name, sock_base) < ZELLIJ_SOCK_MAX_LENGTH
    assert name  # never empty


def test_invalid_chars_sanitized(tmp_path, monkeypatch):
    monkeypatch.setenv("ZELLIJ_SOCKET_DIR", "/tmp/zj")
    repo = tmp_path / "weird.name with spaces"
    repo.mkdir()
    name = get_session_name(repo)
    # only alnum, dash, underscore remain
    assert all(c.isalnum() or c in "-_" for c in name)
