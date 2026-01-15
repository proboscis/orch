"""Vault reader for parsing issues and runs from markdown files.

This module provides a VaultReader class that reads issues and runs from the
orch vault directory structure. It uses a Rust backend via PyO3 for performance
and to match the Go implementation's vault scanning behavior.

If the Rust extension is not available, it falls back to a pure Python
implementation (which may have limited functionality).
"""

from pathlib import Path
from typing import Any, Dict, List, Optional

from .models import Event, EventType, Issue, IssueStatus, Run, Status

# Try to import the Rust backend
try:
    from orch_vault_rs import VaultReader as RustVaultReader

    _HAS_RUST_BACKEND = True
except ImportError:
    _HAS_RUST_BACKEND = False
    RustVaultReader = None  # type: ignore


class VaultReader:
    """Reader for orch vault directory structure.

    Uses Rust backend via PyO3 for vault scanning that matches the Go
    implementation's behavior (walks entire vault for issues, not just issues/).

    Falls back to pure Python implementation if Rust extension is not available.
    """

    def __init__(self, vault_path: Path):
        self.vault_path = vault_path
        self._rust_reader: Any = None

        if _HAS_RUST_BACKEND and RustVaultReader is not None:
            self._rust_reader = RustVaultReader(str(vault_path))
        else:
            # Fallback paths for pure Python implementation
            self.issues_dir = vault_path / "issues"
            self.runs_dir = vault_path / "runs"

    def list_issues(
        self, include_resolved: bool = False, include_closed: bool = True
    ) -> List[Issue]:
        """List all issues in the vault.

        When using Rust backend, walks the entire vault directory to find
        files with type: issue frontmatter (matching Go behavior).
        """
        if self._rust_reader:
            rust_issues = self._rust_reader.list_issues(
                include_resolved, include_closed
            )
            return [_convert_rust_issue(ri) for ri in rust_issues]
        else:
            return self._list_issues_python(include_resolved, include_closed)

    def get_issue(self, issue_id: str) -> Optional[Issue]:
        """Get a specific issue by ID."""
        if self._rust_reader:
            rust_issue = self._rust_reader.get_issue(issue_id)
            return _convert_rust_issue(rust_issue) if rust_issue else None
        else:
            return self._get_issue_python(issue_id)

    def list_runs(self, issue_id: Optional[str] = None) -> List[Run]:
        """List all runs, optionally filtered by issue ID."""
        if self._rust_reader:
            rust_runs = self._rust_reader.list_runs(issue_id)
            return [_convert_rust_run(rr) for rr in rust_runs]
        else:
            return self._list_runs_python(issue_id)

    def get_run(self, issue_id: str, run_id: str) -> Optional[Run]:
        """Get a specific run by issue ID and run ID."""
        if self._rust_reader:
            rust_run = self._rust_reader.get_run(issue_id, run_id)
            return _convert_rust_run(rust_run) if rust_run else None
        else:
            return self._get_run_python(issue_id, run_id)

    def get_run_content(self, issue_id: str, run_id: str) -> str:
        """Get the full content of a run file."""
        if self._rust_reader:
            return self._rust_reader.get_run_content(issue_id, run_id)
        else:
            return self._get_run_content_python(issue_id, run_id)

    # -------------------------------------------------------------------------
    # Pure Python fallback implementation
    # -------------------------------------------------------------------------

    def _list_issues_python(
        self, include_resolved: bool = False, include_closed: bool = True
    ) -> List[Issue]:
        """Pure Python fallback for list_issues."""
        issues = []

        if not self.issues_dir.exists():
            return issues

        for issue_file in self.issues_dir.glob("*.md"):
            issue = self._parse_issue_python(issue_file)
            if issue:
                if issue.status == IssueStatus.RESOLVED and not include_resolved:
                    continue
                if issue.status == IssueStatus.CLOSED and not include_closed:
                    continue
                issues.append(issue)

        return issues

    def _get_issue_python(self, issue_id: str) -> Optional[Issue]:
        """Pure Python fallback for get_issue."""
        issue_file = self.issues_dir / f"{issue_id}.md"
        if not issue_file.exists():
            return None
        return self._parse_issue_python(issue_file)

    def _list_runs_python(self, issue_id: Optional[str] = None) -> List[Run]:
        """Pure Python fallback for list_runs."""
        runs = []

        if not self.runs_dir.exists():
            return runs

        if issue_id:
            issue_runs_dir = self.runs_dir / issue_id
            if issue_runs_dir.exists():
                for run_file in issue_runs_dir.glob("*.md"):
                    run = self._parse_run_python(run_file, issue_id)
                    if run:
                        runs.append(run)
        else:
            for issue_runs_dir in self.runs_dir.iterdir():
                if not issue_runs_dir.is_dir():
                    continue
                issue_id = issue_runs_dir.name
                for run_file in issue_runs_dir.glob("*.md"):
                    run = self._parse_run_python(run_file, issue_id)
                    if run:
                        runs.append(run)

        return runs

    def _get_run_python(self, issue_id: str, run_id: str) -> Optional[Run]:
        """Pure Python fallback for get_run."""
        run_file = self.runs_dir / issue_id / f"{run_id}.md"
        if not run_file.exists():
            return None
        return self._parse_run_python(run_file, issue_id)

    def _get_run_content_python(self, issue_id: str, run_id: str) -> str:
        """Pure Python fallback for get_run_content."""
        run_file = self.runs_dir / issue_id / f"{run_id}.md"
        if not run_file.exists():
            return ""
        return run_file.read_text()

    def _parse_issue_python(self, path: Path) -> Optional[Issue]:
        """Parse an issue markdown file (Python fallback)."""
        try:
            import frontmatter

            post = frontmatter.load(path)

            # Check if this is an issue file
            if post.metadata.get("type") != "issue":
                return None

            issue_id = post.metadata.get("id", path.stem)
            title = post.metadata.get("title", "")
            status_str = post.metadata.get("status", "open")

            try:
                status = IssueStatus(status_str)
            except ValueError:
                status = IssueStatus.OPEN

            body = post.content

            summary = ""
            if body:
                lines = [
                    line.strip()
                    for line in body.split("\n")
                    if line.strip() and not line.startswith("#")
                ]
                if lines:
                    summary = lines[0][:100]

            return Issue(
                id=issue_id,
                title=title,
                topic=title[:50] if title else issue_id,
                summary=summary,
                status=status,
                body=body,
                path=path,
                frontmatter=dict(post.metadata),
            )
        except Exception:
            return None

    def _parse_run_python(self, path: Path, issue_id: str) -> Optional[Run]:
        """Parse a run markdown file (Python fallback)."""
        try:
            import frontmatter

            post = frontmatter.load(path)

            run_id = post.metadata.get("run", path.stem)
            agent = post.metadata.get("agent", "")
            continued_from = post.metadata.get("continued_from", "")

            run = Run(
                issue_id=issue_id,
                run_id=run_id,
                path=path,
                agent=agent,
                continued_from=continued_from,
            )

            in_events_section = False
            for line in post.content.split("\n"):
                stripped = line.strip()

                if stripped == "# Events":
                    in_events_section = True
                    continue

                if in_events_section and stripped.startswith("- "):
                    event = Event.parse(line)
                    if event:
                        run.events.append(event)

            run.derive_state()

            return run
        except Exception:
            return None


def _convert_rust_issue(rust_issue) -> Issue:
    """Convert a Rust Issue to a Python Issue dataclass."""
    try:
        status = IssueStatus(rust_issue.status)
    except ValueError:
        status = IssueStatus.OPEN

    return Issue(
        id=rust_issue.id,
        title=rust_issue.title,
        topic=rust_issue.topic,
        summary=rust_issue.summary,
        status=status,
        body=rust_issue.body,
        path=Path(rust_issue.path),
        frontmatter=dict(rust_issue.frontmatter),
    )


def _convert_rust_event(rust_event) -> Event:
    """Convert a Rust Event to a Python Event dataclass."""
    from datetime import datetime

    try:
        timestamp = datetime.fromisoformat(rust_event.timestamp.replace("Z", "+00:00"))
    except (ValueError, AttributeError):
        timestamp = datetime.now()

    try:
        event_type = EventType(rust_event.event_type)
    except ValueError:
        event_type = EventType.NOTE

    return Event(
        timestamp=timestamp,
        type=event_type,
        name=rust_event.name,
        attrs=dict(rust_event.attrs),
        raw=rust_event.raw,
    )


def _convert_rust_run(rust_run) -> Run:
    """Convert a Rust Run to a Python Run dataclass."""
    from datetime import datetime

    try:
        status = Status(rust_run.status)
    except ValueError:
        status = Status.UNKNOWN

    # Convert events
    events = [_convert_rust_event(e) for e in rust_run.events]

    # Parse timestamps
    started_at = None
    updated_at = None
    if rust_run.started_at:
        try:
            started_at = datetime.fromisoformat(
                rust_run.started_at.replace("Z", "+00:00")
            )
        except (ValueError, AttributeError):
            pass
    if rust_run.updated_at:
        try:
            updated_at = datetime.fromisoformat(
                rust_run.updated_at.replace("Z", "+00:00")
            )
        except (ValueError, AttributeError):
            pass

    run = Run(
        issue_id=rust_run.issue_id,
        run_id=rust_run.run_id,
        path=Path(rust_run.path),
        status=status,
        events=events,
        started_at=started_at,
        updated_at=updated_at,
        agent=rust_run.agent,
        model=rust_run.model,
        model_variant=rust_run.model_variant,
        branch=rust_run.branch,
        worktree_path=rust_run.worktree_path,
        tmux_session=rust_run.tmux_session,
        tmux_window_id=rust_run.tmux_window_id,
        pr_url=rust_run.pr_url,
        server_port=rust_run.server_port,
        opencode_session_id=rust_run.opencode_session_id,
        continued_from=rust_run.continued_from,
    )

    # Phase needs to be set separately
    if rust_run.phase:
        from .models import Phase

        try:
            run.phase = Phase(rust_run.phase)
        except ValueError:
            pass

    return run
