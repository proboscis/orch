"""Vault reader for parsing issues and runs from markdown files."""

from datetime import datetime
from pathlib import Path
from typing import Dict, List, Optional

import frontmatter

from .models import Event, Issue, IssueStatus, Run, Status


class VaultReader:
    """Reader for orch vault directory structure."""

    def __init__(self, vault_path: Path):
        self.vault_path = vault_path
        self.issues_dir = vault_path / "issues"
        self.runs_dir = vault_path / "issues" / "runs"

    def list_issues(
        self, include_resolved: bool = False, include_closed: bool = True
    ) -> List[Issue]:
        """List all issues in the vault."""
        issues = []

        if not self.issues_dir.exists():
            return issues

        for issue_file in self.issues_dir.glob("*.md"):
            issue = self._parse_issue(issue_file)
            if issue:
                if issue.status == IssueStatus.RESOLVED and not include_resolved:
                    continue
                if issue.status == IssueStatus.CLOSED and not include_closed:
                    continue
                issues.append(issue)

        return issues

    def get_issue(self, issue_id: str) -> Optional[Issue]:
        """Get a specific issue by ID."""
        issue_file = self.issues_dir / f"{issue_id}.md"
        if not issue_file.exists():
            return None
        return self._parse_issue(issue_file)

    def list_runs(self, issue_id: Optional[str] = None) -> List[Run]:
        """List all runs, optionally filtered by issue ID."""
        runs = []

        if not self.runs_dir.exists():
            return runs

        if issue_id:
            issue_runs_dir = self.runs_dir / issue_id
            if issue_runs_dir.exists():
                for run_file in issue_runs_dir.glob("*.md"):
                    run = self._parse_run(run_file, issue_id)
                    if run:
                        runs.append(run)
        else:
            for issue_runs_dir in self.runs_dir.iterdir():
                if not issue_runs_dir.is_dir():
                    continue
                issue_id = issue_runs_dir.name
                for run_file in issue_runs_dir.glob("*.md"):
                    run = self._parse_run(run_file, issue_id)
                    if run:
                        runs.append(run)

        return runs

    def get_run(self, issue_id: str, run_id: str) -> Optional[Run]:
        """Get a specific run by issue ID and run ID."""
        run_file = self.runs_dir / issue_id / f"{run_id}.md"
        if not run_file.exists():
            return None
        return self._parse_run(run_file, issue_id)

    def get_run_content(self, issue_id: str, run_id: str) -> str:
        """Get the full content of a run file."""
        run_file = self.runs_dir / issue_id / f"{run_id}.md"
        if not run_file.exists():
            return ""
        return run_file.read_text()

    def _parse_issue(self, path: Path) -> Optional[Issue]:
        """Parse an issue markdown file."""
        try:
            post = frontmatter.load(path)

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

    def _parse_run(self, path: Path, issue_id: str) -> Optional[Run]:
        """Parse a run markdown file."""
        try:
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
