import os
import re
from datetime import datetime
from pathlib import Path
from typing import Any

from .models import Event, EventType, Issue, IssueStatus, Run, Status


EVENT_LINE_REGEX = re.compile(r"^-\s+(\S+)\s+\|\s+(\w+)\s+\|\s+(\S+)(.*)$")
ATTR_REGEX = re.compile(r'(\w+)=(?:"([^"]*)"|(\S+))')


def parse_frontmatter(content: str) -> tuple[dict[str, Any], str]:
    lines = content.split("\n")
    if not lines or lines[0].strip() != "---":
        return {}, content

    frontmatter = {}
    body_start = 0

    for i in range(1, len(lines)):
        line = lines[i]
        if line.strip() == "---":
            body_start = i + 1
            break

        if ":" in line:
            key, _, value = line.partition(":")
            frontmatter[key.strip()] = value.strip()

    body = "\n".join(lines[body_start:])
    return frontmatter, body


def parse_event(line: str) -> Event | None:
    line = line.strip()
    if not line.startswith("- "):
        return None

    match = EVENT_LINE_REGEX.match(line)
    if not match:
        return None

    try:
        timestamp = datetime.fromisoformat(match.group(1).replace("Z", "+00:00"))
    except ValueError:
        return None

    event = Event(
        timestamp=timestamp,
        type=EventType(match.group(2)),
        name=match.group(3),
        attrs={},
        raw=line,
    )

    if len(match.groups()) > 3 and match.group(4):
        attr_matches = ATTR_REGEX.findall(match.group(4))
        for attr_match in attr_matches:
            key = attr_match[0]
            value = attr_match[1] if attr_match[1] else attr_match[2]
            event.attrs[key] = value

    return event


def parse_issue_file(path: Path) -> Issue | None:
    try:
        content = path.read_text()
    except Exception:
        return None

    frontmatter, body = parse_frontmatter(content)

    if frontmatter.get("type") != "issue":
        return None

    issue_id = frontmatter.get("id") or path.stem

    title = ""
    summary = ""
    topic = ""

    for line in body.split("\n"):
        line = line.strip()
        if line.startswith("# "):
            if not title:
                title = line[2:].strip()
        elif line.startswith("## "):
            if not summary:
                summary = line[3:].strip()
        elif line and not title:
            title = line
            break

    topic = frontmatter.get("topic", "")
    summary = frontmatter.get("summary", summary)

    status_str = frontmatter.get("status", "open")
    try:
        status = IssueStatus(status_str)
    except ValueError:
        status = IssueStatus.OPEN

    return Issue(
        id=issue_id,
        title=title,
        topic=topic,
        summary=summary,
        status=status,
        body=body,
        path=str(path),
        frontmatter=frontmatter,
    )


def parse_run_file(path: Path) -> Run | None:
    try:
        content = path.read_text()
    except Exception:
        return None

    frontmatter, body = parse_frontmatter(content)

    issue_id = frontmatter.get("issue", "")
    run_id = frontmatter.get("run", "") or path.stem

    if not issue_id:
        parts = path.parent.name.split("/")
        if parts:
            issue_id = parts[-1]

    events = []
    for line in body.split("\n"):
        event = parse_event(line)
        if event:
            events.append(event)

    run = Run(
        issue_id=issue_id,
        run_id=run_id,
        path=str(path),
        events=events,
        continued_from=frontmatter.get("continued_from", ""),
    )

    derive_run_state(run)
    return run


def derive_run_state(run: Run) -> None:
    for event in reversed(run.events):
        if event.type == EventType.STATUS:
            try:
                run.status = Status(event.name)
            except ValueError:
                run.status = Status.UNKNOWN
            break

    for event in reversed(run.events):
        if event.type == EventType.PHASE:
            run.phase = event.name
            break

    artifacts: dict[str, dict[str, str]] = {}
    for event in run.events:
        if event.type == EventType.ARTIFACT:
            artifacts[event.name] = event.attrs

    if "worktree" in artifacts:
        run.worktree_path = artifacts["worktree"].get("path", "")
    if "branch" in artifacts:
        run.branch = artifacts["branch"].get("name", "")
    if "session" in artifacts:
        run.tmux_session = artifacts["session"].get("name", "")
    if "pr" in artifacts:
        run.pr_url = artifacts["pr"].get("url", "")
    if "agent_model" in artifacts:
        run.agent = artifacts["agent_model"].get("agent", "")
        run.model = artifacts["agent_model"].get("model", "")

    if run.events:
        run.started_at = run.events[0].timestamp
        run.updated_at = run.events[-1].timestamp


class VaultStore:
    def __init__(self, vault_path: str):
        self.vault_path = Path(vault_path)
        if not self.vault_path.exists():
            raise ValueError(f"Vault path does not exist: {vault_path}")

    def list_issues(self) -> list[Issue]:
        issues = []
        runs_dir = self.vault_path / "runs"

        for md_file in self.vault_path.rglob("*.md"):
            if md_file.is_relative_to(runs_dir):
                continue

            issue = parse_issue_file(md_file)
            if issue:
                issues.append(issue)

        return sorted(issues, key=lambda i: i.id)

    def list_runs(self, issue_id: str | None = None) -> list[Run]:
        runs = []
        runs_dir = self.vault_path / "runs"

        if not runs_dir.exists():
            return runs

        if issue_id:
            issue_run_dir = runs_dir / issue_id
            if issue_run_dir.exists():
                for md_file in issue_run_dir.glob("*.md"):
                    run = parse_run_file(md_file)
                    if run:
                        runs.append(run)
        else:
            for md_file in runs_dir.rglob("*.md"):
                run = parse_run_file(md_file)
                if run:
                    runs.append(run)

        return sorted(runs, key=lambda r: r.updated_at or datetime.min, reverse=True)

    def get_issue(self, issue_id: str) -> Issue | None:
        for issue in self.list_issues():
            if issue.id == issue_id:
                return issue
        return None

    def get_run(self, issue_id: str, run_id: str) -> Run | None:
        runs_dir = self.vault_path / "runs" / issue_id
        run_file = runs_dir / f"{run_id}.md"

        if run_file.exists():
            return parse_run_file(run_file)
        return None
