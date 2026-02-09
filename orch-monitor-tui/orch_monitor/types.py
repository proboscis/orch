"""Shared types for orch-monitor daemon communication.

This module contains:
- Exception classes for daemon errors
- Dataclass definitions for filters and responses
- Constants used across the client implementations
"""

from dataclasses import dataclass, field
from typing import Optional

from .models import Issue, IssueStatus, Run, Status


# ============================================================================
# Exceptions
# ============================================================================


class ProtoDaemonError(Exception):
    """Base exception for proto daemon communication errors."""

    pass


class ProtoDaemonNotRunningError(ProtoDaemonError):
    """Raised when the daemon is not running or socket is unavailable."""

    pass


class ProtoDaemonSocketMissingError(ProtoDaemonNotRunningError):
    """Raised when the daemon socket file does not exist."""

    pass


class ProtoDaemonConnectionRefusedError(ProtoDaemonNotRunningError):
    """Raised when connection to daemon socket is refused (stale socket or daemon not listening)."""

    pass


class ProtoDaemonTimeoutError(ProtoDaemonError):
    """Raised when communication with daemon times out."""

    pass


class ProtoDaemonPermissionError(ProtoDaemonError):
    """Raised when there's a permission issue accessing the daemon socket."""

    pass


# ============================================================================
# Constants
# ============================================================================

MAX_PAGE_SIZE = 200
MAX_PAGES = 100


# ============================================================================
# Filter Dataclasses
# ============================================================================


@dataclass
class RunFilters:
    """Filters for listing runs."""

    issue_id: Optional[str] = None
    status: list[Status] = field(default_factory=list)
    agent: Optional[str] = None
    text_search: Optional[str] = None
    time_range: Optional[str] = None


@dataclass
class IssueFilters:
    """Filters for listing issues."""

    status: list[IssueStatus] = field(default_factory=list)
    tags: list[str] = field(default_factory=list)
    tags_mode: Optional[str] = None
    text_search: Optional[str] = None


# ============================================================================
# Response Dataclasses
# ============================================================================


@dataclass
class ListRunsResponse:
    """Response from listing runs."""

    runs: list[Run]
    next_cursor: Optional[str]
    total: int


@dataclass
class ListIssuesResponse:
    """Response from listing issues."""

    issues: list[Issue]
    next_cursor: Optional[str]
    total: int


@dataclass
class ControlAgentLaunch:
    command: str
    prompt_file: str
    port: int
    session_id: str
    agent: str


# ============================================================================
# UI Filter Result Dataclasses
# ============================================================================


@dataclass
class RunFilterResult:
    """Result from run filter screen."""

    statuses: set[Status] = field(default_factory=set)
    agents: set[str] = field(default_factory=set)
    text_search: str = ""
    time_range: str = "all"


@dataclass
class IssueFilterResult:
    """Result from issue filter screen."""

    statuses: set[IssueStatus] = field(default_factory=set)
    priorities: set[str] = field(default_factory=set)
    tags: set[str] = field(default_factory=set)
    tag_mode: str = "all"  # "all" (AND) or "any" (OR)
    text_search: str = ""
