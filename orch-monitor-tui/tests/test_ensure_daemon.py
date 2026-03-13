"""Tests for daemon startup and auto-repair behavior in orch-monitor launcher."""

from pathlib import Path
from subprocess import CompletedProcess
from unittest.mock import MagicMock

from orch_monitor.__main__ import ensure_daemon
from orch_monitor.config import Config


def test_ensure_daemon_auto_repair_recovers(monkeypatch):
    project_root = Path("/tmp/project")
    cfg = Config(project_root=project_root)

    monkeypatch.setattr("orch_monitor.__main__.Config.from_project_root", lambda _: cfg)
    monkeypatch.setattr("orch_monitor.__main__.DAEMON_STARTUP_TIMEOUT_SEC", 1)
    monkeypatch.setattr("orch_monitor.__main__.DAEMON_POLL_INTERVAL_SEC", 0.5)
    monkeypatch.setattr("orch_monitor.__main__.time.sleep", lambda *_: None)

    daemon = MagicMock()
    availability = iter([False, False, False, True])

    def is_available():
        try:
            return next(availability)
        except StopIteration:
            return True

    daemon.is_available.side_effect = is_available
    monkeypatch.setattr("orch_monitor.__main__.ProtoDaemonClient", lambda *_: daemon)

    commands: list[list[str]] = []

    def mock_run(args, **_kwargs):
        commands.append(args)
        if args[-2:] == ["daemon", "start"]:
            return CompletedProcess(args=args, returncode=0, stdout="", stderr="")
        if args[-2:] == ["repair", "--force"]:
            return CompletedProcess(args=args, returncode=1, stdout="fixed", stderr="")
        raise AssertionError(f"unexpected command: {args}")

    monkeypatch.setattr("orch_monitor.__main__.subprocess.run", mock_run)

    ok, msg = ensure_daemon(project_root=project_root)
    assert ok is True
    assert msg == ""
    assert any(cmd[-2:] == ["repair", "--force"] for cmd in commands)


def test_ensure_daemon_auto_repair_still_fails(monkeypatch):
    project_root = Path("/tmp/project")
    cfg = Config(project_root=project_root)

    monkeypatch.setattr("orch_monitor.__main__.Config.from_project_root", lambda _: cfg)
    monkeypatch.setattr("orch_monitor.__main__.DAEMON_STARTUP_TIMEOUT_SEC", 1)
    monkeypatch.setattr("orch_monitor.__main__.DAEMON_POLL_INTERVAL_SEC", 0.5)
    monkeypatch.setattr("orch_monitor.__main__.time.sleep", lambda *_: None)

    daemon = MagicMock()
    daemon.is_available.return_value = False
    monkeypatch.setattr("orch_monitor.__main__.ProtoDaemonClient", lambda *_: daemon)

    commands: list[list[str]] = []

    def mock_run(args, **_kwargs):
        commands.append(args)
        if args[-2:] == ["daemon", "start"]:
            return CompletedProcess(args=args, returncode=0, stdout="", stderr="")
        if args[-2:] == ["repair", "--force"]:
            return CompletedProcess(args=args, returncode=1, stdout="fixed", stderr="")
        raise AssertionError(f"unexpected command: {args}")

    monkeypatch.setattr("orch_monitor.__main__.subprocess.run", mock_run)

    ok, msg = ensure_daemon(project_root=project_root)
    assert ok is False
    assert "after auto-repair" in msg

    daemon_starts = [cmd for cmd in commands if cmd[-2:] == ["daemon", "start"]]
    repairs = [cmd for cmd in commands if cmd[-2:] == ["repair", "--force"]]
    assert len(daemon_starts) == 2
    assert len(repairs) == 1
