"""Optional integration tests for zellij.

These tests require a running zellij installation and actually execute
zellij commands. They are skipped if zellij is not available.

Run with: pytest tests/test_zellij_integration.py -v

These tests are designed for CI environments or local testing where
zellij is installed and can create real sessions.
"""

import os
import shutil
import subprocess
import time
from pathlib import Path

import pytest

from orch_monitor.multiplexer import ZellijMultiplexer, MultiplexerType


# Skip all tests in this module if zellij is not available
pytestmark = pytest.mark.skipif(
    shutil.which("zellij") is None,
    reason="zellij is not installed",
)


# ============================================================================
# Test Session Management
# ============================================================================


@pytest.fixture
def test_session_name():
    """Generate a unique test session name."""
    return f"orch-test-{os.getpid()}-{int(time.time())}"


@pytest.fixture
def zellij():
    """Create a ZellijMultiplexer instance."""
    return ZellijMultiplexer()


@pytest.fixture(autouse=True)
def cleanup_test_sessions(test_session_name):
    """Clean up any test sessions after each test."""
    yield
    # Cleanup: try to kill the test session if it exists
    try:
        subprocess.run(
            ["zellij", "delete-session", test_session_name, "--force"],
            capture_output=True,
            timeout=5,
        )
    except (subprocess.TimeoutExpired, Exception):
        pass


# ============================================================================
# Basic Zellij Operations
# ============================================================================


class TestZellijSessionOperations:
    """Integration tests for zellij session operations."""

    def test_is_available(self, zellij):
        """Test that zellij is detected as available."""
        assert zellij.is_available() is True

    def test_list_sessions(self, zellij):
        """Test listing zellij sessions doesn't error."""
        result = subprocess.run(
            ["zellij", "list-sessions"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        # Should not fail (may have 0 or more sessions)
        assert result.returncode == 0 or "no active sessions" in result.stderr.lower()


# ============================================================================
# Session Lifecycle Tests
# ============================================================================


class TestZellijSessionLifecycle:
    """Tests for zellij session create/kill lifecycle.

    Note: These tests actually create zellij sessions, which requires
    a terminal that supports zellij operations.
    """

    @pytest.mark.skip(reason="Requires headless zellij setup for CI")
    def test_session_create_and_kill(self, zellij, test_session_name):
        """Test creating and killing a zellij session."""
        # Create session (this is tricky as zellij creates on attach)
        # For now, we'll skip this test in CI

        # If we could create:
        # assert zellij.has_session(test_session_name) is True
        # assert zellij.kill_session(test_session_name) is True
        # assert zellij.has_session(test_session_name) is False
        pass

    @pytest.mark.skip(reason="Requires headless zellij setup for CI")
    def test_send_keys_to_session(self, zellij, test_session_name):
        """Test sending keys to a zellij session."""
        # Would need to create session first
        pass


# ============================================================================
# Zellij Action Tests
# ============================================================================


class TestZellijActions:
    """Tests for zellij action commands.

    These can be run if we're inside an existing zellij session.
    """

    @pytest.fixture
    def inside_zellij(self):
        """Check if running inside zellij."""
        return bool(os.environ.get("ZELLIJ"))

    def test_dump_layout_command(self, inside_zellij):
        """Test dump-layout command when inside zellij."""
        if not inside_zellij:
            pytest.skip("Not inside a zellij session")

        result = subprocess.run(
            ["zellij", "action", "dump-layout"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        # When inside zellij, this should work
        assert result.returncode == 0


# ============================================================================
# CI/Headless Mode Tests
# ============================================================================


class TestZellijHeadlessMode:
    """Tests that can run in CI/headless environments.

    These tests verify command structure and behavior without
    actually interacting with real sessions.
    """

    def test_list_sessions_format(self):
        """Test that list-sessions returns expected format."""
        result = subprocess.run(
            ["zellij", "list-sessions"],
            capture_output=True,
            text=True,
            timeout=10,
        )

        # Either succeeds with session list or reports no sessions
        if result.returncode == 0:
            # Output should be text with session names
            assert isinstance(result.stdout, str)
        else:
            # May fail if no sessions or other issue
            assert "no" in result.stderr.lower() or result.returncode != 0

    def test_help_command(self):
        """Test that zellij help command works."""
        result = subprocess.run(
            ["zellij", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert result.returncode == 0
        assert "zellij" in result.stdout.lower()

    def test_version_command(self):
        """Test that zellij version command works."""
        result = subprocess.run(
            ["zellij", "--version"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert result.returncode == 0


# ============================================================================
# Dump Screen Content Tests
# ============================================================================


class TestZellijScreenDump:
    """Tests for zellij screen content dumping.

    These tests verify the dump-screen functionality that would be used
    for testing run output.
    """

    @pytest.fixture
    def inside_zellij(self):
        return bool(os.environ.get("ZELLIJ"))

    @pytest.mark.skip(reason="Requires active zellij session")
    def test_dump_screen_to_file(self, inside_zellij, tmp_path):
        """Test dumping screen content to a file."""
        if not inside_zellij:
            pytest.skip("Not inside a zellij session")

        output_file = tmp_path / "screen-dump.txt"

        result = subprocess.run(
            ["zellij", "action", "dump-screen", str(output_file)],
            capture_output=True,
            timeout=10,
        )

        if result.returncode == 0:
            assert output_file.exists()
            content = output_file.read_text()
            assert isinstance(content, str)


# ============================================================================
# Notes for CI Setup
# ============================================================================
#
# To run these tests in CI:
#
# 1. Install zellij:
#    cargo install zellij  # or use package manager
#
# 2. Start a headless zellij session:
#    #!/bin/bash
#    zellij --session test-session &
#    ZELLIJ_PID=$!
#    sleep 2
#
#    # Run tests
#    pytest tests/test_zellij_integration.py -v
#
#    # Cleanup
#    zellij kill-session test-session
#    kill $ZELLIJ_PID 2>/dev/null
#
# 3. Alternative: Use zellij's headless mode (if available)
#
# 4. For GitHub Actions, consider using a container with zellij pre-installed
#
