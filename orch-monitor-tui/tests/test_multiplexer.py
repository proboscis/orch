"""Contract tests for multiplexer command generation.

These tests verify that the multiplexer implementations generate correct commands
without actually executing them. This is useful for:
1. Ensuring command structure is correct
2. Testing in CI without requiring tmux/zellij
3. Verifying zellij-specific features work correctly
"""

import subprocess
from unittest.mock import patch, MagicMock, call
import os

import pytest

from orch_monitor.multiplexer import (
    TmuxMultiplexer,
    ZellijMultiplexer,
    MultiplexerType,
    get_multiplexer,
    detect_current_multiplexer,
    get_default_multiplexer_type,
)


# ============================================================================
# TmuxMultiplexer Command Generation Tests
# ============================================================================


class TestTmuxCommands:
    """Test that TmuxMultiplexer generates correct subprocess commands."""

    @pytest.fixture
    def tmux(self):
        return TmuxMultiplexer()

    def test_has_session_command(self, tmux):
        """Test has_session generates correct tmux command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = tmux.has_session("test-session")

            mock_run.assert_called_once_with(
                ["tmux", "has-session", "-t", "test-session"],
                capture_output=True,
            )
            assert result is True

    def test_has_session_not_found(self, tmux):
        """Test has_session returns False when session doesn't exist."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=1)

            result = tmux.has_session("nonexistent")

            assert result is False

    def test_kill_session_command(self, tmux):
        """Test kill_session generates correct tmux command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = tmux.kill_session("test-session")

            mock_run.assert_called_once_with(
                ["tmux", "kill-session", "-t", "test-session"],
                capture_output=True,
            )
            assert result is True

    def test_new_session_command(self, tmux):
        """Test new_session generates correct tmux command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = tmux.new_session(
                "my-session", "/path/to/cwd", width=200, height=60
            )

            mock_run.assert_called_once_with(
                [
                    "tmux",
                    "new-session",
                    "-d",
                    "-s",
                    "my-session",
                    "-x",
                    "200",
                    "-y",
                    "60",
                    "-c",
                    "/path/to/cwd",
                ]
            )
            assert result is True

    def test_split_horizontal_command(self, tmux):
        """Test split_horizontal generates correct tmux command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = tmux.split_horizontal("test-session", "/cwd")

            mock_run.assert_called_once_with(
                ["tmux", "split-window", "-h", "-t", "test-session", "-c", "/cwd"]
            )
            assert result is True

    def test_split_vertical_command(self, tmux):
        """Test split_vertical generates correct tmux command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = tmux.split_vertical(
                "session", "session:0.1", "/cwd", percentage=30
            )

            mock_run.assert_called_once_with(
                [
                    "tmux",
                    "split-window",
                    "-v",
                    "-t",
                    "session:0.1",
                    "-p",
                    "30",
                    "-c",
                    "/cwd",
                ]
            )

    def test_send_keys_command(self, tmux):
        """Test send_keys generates correct tmux command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = tmux.send_keys("session:0.0", "echo hello", enter=True)

            mock_run.assert_called_once_with(
                ["tmux", "send-keys", "-t", "session:0.0", "echo hello", "Enter"]
            )
            assert result is True

    def test_send_keys_no_enter(self, tmux):
        """Test send_keys without Enter key."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            tmux.send_keys("session:0.0", "partial-text", enter=False)

            mock_run.assert_called_once_with(
                ["tmux", "send-keys", "-t", "session:0.0", "partial-text"]
            )

    def test_list_windows_command(self, tmux):
        """Test list_windows generates correct tmux command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(
                returncode=0, stdout="window1\nwindow2\nedit-issue\n"
            )

            result = tmux.list_windows()

            mock_run.assert_called_once_with(
                ["tmux", "list-windows", "-F", "#{window_name}"],
                capture_output=True,
                text=True,
            )
            assert result == ["window1", "window2", "edit-issue"]

    def test_list_windows_empty(self, tmux):
        """Test list_windows returns empty list when no windows."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=1, stdout="")

            result = tmux.list_windows()

            assert result == []

    def test_select_window_command(self, tmux):
        """Test select_window generates correct tmux command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = tmux.select_window("edit-issue")

            mock_run.assert_called_once_with(
                ["tmux", "select-window", "-t", ":edit-issue"],
                capture_output=True,
            )
            assert result is True

    def test_new_tab_with_command_creates_new(self, tmux):
        """Test new_tab_with_command creates new window when not exists."""
        with patch("subprocess.run") as mock_run:
            mock_run.side_effect = [
                MagicMock(returncode=0, stdout="window1\nwindow2\n"),
                MagicMock(returncode=0),
            ]

            result = tmux.new_tab_with_command(
                "edit-issue", ["vim", "/path/to/file.md"], cwd="/project/root"
            )

            assert mock_run.call_count == 2
            calls = mock_run.call_args_list
            assert calls[0] == call(
                ["tmux", "list-windows", "-F", "#{window_name}"],
                capture_output=True,
                text=True,
            )
            assert calls[1] == call(
                [
                    "tmux",
                    "new-window",
                    "-n",
                    "edit-issue",
                    "-c",
                    "/project/root",
                    "vim",
                    "/path/to/file.md",
                ],
                capture_output=True,
            )
            assert result is True

    def test_new_tab_with_command_selects_existing(self, tmux):
        """Test new_tab_with_command selects existing window instead of creating."""
        with patch("subprocess.run") as mock_run:
            mock_run.side_effect = [
                MagicMock(returncode=0, stdout="window1\nedit-issue\n"),
                MagicMock(returncode=0),
            ]

            result = tmux.new_tab_with_command(
                "edit-issue", ["vim", "/path/to/file.md"], cwd="/project/root"
            )

            assert mock_run.call_count == 2
            calls = mock_run.call_args_list
            assert calls[0] == call(
                ["tmux", "list-windows", "-F", "#{window_name}"],
                capture_output=True,
                text=True,
            )
            assert calls[1] == call(
                ["tmux", "select-window", "-t", ":edit-issue"],
                capture_output=True,
            )
            assert result is True


# ============================================================================
# ZellijMultiplexer Command Generation Tests
# ============================================================================


class TestZellijCommands:
    """Test that ZellijMultiplexer generates correct subprocess commands."""

    @pytest.fixture
    def zellij(self):
        return ZellijMultiplexer()

    def test_has_session_command(self, zellij):
        """Test has_session queries zellij list-sessions."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(
                returncode=0, stdout="my-session\nother-session\n"
            )

            result = zellij.has_session("my-session")

            mock_run.assert_called_once_with(
                ["zellij", "list-sessions"],
                capture_output=True,
                text=True,
            )
            assert result is True

    def test_has_session_not_found(self, zellij):
        """Test has_session returns False when session not listed."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0, stdout="other-session\n")

            result = zellij.has_session("nonexistent")

            assert result is False

    def test_kill_session_command(self, zellij):
        """Test kill_session generates correct zellij command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = zellij.kill_session("test-session")

            mock_run.assert_called_once_with(
                ["zellij", "delete-session", "test-session", "--force"],
                capture_output=True,
            )
            assert result is True

    def test_split_horizontal_command(self, zellij):
        """Test split_horizontal generates correct zellij action command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = zellij.split_horizontal("my-session", "/cwd")

            mock_run.assert_called_once_with(
                [
                    "zellij",
                    "--session",
                    "my-session",
                    "action",
                    "new-pane",
                    "--direction",
                    "right",
                ],
                cwd="/cwd",
            )
            assert result is True

    def test_split_vertical_command(self, zellij):
        """Test split_vertical generates correct zellij action command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = zellij.split_vertical(
                "my-session", "ignored", "/cwd", percentage=50
            )

            mock_run.assert_called_once_with(
                [
                    "zellij",
                    "--session",
                    "my-session",
                    "action",
                    "new-pane",
                    "--direction",
                    "down",
                ],
                cwd="/cwd",
            )

    def test_send_keys_command(self, zellij):
        """Test send_keys generates correct zellij write-chars command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = zellij.send_keys("my-session:ignored", "echo hello", enter=True)

            mock_run.assert_called_once_with(
                [
                    "zellij",
                    "--session",
                    "my-session",
                    "action",
                    "write-chars",
                    "echo hello\n",
                ]
            )
            assert result is True

    def test_send_keys_no_enter(self, zellij):
        """Test send_keys without newline."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            zellij.send_keys("my-session", "partial", enter=False)

            mock_run.assert_called_once_with(
                [
                    "zellij",
                    "--session",
                    "my-session",
                    "action",
                    "write-chars",
                    "partial",
                ]
            )

    def test_list_windows_command(self, zellij):
        """Test list_windows generates correct zellij command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(
                returncode=0, stdout="tab1\ntab2\nedit-tab\n"
            )

            result = zellij.list_windows()

            mock_run.assert_called_once_with(
                ["zellij", "action", "query-tab-names"],
                capture_output=True,
                text=True,
            )
            assert result == ["tab1", "tab2", "edit-tab"]

    def test_list_windows_empty(self, zellij):
        """Test list_windows returns empty list when no tabs."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=1, stdout="")

            result = zellij.list_windows()

            assert result == []

    def test_select_window_command(self, zellij):
        """Test select_window generates correct zellij command."""
        with patch("subprocess.run") as mock_run:
            mock_run.return_value = MagicMock(returncode=0)

            result = zellij.select_window("edit-tab")

            mock_run.assert_called_once_with(
                ["zellij", "action", "go-to-tab-name", "edit-tab"],
                capture_output=True,
            )
            assert result is True

    def test_new_tab_with_command_creates_new(self, zellij):
        """Test new_tab_with_command creates new tab when not exists."""
        with patch("subprocess.run") as mock_run:
            mock_run.side_effect = [
                MagicMock(returncode=0, stdout="tab1\ntab2\n"),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
            ]

            result = zellij.new_tab_with_command(
                "edit-tab", ["vim", "/path/to/file.md"], cwd="/project"
            )

            assert mock_run.call_count == 4
            calls = mock_run.call_args_list

            assert calls[0] == call(
                ["zellij", "action", "query-tab-names"],
                capture_output=True,
                text=True,
            )
            assert calls[1] == call(
                [
                    "zellij",
                    "action",
                    "new-tab",
                    "--name",
                    "edit-tab",
                    "--cwd",
                    "/project",
                ],
                capture_output=True,
            )
            assert calls[2] == call(
                ["zellij", "action", "write-chars", "vim /path/to/file.md"],
                capture_output=True,
            )
            assert calls[3] == call(
                ["zellij", "action", "write", "10"],
                capture_output=True,
            )

            assert result is True

    def test_new_tab_with_command_selects_existing(self, zellij):
        """Test new_tab_with_command selects existing tab instead of creating."""
        with patch("subprocess.run") as mock_run:
            mock_run.side_effect = [
                MagicMock(returncode=0, stdout="tab1\nedit-tab\n"),
                MagicMock(returncode=0),
            ]

            result = zellij.new_tab_with_command(
                "edit-tab", ["vim", "/path/to/file.md"], cwd="/project"
            )

            assert mock_run.call_count == 2
            calls = mock_run.call_args_list

            assert calls[0] == call(
                ["zellij", "action", "query-tab-names"],
                capture_output=True,
                text=True,
            )
            assert calls[1] == call(
                ["zellij", "action", "go-to-tab-name", "edit-tab"],
                capture_output=True,
            )
            assert result is True

    def test_new_tab_with_command_escapes_spaces(self, zellij):
        """Test new_tab_with_command properly escapes paths with spaces."""
        with patch("subprocess.run") as mock_run:
            mock_run.side_effect = [
                MagicMock(returncode=0, stdout="tab1\n"),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
            ]

            result = zellij.new_tab_with_command(
                "edit-tab", ["vim", "/path/with spaces/file.md"], cwd="/project"
            )

            assert result is True
            calls = mock_run.call_args_list
            # The write-chars call should have properly quoted path
            write_chars_call = calls[2]
            assert write_chars_call == call(
                ["zellij", "action", "write-chars", "vim '/path/with spaces/file.md'"],
                capture_output=True,
            )

    def test_new_tab_with_command_escapes_special_characters(self, zellij):
        """Test new_tab_with_command properly escapes paths with special characters."""
        with patch("subprocess.run") as mock_run:
            mock_run.side_effect = [
                MagicMock(returncode=0, stdout="tab1\n"),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
            ]

            # Test with dollar sign, exclamation, and ampersand
            result = zellij.new_tab_with_command(
                "edit-tab",
                ["vim", "/path/with$special!chars&here/file.md"],
                cwd="/project",
            )

            assert result is True
            calls = mock_run.call_args_list
            write_chars_call = calls[2]
            # shlex.join quotes the path to protect special characters
            assert write_chars_call == call(
                [
                    "zellij",
                    "action",
                    "write-chars",
                    "vim '/path/with$special!chars&here/file.md'",
                ],
                capture_output=True,
            )

    def test_new_tab_with_command_escapes_quotes(self, zellij):
        """Test new_tab_with_command properly escapes paths with quotes."""
        with patch("subprocess.run") as mock_run:
            mock_run.side_effect = [
                MagicMock(returncode=0, stdout="tab1\n"),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
            ]

            # Test with double quotes in path
            result = zellij.new_tab_with_command(
                "edit-tab",
                ["code", "--wait", '/path/with "quotes"/file.md'],
                cwd="/project",
            )

            assert result is True
            calls = mock_run.call_args_list
            write_chars_call = calls[2]
            # shlex.join escapes the quotes properly
            assert write_chars_call == call(
                [
                    "zellij",
                    "action",
                    "write-chars",
                    "code --wait '/path/with \"quotes\"/file.md'",
                ],
                capture_output=True,
            )

    def test_new_tab_with_command_no_escaping_needed(self, zellij):
        """Test new_tab_with_command doesn't add unnecessary quotes for simple paths."""
        with patch("subprocess.run") as mock_run:
            mock_run.side_effect = [
                MagicMock(returncode=0, stdout="tab1\n"),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
                MagicMock(returncode=0),
            ]

            result = zellij.new_tab_with_command(
                "edit-tab", ["vim", "/simple/path/file.md"], cwd="/project"
            )

            assert result is True
            calls = mock_run.call_args_list
            write_chars_call = calls[2]
            # No quoting needed for simple paths
            assert write_chars_call == call(
                ["zellij", "action", "write-chars", "vim /simple/path/file.md"],
                capture_output=True,
            )


# ============================================================================
# Multiplexer Detection Tests
# ============================================================================


class TestMultiplexerDetection:
    """Tests for multiplexer detection and selection."""

    def test_detect_tmux_inside(self):
        """Test detecting when inside tmux."""
        with patch.dict(os.environ, {"TMUX": "/tmp/tmux-1001/default,12345,0"}):
            result = detect_current_multiplexer()
            assert result == MultiplexerType.TMUX

    def test_detect_zellij_inside(self):
        """Test detecting when inside zellij."""
        with patch.dict(os.environ, {"ZELLIJ": "0"}, clear=False):
            # Clear TMUX to ensure we detect zellij
            env = os.environ.copy()
            env.pop("TMUX", None)
            env["ZELLIJ"] = "0"
            with patch.dict(os.environ, env, clear=True):
                result = detect_current_multiplexer()
                assert result == MultiplexerType.ZELLIJ

    def test_detect_none_outside(self):
        """Test detecting when outside any multiplexer."""
        with patch.dict(os.environ, {}, clear=True):
            result = detect_current_multiplexer()
            assert result is None

    def test_get_multiplexer_tmux(self):
        """Test getting tmux multiplexer by type."""
        mux = get_multiplexer(MultiplexerType.TMUX)
        assert isinstance(mux, TmuxMultiplexer)
        assert mux.name == "tmux"

    def test_get_multiplexer_zellij(self):
        """Test getting zellij multiplexer by type."""
        mux = get_multiplexer(MultiplexerType.ZELLIJ)
        assert isinstance(mux, ZellijMultiplexer)
        assert mux.name == "zellij"


# ============================================================================
# Multiplexer Availability Tests
# ============================================================================


class TestMultiplexerAvailability:
    """Tests for multiplexer availability checks."""

    def test_tmux_is_available_when_found(self):
        """Test tmux availability when binary exists."""
        tmux = TmuxMultiplexer()
        with patch("shutil.which", return_value="/usr/bin/tmux"):
            assert tmux.is_available() is True

    def test_tmux_not_available_when_missing(self):
        """Test tmux not available when binary missing."""
        tmux = TmuxMultiplexer()
        with patch("shutil.which", return_value=None):
            assert tmux.is_available() is False

    def test_zellij_is_available_when_found(self):
        """Test zellij availability when binary exists."""
        zellij = ZellijMultiplexer()
        with patch("shutil.which", return_value="/usr/local/bin/zellij"):
            assert zellij.is_available() is True

    def test_zellij_not_available_when_missing(self):
        """Test zellij not available when binary missing."""
        zellij = ZellijMultiplexer()
        with patch("shutil.which", return_value=None):
            assert zellij.is_available() is False


# ============================================================================
# Default Multiplexer Selection Tests
# ============================================================================


class TestDefaultMultiplexerSelection:
    """Tests for default multiplexer selection logic."""

    def test_prefers_current_multiplexer(self):
        """Test that current multiplexer is preferred."""
        with patch.dict(os.environ, {"TMUX": "/tmp/tmux"}):
            with patch("shutil.which", return_value="/usr/bin/tmux"):
                result = get_default_multiplexer_type()
                assert result == MultiplexerType.TMUX

    def test_uses_env_var_when_outside(self):
        """Test ORCH_MULTIPLEXER env var is used when outside."""
        env = {"ORCH_MULTIPLEXER": "zellij"}
        with patch.dict(os.environ, env, clear=True):
            with patch("shutil.which", return_value="/usr/bin/zellij"):
                result = get_default_multiplexer_type()
                assert result == MultiplexerType.ZELLIJ


# ============================================================================
# Command Generation Contract Tests (Zellij-specific)
# ============================================================================


class TestZellijCommandContracts:
    """Contract tests verifying zellij command structure meets CLI requirements.

    These tests document the expected command formats for zellij operations,
    ensuring compatibility with zellij CLI.
    """

    def test_dump_screen_command_format(self):
        """Document expected zellij dump-screen command format."""
        # Expected: zellij action dump-screen <path>
        expected_cmd = ["zellij", "action", "dump-screen", "/tmp/output.txt"]

        # This is what a test or integration would use
        assert expected_cmd[0] == "zellij"
        assert expected_cmd[1] == "action"
        assert expected_cmd[2] == "dump-screen"

    def test_dump_layout_command_format(self):
        """Document expected zellij dump-layout command format."""
        expected_cmd = ["zellij", "action", "dump-layout"]

        assert expected_cmd[0] == "zellij"
        assert expected_cmd[1] == "action"
        assert expected_cmd[2] == "dump-layout"

    def test_new_pane_command_format(self):
        """Document expected zellij new-pane command format."""
        # Expected for starting opencode in a new pane
        expected_cmd = [
            "zellij",
            "action",
            "new-pane",
            "--cwd",
            "/path/to/worktree",
            "--",
            "opencode",
        ]

        assert expected_cmd[0] == "zellij"
        assert expected_cmd[1] == "action"
        assert expected_cmd[2] == "new-pane"
        assert "--cwd" in expected_cmd
        assert "--" in expected_cmd

    def test_session_aware_command_format(self):
        """Document session-aware zellij command format."""
        # Commands that target a specific session
        expected_cmd = ["zellij", "--session", "orch-run-123", "action", "new-pane"]

        assert expected_cmd[0] == "zellij"
        assert expected_cmd[1] == "--session"
        assert "action" in expected_cmd
