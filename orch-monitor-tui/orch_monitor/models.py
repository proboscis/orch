"""Data models for orch issues, runs, and events."""

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from pathlib import Path
from typing import Dict, List, Optional


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


class Phase(str, Enum):
    """Work phases."""

    PLAN = "plan"
    IMPLEMENT = "implement"
    TEST = "test"
    PR = "pr"
    REVIEW = "review"


class EventType(str, Enum):
    """Event types."""

    STATUS = "status"
    PHASE = "phase"
    ARTIFACT = "artifact"
    TEST = "test"
    NOTE = "note"


@dataclass
class Event:
    """A single event in a run."""

    timestamp: datetime
    type: EventType
    name: str
    attrs: Dict[str, str] = field(default_factory=dict)
    raw: str = ""

    @classmethod
    def parse(cls, line: str) -> Optional["Event"]:
        """Parse an event line from markdown.

        Format: - <timestamp> | <type> | <name> | key=value | key=value ...
        """
        line = line.strip()
        if not line.startswith("- "):
            return None

        parts = line[2:].split(" | ")
        if len(parts) < 3:
            return None

        try:
            timestamp = datetime.fromisoformat(parts[0])
            event_type = EventType(parts[1])
            name = parts[2]

            attrs = {}
            for part in parts[3:]:
                if "=" in part:
                    key, value = part.split("=", 1)
                    if value.startswith('"') and value.endswith('"'):
                        value = value[1:-1]
                    attrs[key.strip()] = value.strip()

            return cls(
                timestamp=timestamp,
                type=event_type,
                name=name,
                attrs=attrs,
                raw=line,
            )
        except (ValueError, KeyError):
            return None


@dataclass
class Run:
    """A single execution of an issue."""

    issue_id: str
    run_id: str
    path: Path
    status: Status = Status.QUEUED
    phase: Optional[Phase] = None
    events: List[Event] = field(default_factory=list)
    started_at: Optional[datetime] = None
    updated_at: Optional[datetime] = None
    agent: str = ""
    model: str = ""
    model_variant: str = ""
    branch: str = ""
    worktree_path: str = ""
    tmux_session: str = ""
    tmux_window_id: str = ""
    multiplexer: str = ""
    pr_url: str = ""
    server_port: int = 0
    opencode_session_id: str = ""
    continued_from: str = ""
    additions: int = 0
    deletions: int = 0

    def ref(self) -> str:
        """Return the run reference (ISSUE_ID#RUN_ID)."""
        return f"{self.issue_id}#{self.run_id}"

    def __repr__(self) -> str:
        """Safe repr that doesn't include full event list."""
        return (
            f"Run({self.ref()}, status={self.status.value}, events={len(self.events)})"
        )

    def __str__(self) -> str:
        """Safe string representation."""
        return self.ref()

    def short_id(self) -> str:
        """Return a 6-character hex identifier for the run (git-style)."""
        import hashlib

        h = hashlib.sha256(self.ref().encode()).hexdigest()
        return h[:6]

    def is_active(self) -> bool:
        active_states = {
            Status.QUEUED,
            Status.BOOTING,
            Status.RUNNING,
            Status.BLOCKED,
            Status.BLOCKED_API,
        }
        return self.status in active_states

    def elapsed_time(self) -> str:
        if not self.started_at or not isinstance(self.started_at, datetime):
            return "-"

        try:
            if self.is_active():
                delta = datetime.now() - self.started_at
            elif self.updated_at:
                delta = self.updated_at - self.started_at
            else:
                delta = datetime.now() - self.started_at
        except (TypeError, ValueError):
            return "-"

        total_seconds = int(delta.total_seconds())
        hours, remainder = divmod(total_seconds, 3600)
        minutes, seconds = divmod(remainder, 60)

        if hours > 0:
            return f"{hours}h{minutes}m"
        elif minutes > 0:
            return f"{minutes}m{seconds}s"
        else:
            return f"{seconds}s"


@dataclass
class Issue:
    """An issue specification."""

    id: str
    title: str = ""
    topic: str = ""
    summary: str = ""
    status: IssueStatus = IssueStatus.OPEN
    tags: List[str] = field(default_factory=list)
    body: str = ""
    path: Path = Path()
    frontmatter: Dict[str, str] = field(default_factory=dict)
    modified_at: Optional[datetime] = None

    def status_display(self) -> str:
        """Return a display string for status."""
        return self.status.value

    def tags_display(self) -> str:
        """Return a formatted string for tags display."""
        if not self.tags:
            return ""
        return " ".join(f"[{tag}]" for tag in self.tags)


def parse_tags(s: str) -> List[str]:
    """Parse a tags string from JSON/frontmatter.

    Supports formats: "[tag1, tag2]", "tag1, tag2", "tag1,tag2"
    """
    if not s:
        return []

    # Remove brackets if present
    s = s.strip()
    s = s.lstrip("[").rstrip("]")

    # Split by comma
    tags = [tag.strip() for tag in s.split(",") if tag.strip()]
    return tags


@dataclass
class FileChange:
    """A single file change with addition/deletion counts."""

    path: str
    additions: int
    deletions: int

    def display_str(self, max_path_width: int = 40) -> str:
        """Return a formatted display string for the file change."""
        # Truncate path if needed
        path = self.path
        if len(path) > max_path_width:
            path = "..." + path[-(max_path_width - 3) :]

        # Format additions/deletions
        add_str = f"+{self.additions}" if self.additions > 0 else ""
        del_str = f"-{self.deletions}" if self.deletions > 0 else ""

        return f"  {path:<{max_path_width}}  {add_str:>5}  {del_str:>5}"


@dataclass
class DiffStats:
    """Git diff statistics for a run."""

    files: List[FileChange] = field(default_factory=list)
    total_additions: int = 0
    total_deletions: int = 0

    @property
    def file_count(self) -> int:
        return len(self.files)

    def summary_str(self) -> str:
        """Return a summary string like '5 files, +99 -18'."""
        return f"{self.file_count} file(s), +{self.total_additions} -{self.total_deletions}"
