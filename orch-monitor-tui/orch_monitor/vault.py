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
        self.runs_dir = vault_path / "runs"
        self._issue_cache: Dict[str, Issue] = {}

    def _scan_issues(self) -> Dict[str, Issue]:
        """Walk the vault directory tree to find all issue files.

        Similar to Go's scanIssues(), this walks the entire vault looking for
        .md files with `type: issue` frontmatter, skipping the runs directory.
        This implementation follows symlinks (like Go's walkWithSymlinks).
        """
        issues: Dict[str, Issue] = {}
        runs_dir = self.runs_dir
        visited: set[str] = set()

        def walk_with_symlinks(root: Path) -> None:
            """Walk directory tree following symlinks, avoiding cycles."""
            try:
                # Resolve symlinks to detect cycles
                real_path = str(root.resolve())
                if real_path in visited:
                    return
                visited.add(real_path)

                for entry in root.iterdir():
                    path = entry

                    # Skip the runs directory entirely
                    if path == runs_dir:
                        continue
                    try:
                        path.relative_to(runs_dir)
                        continue  # Path is inside runs_dir, skip it
                    except ValueError:
                        pass

                    if path.is_dir():
                        walk_with_symlinks(path)
                    elif path.suffix == ".md":
                        issue = self._parse_issue(path)
                        if issue:
                            issues[issue.id] = issue
            except (PermissionError, OSError):
                pass  # Skip directories we can't access

        walk_with_symlinks(self.vault_path)
        return issues

    def list_issues(
        self, include_resolved: bool = False, include_closed: bool = True
    ) -> List[Issue]:
        """List all issues in the vault."""
        # Rescan to get fresh data (matching Go behavior)
        self._issue_cache = self._scan_issues()

        issues = []
        for issue in self._issue_cache.values():
            if issue.status == IssueStatus.RESOLVED and not include_resolved:
                continue
            if issue.status == IssueStatus.CLOSED and not include_closed:
                continue
            issues.append(issue)

        return issues

    def get_issue(self, issue_id: str) -> Optional[Issue]:
        """Get a specific issue by ID."""
        # First check cache
        if issue_id in self._issue_cache:
            return self._issue_cache[issue_id]

        # Rescan in case file was added
        self._issue_cache = self._scan_issues()
        return self._issue_cache.get(issue_id)

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
        """Parse an issue markdown file.

        Only returns an Issue if the file has `type: issue` in its frontmatter,
        matching the Go implementation's behavior.
        """
        try:
            post = frontmatter.load(path)

            # Check if this is an issue file (must have type: issue)
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
