"""Main Textual app for orch monitor.

This module re-exports classes from Hy implementations that use
macro-based error handling to enforce no-silent-error patterns.
"""

# Enable Hy imports
import hy  # noqa: F401

# Re-export from Hy implementations with enforced error handling
from .runs_dashboard import RunsDashboard, RUNS_DASHBOARD_CSS, COMMON_CSS
from .issues_dashboard import IssuesDashboard
from .orch_monitor_app import OrchMonitorApp, ORCH_MONITOR_CSS
from .help_screen import HelpScreen, OnboardingApp
from .filter_screens import RunFilterScreen, IssueFilterScreen
from .confirm_screens import KillConfirmScreen, CloseIssueConfirmScreen
from .agent_screen import AgentSelectScreen

# Re-export helper functions from app_base.hy
from .app_base import (
    get_logger,
    setup_logging,
    _log_fallback,
    _log_error,
    _get_log_path,
    _format_changed_files_lines,
    _build_orch_cmd,
    _get_editor_command,
    _get_issue_file_path,
    _input_has_focus,
    _get_available_agents,
    _is_url_path,
    AUTO_REFRESH_INTERVAL,
    ELAPSED_UPDATE_INTERVAL,
    MESSAGE_REFRESH_INTERVAL,
    AGENTS,
    TIME_RANGES,
)

# Re-export types for backward compatibility
from .types import RunFilterResult, IssueFilterResult


__all__ = [
    # Main App classes (from Hy)
    "RunsDashboard",
    "IssuesDashboard",
    "OrchMonitorApp",
    "HelpScreen",
    "OnboardingApp",
    "RunFilterScreen",
    "IssueFilterScreen",
    "KillConfirmScreen",
    "CloseIssueConfirmScreen",
    "AgentSelectScreen",
    # CSS constants
    "COMMON_CSS",
    "RUNS_DASHBOARD_CSS",
    "ORCH_MONITOR_CSS",
    # Helper functions
    "get_logger",
    "setup_logging",
    "_log_fallback",
    "_log_error",
    "_get_log_path",
    "_format_changed_files_lines",
    "_build_orch_cmd",
    "_get_editor_command",
    "_get_issue_file_path",
    "_input_has_focus",
    "_get_available_agents",
    "_is_url_path",
    # Constants
    "AUTO_REFRESH_INTERVAL",
    "ELAPSED_UPDATE_INTERVAL",
    "MESSAGE_REFRESH_INTERVAL",
    "AGENTS",
    "TIME_RANGES",
    # Types
    "RunFilterResult",
    "IssueFilterResult",
]
