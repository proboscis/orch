"""Tests for HelpScreen functionality."""

import pytest
from orch_monitor.app import HelpScreen, OrchMonitorApp, RunsDashboard, IssuesDashboard


class TestHelpScreenStructure:
    """Test HelpScreen class structure."""

    def test_help_screen_exists(self):
        """HelpScreen class should be importable."""
        assert HelpScreen is not None

    def test_help_screen_has_css(self):
        """HelpScreen should have CSS defined."""
        assert HelpScreen.CSS
        assert "#help-dialog" in HelpScreen.CSS

    def test_help_screen_has_bindings(self):
        """HelpScreen should have close bindings."""
        binding_keys = [b.key for b in HelpScreen.BINDINGS]
        assert "escape" in binding_keys
        assert "q" in binding_keys
        assert "?" in binding_keys


class TestHelpBindings:
    """Test that help bindings exist in all app classes."""

    @pytest.mark.parametrize("app_class", [RunsDashboard, IssuesDashboard, OrchMonitorApp])
    def test_help_binding_exists(self, app_class):
        """Each app class should have a help binding."""
        help_bindings = [b for b in app_class.BINDINGS if b.action == "help"]
        assert len(help_bindings) == 1, f"{app_class.__name__} should have exactly one help binding"
        
        binding = help_bindings[0]
        assert binding.key == "question_mark", f"{app_class.__name__} help binding should use 'question_mark' key"
        assert binding.key_display == "?", f"{app_class.__name__} help binding should display as '?'"

    @pytest.mark.parametrize("app_class", [RunsDashboard, IssuesDashboard, OrchMonitorApp])
    def test_action_help_exists(self, app_class):
        """Each app class should have action_help method."""
        assert hasattr(app_class, "action_help"), f"{app_class.__name__} should have action_help method"
        assert callable(getattr(app_class, "action_help")), f"{app_class.__name__}.action_help should be callable"
