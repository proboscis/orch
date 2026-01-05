"""Data models for orch monitor."""

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any


class Status(str, Enum):
    """Run operational lifecycle states."""

    QUEUED = "queued"
    BOOTING = "booting"
    RUNNING = "running"
    BLOCKED = "blocked"
    BLOCKED_API = "blocked_api"
    PR_OPEN = "pr_open"
    DONE = "done"
    FAILED = "failed"
    CANCELED = "canceled"
    UNKNOWN = "unknown"


class IssueStatus(str, Enum):
    """Issue resolution states."""

    OPEN = "open"
    RESOLVED = "resolved"
    CLOSED = "closed"


class EventType(str, Enum):
    """Event types."""

    STATUS = "status"
    PHASE = "phase"
    ARTIFACT = "artifact"
    TEST = "test"
    NOTE = "note"


@dataclass
class Event:
    """Represents a single event in a run."""

    timestamp: datetime
    type: EventType
    name: str
    attrs: dict[str, str] = field(default_factory=dict)
    raw: str = ""


@dataclass
class Run:
    """Represents a single execution of an issue."""

    issue_id: str
    run_id: str
    path: str
    status: Status = Status.QUEUED
    phase: str = ""
    events: list[Event] = field(default_factory=list)
    started_at: datetime | None = None
    updated_at: datetime | None = None
    agent: str = ""
    model: str = ""
    branch: str = ""
    worktree_path: str = ""
    tmux_session: str = ""
    pr_url: str = ""
    continued_from: str = ""

    @property
    def ref(self) -> str:
        """Get the run reference (issue_id#run_id)."""
        return f"{self.issue_id}#{self.run_id}"

    @property
    def elapsed(self) -> str:
        """Get elapsed time since start."""
        if not self.started_at:
            return ""
        delta = (self.updated_at or datetime.now()) - self.started_at
        hours, remainder = divmod(int(delta.total_seconds()), 3600)
        minutes, seconds = divmod(remainder, 60)
        if hours > 0:
            return f"{hours}h{minutes}m"
        elif minutes > 0:
            return f"{minutes}m{seconds}s"
        else:
            return f"{seconds}s"


@dataclass
class Issue:
    """Represents an issue specification."""

    id: str
    title: str
    topic: str = ""
    summary: str = ""
    status: IssueStatus = IssueStatus.OPEN
    body: str = ""
    path: str = ""
    frontmatter: dict[str, Any] = field(default_factory=dict)
