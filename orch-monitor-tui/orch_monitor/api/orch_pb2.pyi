from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RunStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RUN_STATUS_UNSPECIFIED: _ClassVar[RunStatus]
    RUN_STATUS_QUEUED: _ClassVar[RunStatus]
    RUN_STATUS_BOOTING: _ClassVar[RunStatus]
    RUN_STATUS_RUNNING: _ClassVar[RunStatus]
    RUN_STATUS_BLOCKED: _ClassVar[RunStatus]
    RUN_STATUS_BLOCKED_API: _ClassVar[RunStatus]
    RUN_STATUS_PR_OPEN: _ClassVar[RunStatus]
    RUN_STATUS_DONE: _ClassVar[RunStatus]
    RUN_STATUS_FAILED: _ClassVar[RunStatus]
    RUN_STATUS_CANCELED: _ClassVar[RunStatus]

class IssueStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ISSUE_STATUS_UNSPECIFIED: _ClassVar[IssueStatus]
    ISSUE_STATUS_OPEN: _ClassVar[IssueStatus]
    ISSUE_STATUS_RESOLVED: _ClassVar[IssueStatus]
    ISSUE_STATUS_CLOSED: _ClassVar[IssueStatus]

class BranchState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    BRANCH_STATE_UNSPECIFIED: _ClassVar[BranchState]
    BRANCH_STATE_CLEAN: _ClassVar[BranchState]
    BRANCH_STATE_DIRTY: _ClassVar[BranchState]
    BRANCH_STATE_MERGED: _ClassVar[BranchState]
    BRANCH_STATE_CONFLICT: _ClassVar[BranchState]

class Multiplexer(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MULTIPLEXER_UNSPECIFIED: _ClassVar[Multiplexer]
    MULTIPLEXER_TMUX: _ClassVar[Multiplexer]
    MULTIPLEXER_ZELLIJ: _ClassVar[Multiplexer]
RUN_STATUS_UNSPECIFIED: RunStatus
RUN_STATUS_QUEUED: RunStatus
RUN_STATUS_BOOTING: RunStatus
RUN_STATUS_RUNNING: RunStatus
RUN_STATUS_BLOCKED: RunStatus
RUN_STATUS_BLOCKED_API: RunStatus
RUN_STATUS_PR_OPEN: RunStatus
RUN_STATUS_DONE: RunStatus
RUN_STATUS_FAILED: RunStatus
RUN_STATUS_CANCELED: RunStatus
ISSUE_STATUS_UNSPECIFIED: IssueStatus
ISSUE_STATUS_OPEN: IssueStatus
ISSUE_STATUS_RESOLVED: IssueStatus
ISSUE_STATUS_CLOSED: IssueStatus
BRANCH_STATE_UNSPECIFIED: BranchState
BRANCH_STATE_CLEAN: BranchState
BRANCH_STATE_DIRTY: BranchState
BRANCH_STATE_MERGED: BranchState
BRANCH_STATE_CONFLICT: BranchState
MULTIPLEXER_UNSPECIFIED: Multiplexer
MULTIPLEXER_TMUX: Multiplexer
MULTIPLEXER_ZELLIJ: Multiplexer

class DiffStats(_message.Message):
    __slots__ = ("additions", "deletions", "files_changed", "files")
    ADDITIONS_FIELD_NUMBER: _ClassVar[int]
    DELETIONS_FIELD_NUMBER: _ClassVar[int]
    FILES_CHANGED_FIELD_NUMBER: _ClassVar[int]
    FILES_FIELD_NUMBER: _ClassVar[int]
    additions: int
    deletions: int
    files_changed: int
    files: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, additions: _Optional[int] = ..., deletions: _Optional[int] = ..., files_changed: _Optional[int] = ..., files: _Optional[_Iterable[str]] = ...) -> None: ...

class Run(_message.Message):
    __slots__ = ("issue_id", "run_id", "status", "agent", "model", "branch", "worktree_path", "pr_url", "started_at_unix", "updated_at_unix", "elapsed_seconds", "elapsed_display", "diff_stats", "branch_state", "tmux_session", "multiplexer", "server_port", "opencode_session_id", "continued_from")
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    BRANCH_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_PATH_FIELD_NUMBER: _ClassVar[int]
    PR_URL_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_UNIX_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_UNIX_FIELD_NUMBER: _ClassVar[int]
    ELAPSED_SECONDS_FIELD_NUMBER: _ClassVar[int]
    ELAPSED_DISPLAY_FIELD_NUMBER: _ClassVar[int]
    DIFF_STATS_FIELD_NUMBER: _ClassVar[int]
    BRANCH_STATE_FIELD_NUMBER: _ClassVar[int]
    TMUX_SESSION_FIELD_NUMBER: _ClassVar[int]
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    SERVER_PORT_FIELD_NUMBER: _ClassVar[int]
    OPENCODE_SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTINUED_FROM_FIELD_NUMBER: _ClassVar[int]
    issue_id: str
    run_id: str
    status: RunStatus
    agent: str
    model: str
    branch: str
    worktree_path: str
    pr_url: str
    started_at_unix: int
    updated_at_unix: int
    elapsed_seconds: int
    elapsed_display: str
    diff_stats: DiffStats
    branch_state: BranchState
    tmux_session: str
    multiplexer: Multiplexer
    server_port: int
    opencode_session_id: str
    continued_from: str
    def __init__(self, issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., status: _Optional[_Union[RunStatus, str]] = ..., agent: _Optional[str] = ..., model: _Optional[str] = ..., branch: _Optional[str] = ..., worktree_path: _Optional[str] = ..., pr_url: _Optional[str] = ..., started_at_unix: _Optional[int] = ..., updated_at_unix: _Optional[int] = ..., elapsed_seconds: _Optional[int] = ..., elapsed_display: _Optional[str] = ..., diff_stats: _Optional[_Union[DiffStats, _Mapping]] = ..., branch_state: _Optional[_Union[BranchState, str]] = ..., tmux_session: _Optional[str] = ..., multiplexer: _Optional[_Union[Multiplexer, str]] = ..., server_port: _Optional[int] = ..., opencode_session_id: _Optional[str] = ..., continued_from: _Optional[str] = ...) -> None: ...

class Issue(_message.Message):
    __slots__ = ("id", "title", "summary", "status", "tags", "body", "path", "modified_at_unix")
    ID_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MODIFIED_AT_UNIX_FIELD_NUMBER: _ClassVar[int]
    id: str
    title: str
    summary: str
    status: IssueStatus
    tags: _containers.RepeatedScalarFieldContainer[str]
    body: str
    path: str
    modified_at_unix: int
    def __init__(self, id: _Optional[str] = ..., title: _Optional[str] = ..., summary: _Optional[str] = ..., status: _Optional[_Union[IssueStatus, str]] = ..., tags: _Optional[_Iterable[str]] = ..., body: _Optional[str] = ..., path: _Optional[str] = ..., modified_at_unix: _Optional[int] = ...) -> None: ...

class Event(_message.Message):
    __slots__ = ("timestamp_unix", "type", "name", "attrs")
    class AttrsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    TIMESTAMP_UNIX_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    ATTRS_FIELD_NUMBER: _ClassVar[int]
    timestamp_unix: int
    type: str
    name: str
    attrs: _containers.ScalarMap[str, str]
    def __init__(self, timestamp_unix: _Optional[int] = ..., type: _Optional[str] = ..., name: _Optional[str] = ..., attrs: _Optional[_Mapping[str, str]] = ...) -> None: ...

class PingRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class PingResponse(_message.Message):
    __slots__ = ("ok", "version")
    OK_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    ok: bool
    version: str
    def __init__(self, ok: _Optional[bool] = ..., version: _Optional[str] = ...) -> None: ...

class ListRunsRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "status", "agent", "text_search", "time_range", "limit", "cursor")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    TEXT_SEARCH_FIELD_NUMBER: _ClassVar[int]
    TIME_RANGE_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    status: _containers.RepeatedScalarFieldContainer[RunStatus]
    agent: str
    text_search: str
    time_range: str
    limit: int
    cursor: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., status: _Optional[_Iterable[_Union[RunStatus, str]]] = ..., agent: _Optional[str] = ..., text_search: _Optional[str] = ..., time_range: _Optional[str] = ..., limit: _Optional[int] = ..., cursor: _Optional[str] = ...) -> None: ...

class ListRunsResponse(_message.Message):
    __slots__ = ("runs", "total", "next_cursor")
    RUNS_FIELD_NUMBER: _ClassVar[int]
    TOTAL_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    runs: _containers.RepeatedCompositeFieldContainer[Run]
    total: int
    next_cursor: str
    def __init__(self, runs: _Optional[_Iterable[_Union[Run, _Mapping]]] = ..., total: _Optional[int] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class GetRunRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

class GetRunResponse(_message.Message):
    __slots__ = ("run", "events")
    RUN_FIELD_NUMBER: _ClassVar[int]
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    run: Run
    events: _containers.RepeatedCompositeFieldContainer[Event]
    def __init__(self, run: _Optional[_Union[Run, _Mapping]] = ..., events: _Optional[_Iterable[_Union[Event, _Mapping]]] = ...) -> None: ...

class StartRunRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "agent", "model", "model_variant", "base_branch")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MODEL_VARIANT_FIELD_NUMBER: _ClassVar[int]
    BASE_BRANCH_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    agent: str
    model: str
    model_variant: str
    base_branch: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., agent: _Optional[str] = ..., model: _Optional[str] = ..., model_variant: _Optional[str] = ..., base_branch: _Optional[str] = ...) -> None: ...

class StartRunResponse(_message.Message):
    __slots__ = ("run_id", "branch", "worktree_path", "tmux_session")
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    BRANCH_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_PATH_FIELD_NUMBER: _ClassVar[int]
    TMUX_SESSION_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    branch: str
    worktree_path: str
    tmux_session: str
    def __init__(self, run_id: _Optional[str] = ..., branch: _Optional[str] = ..., worktree_path: _Optional[str] = ..., tmux_session: _Optional[str] = ...) -> None: ...

class StopRunRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

class StopRunResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ResolveRunRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

class ResolveRunResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListIssuesRequest(_message.Message):
    __slots__ = ("issues_root", "status", "tags", "tags_mode", "text_search", "limit", "cursor")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    TAGS_MODE_FIELD_NUMBER: _ClassVar[int]
    TEXT_SEARCH_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    status: _containers.RepeatedScalarFieldContainer[IssueStatus]
    tags: _containers.RepeatedScalarFieldContainer[str]
    tags_mode: str
    text_search: str
    limit: int
    cursor: str
    def __init__(self, issues_root: _Optional[str] = ..., status: _Optional[_Iterable[_Union[IssueStatus, str]]] = ..., tags: _Optional[_Iterable[str]] = ..., tags_mode: _Optional[str] = ..., text_search: _Optional[str] = ..., limit: _Optional[int] = ..., cursor: _Optional[str] = ...) -> None: ...

class ListIssuesResponse(_message.Message):
    __slots__ = ("issues", "total", "next_cursor")
    ISSUES_FIELD_NUMBER: _ClassVar[int]
    TOTAL_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    issues: _containers.RepeatedCompositeFieldContainer[Issue]
    total: int
    next_cursor: str
    def __init__(self, issues: _Optional[_Iterable[_Union[Issue, _Mapping]]] = ..., total: _Optional[int] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class GetIssueRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ...) -> None: ...

class GetIssueResponse(_message.Message):
    __slots__ = ("issue",)
    ISSUE_FIELD_NUMBER: _ClassVar[int]
    issue: Issue
    def __init__(self, issue: _Optional[_Union[Issue, _Mapping]] = ...) -> None: ...

class CreateIssueRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "title", "body")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    title: str
    body: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., title: _Optional[str] = ..., body: _Optional[str] = ...) -> None: ...

class CreateIssueResponse(_message.Message):
    __slots__ = ("path",)
    PATH_FIELD_NUMBER: _ClassVar[int]
    path: str
    def __init__(self, path: _Optional[str] = ...) -> None: ...

class CloseIssueRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ...) -> None: ...

class CloseIssueResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetControlAgentLaunchRequest(_message.Message):
    __slots__ = ("project_root", "agent", "new_session")
    PROJECT_ROOT_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    NEW_SESSION_FIELD_NUMBER: _ClassVar[int]
    project_root: str
    agent: str
    new_session: bool
    def __init__(self, project_root: _Optional[str] = ..., agent: _Optional[str] = ..., new_session: _Optional[bool] = ...) -> None: ...

class GetControlAgentLaunchResponse(_message.Message):
    __slots__ = ("command", "prompt_file", "port", "session_id")
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FILE_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    command: str
    prompt_file: str
    port: int
    session_id: str
    def __init__(self, command: _Optional[str] = ..., prompt_file: _Optional[str] = ..., port: _Optional[int] = ..., session_id: _Optional[str] = ...) -> None: ...

class GetAttachInfoRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

class GetAttachInfoResponse(_message.Message):
    __slots__ = ("command", "multiplexer", "session_name", "worktree_path")
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_PATH_FIELD_NUMBER: _ClassVar[int]
    command: _containers.RepeatedScalarFieldContainer[str]
    multiplexer: Multiplexer
    session_name: str
    worktree_path: str
    def __init__(self, command: _Optional[_Iterable[str]] = ..., multiplexer: _Optional[_Union[Multiplexer, str]] = ..., session_name: _Optional[str] = ..., worktree_path: _Optional[str] = ...) -> None: ...

class CaptureSessionRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

class CaptureSessionResponse(_message.Message):
    __slots__ = ("content", "timestamp_unix", "source")
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_UNIX_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    content: str
    timestamp_unix: int
    source: str
    def __init__(self, content: _Optional[str] = ..., timestamp_unix: _Optional[int] = ..., source: _Optional[str] = ...) -> None: ...

class SendMessageRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id", "message")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    message: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., message: _Optional[str] = ...) -> None: ...

class SendMessageResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetDiffStatsRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

class GetDiffStatsResponse(_message.Message):
    __slots__ = ("diff_stats",)
    DIFF_STATS_FIELD_NUMBER: _ClassVar[int]
    diff_stats: DiffStats
    def __init__(self, diff_stats: _Optional[_Union[DiffStats, _Mapping]] = ...) -> None: ...

class GetBranchStateRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

class GetBranchStateResponse(_message.Message):
    __slots__ = ("state",)
    STATE_FIELD_NUMBER: _ClassVar[int]
    state: BranchState
    def __init__(self, state: _Optional[_Union[BranchState, str]] = ...) -> None: ...

class GetDiffRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

class GetDiffResponse(_message.Message):
    __slots__ = ("diff",)
    DIFF_FIELD_NUMBER: _ClassVar[int]
    diff: str
    def __init__(self, diff: _Optional[str] = ...) -> None: ...

class RegisterMonitorRequest(_message.Message):
    __slots__ = ("pid", "monitor_type", "view", "project", "session_name")
    PID_FIELD_NUMBER: _ClassVar[int]
    MONITOR_TYPE_FIELD_NUMBER: _ClassVar[int]
    VIEW_FIELD_NUMBER: _ClassVar[int]
    PROJECT_FIELD_NUMBER: _ClassVar[int]
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    pid: int
    monitor_type: str
    view: str
    project: str
    session_name: str
    def __init__(self, pid: _Optional[int] = ..., monitor_type: _Optional[str] = ..., view: _Optional[str] = ..., project: _Optional[str] = ..., session_name: _Optional[str] = ...) -> None: ...

class RegisterMonitorResponse(_message.Message):
    __slots__ = ("monitor_id",)
    MONITOR_ID_FIELD_NUMBER: _ClassVar[int]
    monitor_id: str
    def __init__(self, monitor_id: _Optional[str] = ...) -> None: ...

class UnregisterMonitorRequest(_message.Message):
    __slots__ = ("monitor_id",)
    MONITOR_ID_FIELD_NUMBER: _ClassVar[int]
    monitor_id: str
    def __init__(self, monitor_id: _Optional[str] = ...) -> None: ...

class UnregisterMonitorResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HeartbeatRequest(_message.Message):
    __slots__ = ("monitor_id",)
    MONITOR_ID_FIELD_NUMBER: _ClassVar[int]
    monitor_id: str
    def __init__(self, monitor_id: _Optional[str] = ...) -> None: ...

class HeartbeatResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class Request(_message.Message):
    __slots__ = ("ping", "list_runs", "get_run", "start_run", "stop_run", "resolve_run", "list_issues", "get_issue", "create_issue", "close_issue", "get_control_agent_launch", "get_attach_info", "capture_session", "send_message", "get_diff_stats", "get_branch_state", "get_diff", "register_monitor", "unregister_monitor", "heartbeat")
    PING_FIELD_NUMBER: _ClassVar[int]
    LIST_RUNS_FIELD_NUMBER: _ClassVar[int]
    GET_RUN_FIELD_NUMBER: _ClassVar[int]
    START_RUN_FIELD_NUMBER: _ClassVar[int]
    STOP_RUN_FIELD_NUMBER: _ClassVar[int]
    RESOLVE_RUN_FIELD_NUMBER: _ClassVar[int]
    LIST_ISSUES_FIELD_NUMBER: _ClassVar[int]
    GET_ISSUE_FIELD_NUMBER: _ClassVar[int]
    CREATE_ISSUE_FIELD_NUMBER: _ClassVar[int]
    CLOSE_ISSUE_FIELD_NUMBER: _ClassVar[int]
    GET_CONTROL_AGENT_LAUNCH_FIELD_NUMBER: _ClassVar[int]
    GET_ATTACH_INFO_FIELD_NUMBER: _ClassVar[int]
    CAPTURE_SESSION_FIELD_NUMBER: _ClassVar[int]
    SEND_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    GET_DIFF_STATS_FIELD_NUMBER: _ClassVar[int]
    GET_BRANCH_STATE_FIELD_NUMBER: _ClassVar[int]
    GET_DIFF_FIELD_NUMBER: _ClassVar[int]
    REGISTER_MONITOR_FIELD_NUMBER: _ClassVar[int]
    UNREGISTER_MONITOR_FIELD_NUMBER: _ClassVar[int]
    HEARTBEAT_FIELD_NUMBER: _ClassVar[int]
    ping: PingRequest
    list_runs: ListRunsRequest
    get_run: GetRunRequest
    start_run: StartRunRequest
    stop_run: StopRunRequest
    resolve_run: ResolveRunRequest
    list_issues: ListIssuesRequest
    get_issue: GetIssueRequest
    create_issue: CreateIssueRequest
    close_issue: CloseIssueRequest
    get_control_agent_launch: GetControlAgentLaunchRequest
    get_attach_info: GetAttachInfoRequest
    capture_session: CaptureSessionRequest
    send_message: SendMessageRequest
    get_diff_stats: GetDiffStatsRequest
    get_branch_state: GetBranchStateRequest
    get_diff: GetDiffRequest
    register_monitor: RegisterMonitorRequest
    unregister_monitor: UnregisterMonitorRequest
    heartbeat: HeartbeatRequest
    def __init__(self, ping: _Optional[_Union[PingRequest, _Mapping]] = ..., list_runs: _Optional[_Union[ListRunsRequest, _Mapping]] = ..., get_run: _Optional[_Union[GetRunRequest, _Mapping]] = ..., start_run: _Optional[_Union[StartRunRequest, _Mapping]] = ..., stop_run: _Optional[_Union[StopRunRequest, _Mapping]] = ..., resolve_run: _Optional[_Union[ResolveRunRequest, _Mapping]] = ..., list_issues: _Optional[_Union[ListIssuesRequest, _Mapping]] = ..., get_issue: _Optional[_Union[GetIssueRequest, _Mapping]] = ..., create_issue: _Optional[_Union[CreateIssueRequest, _Mapping]] = ..., close_issue: _Optional[_Union[CloseIssueRequest, _Mapping]] = ..., get_control_agent_launch: _Optional[_Union[GetControlAgentLaunchRequest, _Mapping]] = ..., get_attach_info: _Optional[_Union[GetAttachInfoRequest, _Mapping]] = ..., capture_session: _Optional[_Union[CaptureSessionRequest, _Mapping]] = ..., send_message: _Optional[_Union[SendMessageRequest, _Mapping]] = ..., get_diff_stats: _Optional[_Union[GetDiffStatsRequest, _Mapping]] = ..., get_branch_state: _Optional[_Union[GetBranchStateRequest, _Mapping]] = ..., get_diff: _Optional[_Union[GetDiffRequest, _Mapping]] = ..., register_monitor: _Optional[_Union[RegisterMonitorRequest, _Mapping]] = ..., unregister_monitor: _Optional[_Union[UnregisterMonitorRequest, _Mapping]] = ..., heartbeat: _Optional[_Union[HeartbeatRequest, _Mapping]] = ...) -> None: ...

class Response(_message.Message):
    __slots__ = ("ok", "error", "ping", "list_runs", "get_run", "start_run", "stop_run", "resolve_run", "list_issues", "get_issue", "create_issue", "close_issue", "get_control_agent_launch", "get_attach_info", "capture_session", "send_message", "get_diff_stats", "get_branch_state", "get_diff", "register_monitor", "unregister_monitor", "heartbeat")
    OK_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    PING_FIELD_NUMBER: _ClassVar[int]
    LIST_RUNS_FIELD_NUMBER: _ClassVar[int]
    GET_RUN_FIELD_NUMBER: _ClassVar[int]
    START_RUN_FIELD_NUMBER: _ClassVar[int]
    STOP_RUN_FIELD_NUMBER: _ClassVar[int]
    RESOLVE_RUN_FIELD_NUMBER: _ClassVar[int]
    LIST_ISSUES_FIELD_NUMBER: _ClassVar[int]
    GET_ISSUE_FIELD_NUMBER: _ClassVar[int]
    CREATE_ISSUE_FIELD_NUMBER: _ClassVar[int]
    CLOSE_ISSUE_FIELD_NUMBER: _ClassVar[int]
    GET_CONTROL_AGENT_LAUNCH_FIELD_NUMBER: _ClassVar[int]
    GET_ATTACH_INFO_FIELD_NUMBER: _ClassVar[int]
    CAPTURE_SESSION_FIELD_NUMBER: _ClassVar[int]
    SEND_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    GET_DIFF_STATS_FIELD_NUMBER: _ClassVar[int]
    GET_BRANCH_STATE_FIELD_NUMBER: _ClassVar[int]
    GET_DIFF_FIELD_NUMBER: _ClassVar[int]
    REGISTER_MONITOR_FIELD_NUMBER: _ClassVar[int]
    UNREGISTER_MONITOR_FIELD_NUMBER: _ClassVar[int]
    HEARTBEAT_FIELD_NUMBER: _ClassVar[int]
    ok: bool
    error: str
    ping: PingResponse
    list_runs: ListRunsResponse
    get_run: GetRunResponse
    start_run: StartRunResponse
    stop_run: StopRunResponse
    resolve_run: ResolveRunResponse
    list_issues: ListIssuesResponse
    get_issue: GetIssueResponse
    create_issue: CreateIssueResponse
    close_issue: CloseIssueResponse
    get_control_agent_launch: GetControlAgentLaunchResponse
    get_attach_info: GetAttachInfoResponse
    capture_session: CaptureSessionResponse
    send_message: SendMessageResponse
    get_diff_stats: GetDiffStatsResponse
    get_branch_state: GetBranchStateResponse
    get_diff: GetDiffResponse
    register_monitor: RegisterMonitorResponse
    unregister_monitor: UnregisterMonitorResponse
    heartbeat: HeartbeatResponse
    def __init__(self, ok: _Optional[bool] = ..., error: _Optional[str] = ..., ping: _Optional[_Union[PingResponse, _Mapping]] = ..., list_runs: _Optional[_Union[ListRunsResponse, _Mapping]] = ..., get_run: _Optional[_Union[GetRunResponse, _Mapping]] = ..., start_run: _Optional[_Union[StartRunResponse, _Mapping]] = ..., stop_run: _Optional[_Union[StopRunResponse, _Mapping]] = ..., resolve_run: _Optional[_Union[ResolveRunResponse, _Mapping]] = ..., list_issues: _Optional[_Union[ListIssuesResponse, _Mapping]] = ..., get_issue: _Optional[_Union[GetIssueResponse, _Mapping]] = ..., create_issue: _Optional[_Union[CreateIssueResponse, _Mapping]] = ..., close_issue: _Optional[_Union[CloseIssueResponse, _Mapping]] = ..., get_control_agent_launch: _Optional[_Union[GetControlAgentLaunchResponse, _Mapping]] = ..., get_attach_info: _Optional[_Union[GetAttachInfoResponse, _Mapping]] = ..., capture_session: _Optional[_Union[CaptureSessionResponse, _Mapping]] = ..., send_message: _Optional[_Union[SendMessageResponse, _Mapping]] = ..., get_diff_stats: _Optional[_Union[GetDiffStatsResponse, _Mapping]] = ..., get_branch_state: _Optional[_Union[GetBranchStateResponse, _Mapping]] = ..., get_diff: _Optional[_Union[GetDiffResponse, _Mapping]] = ..., register_monitor: _Optional[_Union[RegisterMonitorResponse, _Mapping]] = ..., unregister_monitor: _Optional[_Union[UnregisterMonitorResponse, _Mapping]] = ..., heartbeat: _Optional[_Union[HeartbeatResponse, _Mapping]] = ...) -> None: ...
