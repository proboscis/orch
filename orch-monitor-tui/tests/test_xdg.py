"""Tests for XDG Base Directory Spec path helpers."""

import os
import platform
from pathlib import Path
from unittest.mock import patch

import pytest

from orch_monitor import xdg
from orch_monitor.config import Config


class TestRuntimeDir:
    """Tests for runtime_dir()."""

    def test_uses_xdg_runtime_dir_when_set(self, tmp_path: Path):
        """Should use XDG_RUNTIME_DIR when set."""
        with patch.dict(os.environ, {"XDG_RUNTIME_DIR": str(tmp_path)}):
            result = xdg.runtime_dir()
            assert result == tmp_path / "orch"

    def test_macos_fallback(self, tmp_path: Path):
        """Should use ~/Library/Caches/orch/run on macOS when XDG not set."""
        with patch.dict(os.environ, {}, clear=True):
            # Remove XDG_RUNTIME_DIR if present
            env = {k: v for k, v in os.environ.items() if k != "XDG_RUNTIME_DIR"}
            with patch.dict(os.environ, env, clear=True):
                with patch.object(platform, "system", return_value="Darwin"):
                    result = xdg.runtime_dir()
                    assert result == Path.home() / "Library" / "Caches" / "orch" / "run"

    def test_linux_fallback(self):
        """Should use /tmp/orch-{uid} on Linux when XDG not set."""
        env = {k: v for k, v in os.environ.items() if k != "XDG_RUNTIME_DIR"}
        with patch.dict(os.environ, env, clear=True):
            with patch.object(platform, "system", return_value="Linux"):
                result = xdg.runtime_dir()
                assert result == Path(f"/tmp/orch-{os.getuid()}")


class TestStateDir:
    """Tests for state_dir()."""

    def test_uses_xdg_state_home_when_set(self, tmp_path: Path):
        """Should use XDG_STATE_HOME when set."""
        with patch.dict(os.environ, {"XDG_STATE_HOME": str(tmp_path)}):
            result = xdg.state_dir()
            assert result == tmp_path / "orch"

    def test_macos_fallback(self):
        """Should use ~/Library/Logs/orch on macOS when XDG not set."""
        env = {k: v for k, v in os.environ.items() if k != "XDG_STATE_HOME"}
        with patch.dict(os.environ, env, clear=True):
            with patch.object(platform, "system", return_value="Darwin"):
                result = xdg.state_dir()
                assert result == Path.home() / "Library" / "Logs" / "orch"

    def test_linux_default(self):
        """Should use ~/.local/state/orch on Linux when XDG not set."""
        env = {k: v for k, v in os.environ.items() if k != "XDG_STATE_HOME"}
        with patch.dict(os.environ, env, clear=True):
            with patch.object(platform, "system", return_value="Linux"):
                result = xdg.state_dir()
                assert result == Path.home() / ".local" / "state" / "orch"


class TestDataDir:
    """Tests for data_dir()."""

    def test_uses_xdg_data_home_when_set(self, tmp_path: Path):
        """Should use XDG_DATA_HOME when set."""
        with patch.dict(os.environ, {"XDG_DATA_HOME": str(tmp_path)}):
            result = xdg.data_dir()
            assert result == tmp_path / "orch"

    def test_macos_fallback(self):
        """Should use ~/Library/Application Support/orch on macOS."""
        env = {k: v for k, v in os.environ.items() if k != "XDG_DATA_HOME"}
        with patch.dict(os.environ, env, clear=True):
            with patch.object(platform, "system", return_value="Darwin"):
                result = xdg.data_dir()
                assert result == Path.home() / "Library" / "Application Support" / "orch"

    def test_linux_default(self):
        """Should use ~/.local/share/orch on Linux."""
        env = {k: v for k, v in os.environ.items() if k != "XDG_DATA_HOME"}
        with patch.dict(os.environ, env, clear=True):
            with patch.object(platform, "system", return_value="Linux"):
                result = xdg.data_dir()
                assert result == Path.home() / ".local" / "share" / "orch"


class TestConfigDir:
    """Tests for config_dir()."""

    def test_uses_xdg_config_home_when_set(self, tmp_path: Path):
        """Should use XDG_CONFIG_HOME when set."""
        with patch.dict(os.environ, {"XDG_CONFIG_HOME": str(tmp_path)}):
            result = xdg.config_dir()
            assert result == tmp_path / "orch"

    def test_default(self):
        """Should use ~/.config/orch when XDG not set."""
        env = {k: v for k, v in os.environ.items() if k != "XDG_CONFIG_HOME"}
        with patch.dict(os.environ, env, clear=True):
            result = xdg.config_dir()
            assert result == Path.home() / ".config" / "orch"


class TestDaemonPaths:
    """Tests for daemon-related path functions."""

    def test_socket_path(self, tmp_path: Path):
        """socket_path() should return runtime_dir/daemon.sock."""
        with patch.dict(os.environ, {"XDG_RUNTIME_DIR": str(tmp_path)}):
            result = xdg.socket_path()
            assert result == tmp_path / "orch" / "daemon.sock"

    def test_pid_path(self, tmp_path: Path):
        """pid_path() should return runtime_dir/daemon.pid."""
        with patch.dict(os.environ, {"XDG_RUNTIME_DIR": str(tmp_path)}):
            result = xdg.pid_path()
            assert result == tmp_path / "orch" / "daemon.pid"

    def test_log_path(self, tmp_path: Path):
        """log_path() should return state_dir/daemon.log."""
        with patch.dict(os.environ, {"XDG_STATE_HOME": str(tmp_path)}):
            result = xdg.log_path()
            assert result == tmp_path / "orch" / "daemon.log"


class TestLegacyPaths:
    """Tests for legacy path helpers."""

    def test_legacy_orch_dir(self, tmp_path: Path):
        """legacy_orch_dir() should return project/.orch."""
        result = xdg.legacy_orch_dir(tmp_path)
        assert result == tmp_path / ".orch"

    def test_legacy_socket_path(self, tmp_path: Path):
        """legacy_socket_path() should return project/.orch/daemon.sock."""
        result = xdg.legacy_socket_path(tmp_path)
        assert result == tmp_path / ".orch" / "daemon.sock"

    def test_has_legacy_daemon_false(self, tmp_path: Path):
        """has_legacy_daemon() should return False when socket doesn't exist."""
        result = xdg.has_legacy_daemon(tmp_path)
        assert result is False

    def test_has_legacy_daemon_true(self, tmp_path: Path):
        """has_legacy_daemon() should return True when socket exists."""
        orch_dir = tmp_path / ".orch"
        orch_dir.mkdir()
        socket_path = orch_dir / "daemon.sock"
        socket_path.touch()
        
        result = xdg.has_legacy_daemon(tmp_path)
        assert result is True


class TestEnsureDirs:
    """Tests for directory creation functions."""

    def test_ensure_runtime_dir(self, tmp_path: Path):
        """ensure_runtime_dir() should create the directory with 0700 perms."""
        with patch.dict(os.environ, {"XDG_RUNTIME_DIR": str(tmp_path)}):
            xdg.ensure_runtime_dir()
            runtime = tmp_path / "orch"
            assert runtime.exists()
            assert runtime.is_dir()
            # Check permissions (0700 = owner rwx only)
            assert (runtime.stat().st_mode & 0o777) == 0o700

    def test_ensure_state_dir(self, tmp_path: Path):
        """ensure_state_dir() should create the directory."""
        with patch.dict(os.environ, {"XDG_STATE_HOME": str(tmp_path)}):
            xdg.ensure_state_dir()
            state = tmp_path / "orch"
            assert state.exists()
            assert state.is_dir()


class TestConfigSocketPath:
    """Tests for Config.socket_path using XDG."""

    def test_config_socket_path_uses_xdg(self, tmp_path: Path):
        """Config.socket_path should use the global XDG socket path."""
        with patch.dict(os.environ, {"XDG_RUNTIME_DIR": str(tmp_path)}):
            config = Config(project_root=tmp_path / "project")
            assert config.socket_path == tmp_path / "orch" / "daemon.sock"

    def test_config_socket_path_not_project_relative(self, tmp_path: Path):
        """Config.socket_path should NOT be in project/.orch anymore."""
        with patch.dict(os.environ, {"XDG_RUNTIME_DIR": str(tmp_path)}):
            project = tmp_path / "my-project"
            project.mkdir()
            config = Config(project_root=project)
            # Should NOT be project-relative
            assert config.socket_path != project / ".orch" / "daemon.sock"
            # Should be XDG global path
            assert config.socket_path == tmp_path / "orch" / "daemon.sock"


class TestPathConsistency:
    """Tests to ensure Python paths match Go implementation."""

    def test_socket_path_structure(self, tmp_path: Path):
        """Socket path should follow XDG_RUNTIME_DIR/orch/daemon.sock pattern."""
        with patch.dict(os.environ, {"XDG_RUNTIME_DIR": str(tmp_path)}):
            socket = xdg.socket_path()
            # Verify structure matches Go: xdg.RuntimeDir() + "/daemon.sock"
            assert socket.parent.name == "orch"
            assert socket.name == "daemon.sock"

    def test_log_path_structure(self, tmp_path: Path):
        """Log path should follow XDG_STATE_HOME/orch/daemon.log pattern."""
        with patch.dict(os.environ, {"XDG_STATE_HOME": str(tmp_path)}):
            log = xdg.log_path()
            assert log.parent.name == "orch"
            assert log.name == "daemon.log"
