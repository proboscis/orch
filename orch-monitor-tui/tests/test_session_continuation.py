"""Tests for session continuation behavior in control agent launchers.

These tests verify that orch-monitor uses explicit session IDs instead of
the --continue flag, which can attach to the wrong session when multiple
sessions are running.
"""

from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest


class TestOpenCodeSessionContinuation:
    """Test OpenCode session continuation behavior."""

    def test_tmux_launcher_uses_explicit_session_id_when_stored(self):
        """When a session ID is stored, TmuxLayoutLauncher should use --session flag."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()

        # Mock subprocess.run to capture the command
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        with patch("subprocess.run", side_effect=mock_run), \
             patch("orch_monitor.__main__.load_control_session", return_value="session-123"), \
             patch("orch_monitor.__main__.write_control_prompt", return_value=True):

            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="opencode",
                cwd="/tmp/test",
                new_control_agent=False,
            )

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "opencode" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent opencode command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--session session-123" in cmd_str, f"Should use explicit --session flag, got: {cmd_str}"
        assert "--continue" not in cmd_str, f"Should NOT use --continue flag, got: {cmd_str}"

    def test_tmux_launcher_starts_fresh_when_no_session_stored(self):
        """When no session ID is stored, TmuxLayoutLauncher should start fresh (no --continue)."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()

        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        with patch("subprocess.run", side_effect=mock_run), \
             patch("orch_monitor.__main__.load_control_session", return_value=None), \
             patch("orch_monitor.__main__.write_control_prompt", return_value=True), \
             patch("orch_monitor.__main__.query_latest_opencode_session", return_value=None), \
             patch("orch_monitor.__main__.save_control_session", return_value=True):

            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="opencode",
                cwd="/tmp/test",
                new_control_agent=False,
            )

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "opencode" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent opencode command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--continue" not in cmd_str, f"Should NOT use --continue flag, got: {cmd_str}"
        assert "--prompt" in cmd_str, f"Should use --prompt flag, got: {cmd_str}"


class TestClaudeSessionContinuation:
    """Test Claude session continuation behavior."""

    def test_tmux_launcher_uses_resume_with_session_id(self):
        """When a session ID is stored, TmuxLayoutLauncher should use --resume flag for Claude."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()

        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        with patch("subprocess.run", side_effect=mock_run), \
             patch("orch_monitor.__main__.load_control_session", return_value="claude-session-456"), \
             patch("orch_monitor.__main__.write_control_prompt", return_value=True):

            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="claude",
                cwd="/tmp/test",
                new_control_agent=False,
            )

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "claude" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent claude command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--resume claude-session-456" in cmd_str, f"Should use --resume with session ID, got: {cmd_str}"
        assert "--continue" not in cmd_str, f"Should NOT use --continue flag, got: {cmd_str}"

    def test_tmux_launcher_starts_fresh_when_no_claude_session(self):
        """When no session ID is stored, TmuxLayoutLauncher should start fresh Claude (no --continue)."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()

        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        with patch("subprocess.run", side_effect=mock_run), \
             patch("orch_monitor.__main__.load_control_session", return_value=None), \
             patch("orch_monitor.__main__.write_control_prompt", return_value=True):

            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="claude",
                cwd="/tmp/test",
                new_control_agent=False,
            )

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd:
                # Get the command string (usually the second-to-last argument)
                for i, arg in enumerate(cmd):
                    if arg == "-t" and i + 2 < len(cmd):
                        # Next is pane target, then the command
                        if "0.2" in str(cmd[i + 1]):
                            agent_cmd = cmd
                            break

        assert agent_cmd is not None, "Should have sent agent command to pane 0.2"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--continue" not in cmd_str, f"Should NOT use --continue flag, got: {cmd_str}"


class TestNewControlAgentFlag:
    """Test that --new-control-agent properly starts fresh sessions."""

    def test_new_control_agent_clears_session_and_starts_fresh(self):
        """When new_control_agent=True, should clear session and start fresh."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()

        commands_sent = []
        clear_called = False

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        def mock_clear_session(project_root):
            nonlocal clear_called
            clear_called = True
            return True

        with patch("subprocess.run", side_effect=mock_run), \
             patch("orch_monitor.__main__.clear_control_session", side_effect=mock_clear_session), \
             patch("orch_monitor.__main__.write_control_prompt", return_value=True), \
             patch("orch_monitor.__main__.query_latest_opencode_session", return_value=None), \
             patch("orch_monitor.__main__.save_control_session", return_value=True):

            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="opencode",
                cwd="/tmp/test",
                new_control_agent=True,  # Force new session
            )

        assert clear_called, "Should have called clear_control_session"

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "opencode" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent opencode command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--continue" not in cmd_str, f"Should NOT use --continue flag, got: {cmd_str}"
        assert "--session" not in cmd_str, f"Should NOT use --session flag (fresh start), got: {cmd_str}"
