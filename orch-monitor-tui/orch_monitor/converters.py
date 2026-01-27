"""API to Model converters for orch-monitor TUI."""

from pathlib import Path

from .orch_api import Issue as ApiIssue
from .orch_api import IssueStatus as ApiIssueStatus
from .orch_api import Run as ApiRun
from .orch_api import RunStatus as ApiRunStatus
from .models import Issue, IssueStatus, Run, Status


def api_run_status_to_model(status: ApiRunStatus) -> Status:
    mapping = {
        ApiRunStatus.QUEUED: Status.QUEUED,
        ApiRunStatus.BOOTING: Status.BOOTING,
        ApiRunStatus.RUNNING: Status.RUNNING,
        ApiRunStatus.BLOCKED: Status.BLOCKED,
        ApiRunStatus.BLOCKED_API: Status.BLOCKED_API,
        ApiRunStatus.PR_OPEN: Status.PR_OPEN,
        ApiRunStatus.DONE: Status.DONE,
        ApiRunStatus.FAILED: Status.FAILED,
        ApiRunStatus.CANCELED: Status.CANCELED,
        ApiRunStatus.UNKNOWN: Status.UNKNOWN,
    }
    return mapping.get(status, Status.UNKNOWN)


def api_issue_status_to_model(status: ApiIssueStatus) -> IssueStatus:
    mapping = {
        ApiIssueStatus.OPEN: IssueStatus.OPEN,
        ApiIssueStatus.RESOLVED: IssueStatus.RESOLVED,
        ApiIssueStatus.CLOSED: IssueStatus.CLOSED,
    }
    return mapping.get(status, IssueStatus.OPEN)


def api_run_to_model(api_run: ApiRun) -> Run:
    return Run(
        issue_id=api_run.issue_id,
        run_id=api_run.run_id,
        path=Path(),
        status=api_run_status_to_model(api_run.status),
        agent=api_run.agent,
        model=api_run.model,
        branch=api_run.branch,
        worktree_path=api_run.worktree_path,
        pr_url=api_run.pr_url,
        started_at=api_run.started_at,
        updated_at=api_run.updated_at,
        tmux_session=api_run.tmux_session,
        multiplexer=api_run.multiplexer,
        server_port=api_run.server_port,
        opencode_session_id=api_run.opencode_session_id,
        additions=api_run.diff_stats.additions if api_run.diff_stats else 0,
        deletions=api_run.diff_stats.deletions if api_run.diff_stats else 0,
    )


def api_runs_to_model(api_runs: list[ApiRun]) -> list[Run]:
    return [api_run_to_model(r) for r in api_runs]


def api_issue_to_model(api_issue: ApiIssue) -> Issue:
    return Issue(
        id=api_issue.id,
        title=api_issue.title,
        summary=api_issue.summary,
        status=api_issue_status_to_model(api_issue.status),
        tags=api_issue.tags,
        body=api_issue.body,
        path=api_issue.path,
        modified_at=api_issue.modified_at,
    )


def api_issues_to_model(api_issues: list[ApiIssue]) -> list[Issue]:
    return [api_issue_to_model(i) for i in api_issues]
