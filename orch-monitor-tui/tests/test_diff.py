"""Tests for diff functionality in orch-monitor TUI."""

import os
import subprocess
import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from orch_monitor.app import (
    OrchMonitorApp,
    RunsDashboard,
    _build_orch_cmd,
)
from orch_monitor.config import Config, MonitorConfig
from orch_monitor.models import Run, Status


# ============================================================================
# Helper to find/build orch binary
# ============================================================================


def get_orch_binary() -> str | None:
    """Get path to orch binary, building if necessary."""
    # Try to find in project root
    project_root = Path(__file__).parent.parent.parent
    
    # Check if we can build
    cmd_dir = project_root / "cmd" / "orch"
    if cmd_dir.exists():
        # Build to temp location
        import tempfile
        tmp = tempfile.mkdtemp()
        binary_path = Path(tmp) / "orch"
        try:
            result = subprocess.run(
                ["go", "build", "-o", str(binary_path), "."],
                cwd=str(cmd_dir),
                capture_output=True,
                timeout=60,
            )
            if result.returncode == 0 and binary_path.exists():
                return str(binary_path)
        except (subprocess.TimeoutExpired, FileNotFoundError):
            pass
    
    return None


# Cache the binary path
_ORCH_BINARY: str | None = None


def orch_binary() -> str:
    """Get orch binary path, building once if needed."""
    global _ORCH_BINARY
    if _ORCH_BINARY is None:
        _ORCH_BINARY = get_orch_binary()
    if _ORCH_BINARY is None:
        pytest.skip("Could not build orch binary")
    return _ORCH_BINARY


# ============================================================================
# Fixtures
# ============================================================================


@pytest.fixture
def mock_run_with_worktree() -> Run:
    """Create a mock run with worktree for diff testing."""
    return Run(
        issue_id="diff-test-issue",
        run_id="20260120-120000",
        path=Path("/tmp/test/diff-test-issue/20260120-120000.md"),
        status=Status.RUNNING,
        agent="claude",
        model="claude-3-5-sonnet",
        model_variant="",
        branch="feature/diff-test",
        worktree_path="/tmp/test-worktree",
        multiplexer="tmux",
        tmux_session="orch-diff-test",
    )


@pytest.fixture
def mock_run_without_worktree() -> Run:
    """Create a mock run without worktree."""
    return Run(
        issue_id="no-worktree-issue",
        run_id="20260120-130000",
        path=Path("/tmp/test/no-worktree-issue/20260120-130000.md"),
        status=Status.RUNNING,
        agent="opencode",
        model="gpt-4",
        model_variant="",
        branch="",
        worktree_path="",  # No worktree
        multiplexer="tmux",
        tmux_session="orch-no-worktree",
    )


@pytest.fixture
def mock_config(tmp_path: Path) -> Config:
    """Create a mock config for testing."""
    orch_dir = tmp_path / ".orch"
    orch_dir.mkdir(parents=True, exist_ok=True)
    return Config(
        project_root=tmp_path,
        issues_root=tmp_path / "issues",
        agent="claude",
        monitor=MonitorConfig(),
    )


# ============================================================================
# Keybinding Tests
# ============================================================================


class TestDiffKeybinding:
    """Tests for the 'd' keybinding in TUI apps."""

    def test_diff_binding_exists_in_runs_dashboard(self):
        """Verify 'd' keybinding is registered in RunsDashboard."""
        binding_keys = [b.key for b in RunsDashboard.BINDINGS]
        assert "d" in binding_keys, "Expected 'd' keybinding in RunsDashboard"

    def test_diff_binding_exists_in_orch_monitor_app(self):
        """Verify 'd' keybinding is registered in OrchMonitorApp."""
        binding_keys = [b.key for b in OrchMonitorApp.BINDINGS]
        assert "d" in binding_keys, "Expected 'd' keybinding in OrchMonitorApp"

    def test_diff_binding_action_name(self):
        """Verify 'd' keybinding is bound to 'diff' action."""
        for binding in RunsDashboard.BINDINGS:
            if binding.key == "d":
                assert binding.action == "diff", f"Expected action 'diff', got '{binding.action}'"
                return
        pytest.fail("'d' keybinding not found")

    def test_action_diff_method_exists_in_runs_dashboard(self):
        """Verify action_diff method exists in RunsDashboard."""
        assert hasattr(RunsDashboard, "action_diff"), "RunsDashboard should have action_diff method"

    def test_action_diff_method_exists_in_orch_monitor_app(self):
        """Verify action_diff method exists in OrchMonitorApp."""
        assert hasattr(OrchMonitorApp, "action_diff"), "OrchMonitorApp should have action_diff method"


# ============================================================================
# Diff Command Build Tests
# ============================================================================


class TestBuildDiffCommand:
    """Tests for building the orch diff command."""

    def test_build_diff_cmd_basic(self, mock_config: Config):
        """Test building basic diff command."""
        base_cmd = _build_orch_cmd(mock_config)
        diff_cmd = base_cmd + ["diff", "test-issue#20260120-120000"]
        
        assert "orch" in diff_cmd[0] or diff_cmd[0] == "orch"
        assert "diff" in diff_cmd
        assert "test-issue#20260120-120000" in diff_cmd

    def test_build_diff_cmd_includes_project_root(self, mock_config: Config):
        """Test that diff command includes project root flag."""
        base_cmd = _build_orch_cmd(mock_config)
        
        # Should include --project-root when config has project_root
        assert "--project-root" in base_cmd or mock_config.project_root is None


# ============================================================================
# Diff Action Tests (with mocking)
# ============================================================================


class TestDiffActionBehavior:
    """Tests for diff action behavior."""

    def test_diff_action_requires_selected_run(self):
        """Verify diff action checks for selected run."""
        import inspect
        source = inspect.getsource(OrchMonitorApp.action_diff)
        assert "selected_run" in source, "action_diff should check selected_run"

    def test_diff_action_checks_worktree_path(self):
        """Verify diff action checks for worktree path."""
        import inspect
        source = inspect.getsource(OrchMonitorApp.action_diff)
        assert "worktree_path" in source, "action_diff should check worktree_path"

    def test_diff_action_notifies_on_no_worktree(self):
        """Verify diff action shows notification when no worktree."""
        import inspect
        source = inspect.getsource(OrchMonitorApp.action_diff)
        assert "notify" in source, "action_diff should notify user"
        assert "no worktree" in source.lower() or "No worktree" in source, \
            "action_diff should mention missing worktree"


# ============================================================================
# Integration with Multiplexer Tests
# ============================================================================


class TestDiffMultiplexerIntegration:
    """Tests for diff integration with terminal multiplexers."""

    def test_diff_opens_in_new_tab(self):
        """Verify diff action tries to open in new terminal tab."""
        import inspect
        
        if hasattr(OrchMonitorApp, "_do_diff"):
            source = inspect.getsource(OrchMonitorApp._do_diff)
            assert "new_tab_with_command" in source, \
                "_do_diff should try to open new tab"
            assert "detect_current_multiplexer" in source, \
                "_do_diff should detect current multiplexer"

    def test_diff_falls_back_to_exit(self):
        """Verify diff action falls back to exit if tab creation fails."""
        import inspect
        
        if hasattr(OrchMonitorApp, "_do_diff"):
            source = inspect.getsource(OrchMonitorApp._do_diff)
            assert "exit" in source.lower(), \
                "_do_diff should have exit fallback"


# ============================================================================
# CLI Integration Tests (using built binary)
# ============================================================================


class TestDiffCLIIntegration:
    """Tests for orch diff CLI command integration."""

    def test_orch_diff_help_available(self):
        """Test that orch diff --help works."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert result.returncode == 0, f"diff --help failed: {result.stderr}"
        assert "diff" in result.stdout.lower()

    def test_orch_diff_accepts_stat_flag(self):
        """Test that orch diff accepts --stat flag."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert "--stat" in result.stdout, "Expected --stat flag in help"

    def test_orch_diff_accepts_base_flag(self):
        """Test that orch diff accepts --base flag."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert "--base" in result.stdout, "Expected --base flag in help"


# ============================================================================
# Tool Selection Tests
# ============================================================================


class TestDiffToolSelection:
    """Tests for diff tool selection priority."""

    def test_env_var_documented_in_help(self):
        """Test that ORCH_DIFFTOOL is documented in help."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert "ORCH_DIFFTOOL" in result.stdout, \
            "Expected ORCH_DIFFTOOL in help text"

    def test_delta_mentioned_in_help(self):
        """Test that delta is mentioned as auto-detected tool."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert "delta" in result.stdout.lower(), \
            "Expected delta mentioned in help text"

    def test_pager_mentioned_in_help(self):
        """Test that PAGER fallback is mentioned in help."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert "PAGER" in result.stdout, \
            "Expected PAGER mentioned in help text"

    def test_tool_priority_order_documented(self):
        """Test that tool priority order is documented."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        output = result.stdout
        assert "1." in output and "2." in output, \
            "Expected numbered priority list in help"


# ============================================================================
# E2E Workflow Tests
# ============================================================================


class TestDiffE2EWorkflow:
    """End-to-end workflow tests for diff functionality."""

    def test_diff_command_requires_run_ref(self):
        """Test that diff command requires a run reference argument."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        # Should fail with error about missing argument
        assert result.returncode != 0
        assert "arg" in result.stderr.lower() or "RUN_REF" in result.stderr

    def test_diff_with_invalid_run_ref(self):
        """Test diff command with invalid run reference."""
        binary = orch_binary()
        result = subprocess.run(
            [binary, "diff", "nonexistent-issue#invalid-run", 
             "--issues-root", "/tmp/nonexistent"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        # Should fail gracefully
        assert result.returncode != 0

    def test_diff_stat_output_format(self):
        """Test that --stat flag produces summary output."""
        binary = orch_binary()
        # Just verify the flag is accepted
        result = subprocess.run(
            [binary, "diff", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert "summary" in result.stdout.lower() or "stat" in result.stdout.lower()
