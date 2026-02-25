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
    RUN_STATUS_WAITING: _ClassVar[RunStatus]
    RUN_STATUS_RATE_LIMITED: _ClassVar[RunStatus]
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
    BRANCH_STATE_AHEAD: _ClassVar[BranchState]
    BRANCH_STATE_BEHIND: _ClassVar[BranchState]
    BRANCH_STATE_DIVERGED: _ClassVar[BranchState]
    BRANCH_STATE_SYNCED: _ClassVar[BranchState]

class Multiplexer(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MULTIPLEXER_UNSPECIFIED: _ClassVar[Multiplexer]
    MULTIPLEXER_TMUX: _ClassVar[Multiplexer]
    MULTIPLEXER_ZELLIJ: _ClassVar[Multiplexer]
RUN_STATUS_UNSPECIFIED: RunStatus
RUN_STATUS_QUEUED: RunStatus
RUN_STATUS_BOOTING: RunStatus
RUN_STATUS_RUNNING: RunStatus
RUN_STATUS_WAITING: RunStatus
RUN_STATUS_RATE_LIMITED: RunStatus
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
BRANCH_STATE_AHEAD: BranchState
BRANCH_STATE_BEHIND: BranchState
BRANCH_STATE_DIVERGED: BranchState
BRANCH_STATE_SYNCED: BranchState
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
    __slots__ = ("issue_id", "run_id", "status", "agent", "model", "branch", "worktree_path", "pr_url", "started_at_unix", "updated_at_unix", "elapsed_seconds", "elapsed_display", "diff_stats", "branch_state", "session_name", "multiplexer", "server_port", "opencode_session_id", "continued_from", "pr_number", "pr_state", "issue_status", "issue_topic", "alive", "alive_known", "worktree_exists")
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
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    SERVER_PORT_FIELD_NUMBER: _ClassVar[int]
    OPENCODE_SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    CONTINUED_FROM_FIELD_NUMBER: _ClassVar[int]
    PR_NUMBER_FIELD_NUMBER: _ClassVar[int]
    PR_STATE_FIELD_NUMBER: _ClassVar[int]
    ISSUE_STATUS_FIELD_NUMBER: _ClassVar[int]
    ISSUE_TOPIC_FIELD_NUMBER: _ClassVar[int]
    ALIVE_FIELD_NUMBER: _ClassVar[int]
    ALIVE_KNOWN_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_EXISTS_FIELD_NUMBER: _ClassVar[int]
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
    session_name: str
    multiplexer: Multiplexer
    server_port: int
    opencode_session_id: str
    continued_from: str
    pr_number: int
    pr_state: str
    issue_status: str
    issue_topic: str
    alive: bool
    alive_known: bool
    worktree_exists: bool
    def __init__(self, issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., status: _Optional[_Union[RunStatus, str]] = ..., agent: _Optional[str] = ..., model: _Optional[str] = ..., branch: _Optional[str] = ..., worktree_path: _Optional[str] = ..., pr_url: _Optional[str] = ..., started_at_unix: _Optional[int] = ..., updated_at_unix: _Optional[int] = ..., elapsed_seconds: _Optional[int] = ..., elapsed_display: _Optional[str] = ..., diff_stats: _Optional[_Union[DiffStats, _Mapping]] = ..., branch_state: _Optional[_Union[BranchState, str]] = ..., session_name: _Optional[str] = ..., multiplexer: _Optional[_Union[Multiplexer, str]] = ..., server_port: _Optional[int] = ..., opencode_session_id: _Optional[str] = ..., continued_from: _Optional[str] = ..., pr_number: _Optional[int] = ..., pr_state: _Optional[str] = ..., issue_status: _Optional[str] = ..., issue_topic: _Optional[str] = ..., alive: _Optional[bool] = ..., alive_known: _Optional[bool] = ..., worktree_exists: _Optional[bool] = ...) -> None: ...

class Issue(_message.Message):
    __slots__ = ("id", "title", "summary", "status", "tags", "body", "path", "modified_at_unix", "topic")
    ID_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MODIFIED_AT_UNIX_FIELD_NUMBER: _ClassVar[int]
    TOPIC_FIELD_NUMBER: _ClassVar[int]
    id: str
    title: str
    summary: str
    status: IssueStatus
    tags: _containers.RepeatedScalarFieldContainer[str]
    body: str
    path: str
    modified_at_unix: int
    topic: str
    def __init__(self, id: _Optional[str] = ..., title: _Optional[str] = ..., summary: _Optional[str] = ..., status: _Optional[_Union[IssueStatus, str]] = ..., tags: _Optional[_Iterable[str]] = ..., body: _Optional[str] = ..., path: _Optional[str] = ..., modified_at_unix: _Optional[int] = ..., topic: _Optional[str] = ...) -> None: ...

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
    __slots__ = ("issues_root", "issue_id", "status", "agent", "text_search", "time_range", "limit", "cursor", "older_than")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    TEXT_SEARCH_FIELD_NUMBER: _ClassVar[int]
    TIME_RANGE_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    OLDER_THAN_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    status: _containers.RepeatedScalarFieldContainer[RunStatus]
    agent: str
    text_search: str
    time_range: str
    limit: int
    cursor: str
    older_than: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., status: _Optional[_Iterable[_Union[RunStatus, str]]] = ..., agent: _Optional[str] = ..., text_search: _Optional[str] = ..., time_range: _Optional[str] = ..., limit: _Optional[int] = ..., cursor: _Optional[str] = ..., older_than: _Optional[str] = ...) -> None: ...

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
    __slots__ = ("issues_root", "issue_id", "agent", "model", "model_variant", "base_branch", "project_root", "preset", "branch", "worktree_dir", "no_pr", "prompt_template", "pr_target_branch", "dry_run", "reuse", "run_id", "agent_cmd", "agent_profile", "multiplexer")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MODEL_VARIANT_FIELD_NUMBER: _ClassVar[int]
    BASE_BRANCH_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ROOT_FIELD_NUMBER: _ClassVar[int]
    PRESET_FIELD_NUMBER: _ClassVar[int]
    BRANCH_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_DIR_FIELD_NUMBER: _ClassVar[int]
    NO_PR_FIELD_NUMBER: _ClassVar[int]
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    PR_TARGET_BRANCH_FIELD_NUMBER: _ClassVar[int]
    DRY_RUN_FIELD_NUMBER: _ClassVar[int]
    REUSE_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_CMD_FIELD_NUMBER: _ClassVar[int]
    AGENT_PROFILE_FIELD_NUMBER: _ClassVar[int]
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    agent: str
    model: str
    model_variant: str
    base_branch: str
    project_root: str
    preset: str
    branch: str
    worktree_dir: str
    no_pr: bool
    prompt_template: str
    pr_target_branch: str
    dry_run: bool
    reuse: bool
    run_id: str
    agent_cmd: str
    agent_profile: str
    multiplexer: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., agent: _Optional[str] = ..., model: _Optional[str] = ..., model_variant: _Optional[str] = ..., base_branch: _Optional[str] = ..., project_root: _Optional[str] = ..., preset: _Optional[str] = ..., branch: _Optional[str] = ..., worktree_dir: _Optional[str] = ..., no_pr: _Optional[bool] = ..., prompt_template: _Optional[str] = ..., pr_target_branch: _Optional[str] = ..., dry_run: _Optional[bool] = ..., reuse: _Optional[bool] = ..., run_id: _Optional[str] = ..., agent_cmd: _Optional[str] = ..., agent_profile: _Optional[str] = ..., multiplexer: _Optional[str] = ...) -> None: ...

class StartRunResponse(_message.Message):
    __slots__ = ("run_id", "branch", "worktree_path", "session_name", "status")
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    BRANCH_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_PATH_FIELD_NUMBER: _ClassVar[int]
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    branch: str
    worktree_path: str
    session_name: str
    status: str
    def __init__(self, run_id: _Optional[str] = ..., branch: _Optional[str] = ..., worktree_path: _Optional[str] = ..., session_name: _Optional[str] = ..., status: _Optional[str] = ...) -> None: ...

class CreateRunRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id", "metadata")
    class MetadataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    metadata: _containers.ScalarMap[str, str]
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., metadata: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateRunResponse(_message.Message):
    __slots__ = ("issue_id", "run_id", "path")
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    issue_id: str
    run_id: str
    path: str
    def __init__(self, issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

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
    __slots__ = ("issues_root", "issue_id", "title", "body", "tags")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    title: str
    body: str
    tags: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., title: _Optional[str] = ..., body: _Optional[str] = ..., tags: _Optional[_Iterable[str]] = ...) -> None: ...

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
    __slots__ = ("issues_root", "issue_id", "run_id", "short_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    short_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., short_id: _Optional[str] = ...) -> None: ...

class GetAttachInfoResponse(_message.Message):
    __slots__ = ("command", "multiplexer", "session_name", "worktree_path", "agent", "server_port", "opencode_session_id", "issue_id", "run_id")
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_PATH_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    SERVER_PORT_FIELD_NUMBER: _ClassVar[int]
    OPENCODE_SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    command: _containers.RepeatedScalarFieldContainer[str]
    multiplexer: Multiplexer
    session_name: str
    worktree_path: str
    agent: str
    server_port: int
    opencode_session_id: str
    issue_id: str
    run_id: str
    def __init__(self, command: _Optional[_Iterable[str]] = ..., multiplexer: _Optional[_Union[Multiplexer, str]] = ..., session_name: _Optional[str] = ..., worktree_path: _Optional[str] = ..., agent: _Optional[str] = ..., server_port: _Optional[int] = ..., opencode_session_id: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ...) -> None: ...

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

class ListMonitorsRequest(_message.Message):
    __slots__ = ("project", "all")
    PROJECT_FIELD_NUMBER: _ClassVar[int]
    ALL_FIELD_NUMBER: _ClassVar[int]
    project: str
    all: bool
    def __init__(self, project: _Optional[str] = ..., all: _Optional[bool] = ...) -> None: ...

class MonitorInfo(_message.Message):
    __slots__ = ("id", "pid", "type", "view", "project", "session_name", "started_at_unix", "last_heartbeat_unix")
    ID_FIELD_NUMBER: _ClassVar[int]
    PID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    VIEW_FIELD_NUMBER: _ClassVar[int]
    PROJECT_FIELD_NUMBER: _ClassVar[int]
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_UNIX_FIELD_NUMBER: _ClassVar[int]
    LAST_HEARTBEAT_UNIX_FIELD_NUMBER: _ClassVar[int]
    id: str
    pid: int
    type: str
    view: str
    project: str
    session_name: str
    started_at_unix: int
    last_heartbeat_unix: int
    def __init__(self, id: _Optional[str] = ..., pid: _Optional[int] = ..., type: _Optional[str] = ..., view: _Optional[str] = ..., project: _Optional[str] = ..., session_name: _Optional[str] = ..., started_at_unix: _Optional[int] = ..., last_heartbeat_unix: _Optional[int] = ...) -> None: ...

class ListMonitorsResponse(_message.Message):
    __slots__ = ("monitors",)
    MONITORS_FIELD_NUMBER: _ClassVar[int]
    monitors: _containers.RepeatedCompositeFieldContainer[MonitorInfo]
    def __init__(self, monitors: _Optional[_Iterable[_Union[MonitorInfo, _Mapping]]] = ...) -> None: ...

class KillMonitorRequest(_message.Message):
    __slots__ = ("monitor_id", "all", "project")
    MONITOR_ID_FIELD_NUMBER: _ClassVar[int]
    ALL_FIELD_NUMBER: _ClassVar[int]
    GLOBAL_FIELD_NUMBER: _ClassVar[int]
    PROJECT_FIELD_NUMBER: _ClassVar[int]
    monitor_id: str
    all: bool
    project: str
    def __init__(self, monitor_id: _Optional[str] = ..., all: _Optional[bool] = ..., project: _Optional[str] = ..., **kwargs) -> None: ...

class KillMonitorResponse(_message.Message):
    __slots__ = ("killed_count",)
    KILLED_COUNT_FIELD_NUMBER: _ClassVar[int]
    killed_count: int
    def __init__(self, killed_count: _Optional[int] = ...) -> None: ...

class GetRunByShortIDRequest(_message.Message):
    __slots__ = ("issues_root", "short_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    short_id: str
    def __init__(self, issues_root: _Optional[str] = ..., short_id: _Optional[str] = ...) -> None: ...

class GetRunByShortIDResponse(_message.Message):
    __slots__ = ("run", "events")
    RUN_FIELD_NUMBER: _ClassVar[int]
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    run: Run
    events: _containers.RepeatedCompositeFieldContainer[Event]
    def __init__(self, run: _Optional[_Union[Run, _Mapping]] = ..., events: _Optional[_Iterable[_Union[Event, _Mapping]]] = ...) -> None: ...

class ResolveIssueRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "force")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    FORCE_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    force: bool
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., force: _Optional[bool] = ...) -> None: ...

class ResolveIssueResponse(_message.Message):
    __slots__ = ("issue_id",)
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    issue_id: str
    def __init__(self, issue_id: _Optional[str] = ...) -> None: ...

class AppendEventRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id", "event_type", "event_name", "event_attrs", "event_source")
    class EventAttrsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    EVENT_NAME_FIELD_NUMBER: _ClassVar[int]
    EVENT_ATTRS_FIELD_NUMBER: _ClassVar[int]
    EVENT_SOURCE_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    event_type: str
    event_name: str
    event_attrs: _containers.ScalarMap[str, str]
    event_source: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., event_type: _Optional[str] = ..., event_name: _Optional[str] = ..., event_attrs: _Optional[_Mapping[str, str]] = ..., event_source: _Optional[str] = ...) -> None: ...

class AppendEventResponse(_message.Message):
    __slots__ = ("skipped", "reason")
    SKIPPED_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    skipped: bool
    reason: str
    def __init__(self, skipped: _Optional[bool] = ..., reason: _Optional[str] = ...) -> None: ...

class EnsureOpenCodeServerRequest(_message.Message):
    __slots__ = ("project_root",)
    PROJECT_ROOT_FIELD_NUMBER: _ClassVar[int]
    project_root: str
    def __init__(self, project_root: _Optional[str] = ...) -> None: ...

class EnsureOpenCodeServerResponse(_message.Message):
    __slots__ = ("port", "already_running")
    PORT_FIELD_NUMBER: _ClassVar[int]
    ALREADY_RUNNING_FIELD_NUMBER: _ClassVar[int]
    port: int
    already_running: bool
    def __init__(self, port: _Optional[int] = ..., already_running: _Optional[bool] = ...) -> None: ...

class RegisterRepoRequest(_message.Message):
    __slots__ = ("project_root",)
    PROJECT_ROOT_FIELD_NUMBER: _ClassVar[int]
    project_root: str
    def __init__(self, project_root: _Optional[str] = ...) -> None: ...

class RegisterRepoResponse(_message.Message):
    __slots__ = ("repo_id",)
    REPO_ID_FIELD_NUMBER: _ClassVar[int]
    repo_id: str
    def __init__(self, repo_id: _Optional[str] = ...) -> None: ...

class ListReposRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class RepoInfo(_message.Message):
    __slots__ = ("id", "project_root", "issues_root")
    ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    id: str
    project_root: str
    issues_root: str
    def __init__(self, id: _Optional[str] = ..., project_root: _Optional[str] = ..., issues_root: _Optional[str] = ...) -> None: ...

class ListReposResponse(_message.Message):
    __slots__ = ("repos",)
    REPOS_FIELD_NUMBER: _ClassVar[int]
    repos: _containers.RepeatedCompositeFieldContainer[RepoInfo]
    def __init__(self, repos: _Optional[_Iterable[_Union[RepoInfo, _Mapping]]] = ...) -> None: ...

class DeleteRunRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id", "short_id", "with_worktree", "with_branch", "force")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    WITH_WORKTREE_FIELD_NUMBER: _ClassVar[int]
    WITH_BRANCH_FIELD_NUMBER: _ClassVar[int]
    FORCE_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    short_id: str
    with_worktree: bool
    with_branch: bool
    force: bool
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., short_id: _Optional[str] = ..., with_worktree: _Optional[bool] = ..., with_branch: _Optional[bool] = ..., force: _Optional[bool] = ...) -> None: ...

class DeleteRunResponse(_message.Message):
    __slots__ = ("issue_id", "run_id", "short_id", "worktree_removed", "branch_removed", "session_killed")
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_REMOVED_FIELD_NUMBER: _ClassVar[int]
    BRANCH_REMOVED_FIELD_NUMBER: _ClassVar[int]
    SESSION_KILLED_FIELD_NUMBER: _ClassVar[int]
    issue_id: str
    run_id: str
    short_id: str
    worktree_removed: bool
    branch_removed: bool
    session_killed: bool
    def __init__(self, issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., short_id: _Optional[str] = ..., worktree_removed: _Optional[bool] = ..., branch_removed: _Optional[bool] = ..., session_killed: _Optional[bool] = ...) -> None: ...

class UpdateIssueRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "title", "summary", "body", "status")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    title: str
    summary: str
    body: str
    status: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., title: _Optional[str] = ..., summary: _Optional[str] = ..., body: _Optional[str] = ..., status: _Optional[str] = ...) -> None: ...

class UpdateIssueResponse(_message.Message):
    __slots__ = ("issue",)
    ISSUE_FIELD_NUMBER: _ClassVar[int]
    issue: Issue
    def __init__(self, issue: _Optional[_Union[Issue, _Mapping]] = ...) -> None: ...

class ValidateIssueFilesRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ...) -> None: ...

class ValidationIssue(_message.Message):
    __slots__ = ("code", "message", "line", "level")
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    LINE_FIELD_NUMBER: _ClassVar[int]
    LEVEL_FIELD_NUMBER: _ClassVar[int]
    code: str
    message: str
    line: int
    level: str
    def __init__(self, code: _Optional[str] = ..., message: _Optional[str] = ..., line: _Optional[int] = ..., level: _Optional[str] = ...) -> None: ...

class ValidationResultItem(_message.Message):
    __slots__ = ("file", "issue_id", "errors", "warnings")
    FILE_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    ERRORS_FIELD_NUMBER: _ClassVar[int]
    WARNINGS_FIELD_NUMBER: _ClassVar[int]
    file: str
    issue_id: str
    errors: _containers.RepeatedCompositeFieldContainer[ValidationIssue]
    warnings: _containers.RepeatedCompositeFieldContainer[ValidationIssue]
    def __init__(self, file: _Optional[str] = ..., issue_id: _Optional[str] = ..., errors: _Optional[_Iterable[_Union[ValidationIssue, _Mapping]]] = ..., warnings: _Optional[_Iterable[_Union[ValidationIssue, _Mapping]]] = ...) -> None: ...

class DuplicateIDItem(_message.Message):
    __slots__ = ("id", "files")
    ID_FIELD_NUMBER: _ClassVar[int]
    FILES_FIELD_NUMBER: _ClassVar[int]
    id: str
    files: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, id: _Optional[str] = ..., files: _Optional[_Iterable[str]] = ...) -> None: ...

class ValidateIssueFilesResponse(_message.Message):
    __slots__ = ("total", "valid", "errors", "warnings", "duplicates")
    TOTAL_FIELD_NUMBER: _ClassVar[int]
    VALID_FIELD_NUMBER: _ClassVar[int]
    ERRORS_FIELD_NUMBER: _ClassVar[int]
    WARNINGS_FIELD_NUMBER: _ClassVar[int]
    DUPLICATES_FIELD_NUMBER: _ClassVar[int]
    total: int
    valid: int
    errors: _containers.RepeatedCompositeFieldContainer[ValidationResultItem]
    warnings: _containers.RepeatedCompositeFieldContainer[ValidationResultItem]
    duplicates: _containers.RepeatedCompositeFieldContainer[DuplicateIDItem]
    def __init__(self, total: _Optional[int] = ..., valid: _Optional[int] = ..., errors: _Optional[_Iterable[_Union[ValidationResultItem, _Mapping]]] = ..., warnings: _Optional[_Iterable[_Union[ValidationResultItem, _Mapping]]] = ..., duplicates: _Optional[_Iterable[_Union[DuplicateIDItem, _Mapping]]] = ...) -> None: ...

class WriteAgentPromptRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id", "short_id", "content")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    short_id: str
    content: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., short_id: _Optional[str] = ..., content: _Optional[str] = ...) -> None: ...

class WriteAgentPromptResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ReadAgentPromptRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id", "short_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    short_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., short_id: _Optional[str] = ...) -> None: ...

class ReadAgentPromptResponse(_message.Message):
    __slots__ = ("content",)
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    content: str
    def __init__(self, content: _Optional[str] = ...) -> None: ...

class RepairStateRequest(_message.Message):
    __slots__ = ("dry_run", "force")
    DRY_RUN_FIELD_NUMBER: _ClassVar[int]
    FORCE_FIELD_NUMBER: _ClassVar[int]
    dry_run: bool
    force: bool
    def __init__(self, dry_run: _Optional[bool] = ..., force: _Optional[bool] = ...) -> None: ...

class RepairStateResponse(_message.Message):
    __slots__ = ("problems_found", "problems_fixed", "details")
    PROBLEMS_FOUND_FIELD_NUMBER: _ClassVar[int]
    PROBLEMS_FIXED_FIELD_NUMBER: _ClassVar[int]
    DETAILS_FIELD_NUMBER: _ClassVar[int]
    problems_found: int
    problems_fixed: int
    details: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, problems_found: _Optional[int] = ..., problems_fixed: _Optional[int] = ..., details: _Optional[_Iterable[str]] = ...) -> None: ...

class GetDaemonLogRequest(_message.Message):
    __slots__ = ("lines",)
    LINES_FIELD_NUMBER: _ClassVar[int]
    lines: int
    def __init__(self, lines: _Optional[int] = ...) -> None: ...

class GetDaemonLogResponse(_message.Message):
    __slots__ = ("content",)
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    content: str
    def __init__(self, content: _Optional[str] = ...) -> None: ...

class ReadFileRequest(_message.Message):
    __slots__ = ("path",)
    PATH_FIELD_NUMBER: _ClassVar[int]
    path: str
    def __init__(self, path: _Optional[str] = ...) -> None: ...

class ReadFileResponse(_message.Message):
    __slots__ = ("content",)
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    content: bytes
    def __init__(self, content: _Optional[bytes] = ...) -> None: ...

class WriteFileRequest(_message.Message):
    __slots__ = ("path", "content", "perm")
    PATH_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    PERM_FIELD_NUMBER: _ClassVar[int]
    path: str
    content: bytes
    perm: int
    def __init__(self, path: _Optional[str] = ..., content: _Optional[bytes] = ..., perm: _Optional[int] = ...) -> None: ...

class WriteFileResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class KillSessionRequest(_message.Message):
    __slots__ = ("session_name", "multiplexer")
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    session_name: str
    multiplexer: Multiplexer
    def __init__(self, session_name: _Optional[str] = ..., multiplexer: _Optional[_Union[Multiplexer, str]] = ...) -> None: ...

class KillSessionResponse(_message.Message):
    __slots__ = ("killed",)
    KILLED_FIELD_NUMBER: _ClassVar[int]
    killed: bool
    def __init__(self, killed: _Optional[bool] = ...) -> None: ...

class ListSessionsRequest(_message.Message):
    __slots__ = ("multiplexer",)
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    multiplexer: Multiplexer
    def __init__(self, multiplexer: _Optional[_Union[Multiplexer, str]] = ...) -> None: ...

class ListSessionsResponse(_message.Message):
    __slots__ = ("sessions",)
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, sessions: _Optional[_Iterable[str]] = ...) -> None: ...

class ResumeRunRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id", "short_id")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    short_id: str
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., short_id: _Optional[str] = ...) -> None: ...

class ResumeRunResponse(_message.Message):
    __slots__ = ("session_name",)
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    session_name: str
    def __init__(self, session_name: _Optional[str] = ...) -> None: ...

class QueryOpenCodeServerRequest(_message.Message):
    __slots__ = ("port", "project_root")
    PORT_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ROOT_FIELD_NUMBER: _ClassVar[int]
    port: int
    project_root: str
    def __init__(self, port: _Optional[int] = ..., project_root: _Optional[str] = ...) -> None: ...

class OpenCodeProviderInfo(_message.Message):
    __slots__ = ("id", "name", "models")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    MODELS_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    models: _containers.RepeatedCompositeFieldContainer[OpenCodeModelInfo]
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., models: _Optional[_Iterable[_Union[OpenCodeModelInfo, _Mapping]]] = ...) -> None: ...

class OpenCodeModelInfo(_message.Message):
    __slots__ = ("id", "name", "variants")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    VARIANTS_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    variants: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., variants: _Optional[_Iterable[str]] = ...) -> None: ...

class QueryOpenCodeServerResponse(_message.Message):
    __slots__ = ("server_running", "providers", "session_status")
    class SessionStatusEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SERVER_RUNNING_FIELD_NUMBER: _ClassVar[int]
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    SESSION_STATUS_FIELD_NUMBER: _ClassVar[int]
    server_running: bool
    providers: _containers.RepeatedCompositeFieldContainer[OpenCodeProviderInfo]
    session_status: _containers.ScalarMap[str, str]
    def __init__(self, server_running: _Optional[bool] = ..., providers: _Optional[_Iterable[_Union[OpenCodeProviderInfo, _Mapping]]] = ..., session_status: _Optional[_Mapping[str, str]] = ...) -> None: ...

class InjectInitialPromptRequest(_message.Message):
    __slots__ = ("issues_root", "issue_id", "run_id", "prompt", "model", "model_variant", "work_dir", "port")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MODEL_VARIANT_FIELD_NUMBER: _ClassVar[int]
    WORK_DIR_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    issue_id: str
    run_id: str
    prompt: str
    model: str
    model_variant: str
    work_dir: str
    port: int
    def __init__(self, issues_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., prompt: _Optional[str] = ..., model: _Optional[str] = ..., model_variant: _Optional[str] = ..., work_dir: _Optional[str] = ..., port: _Optional[int] = ...) -> None: ...

class InjectInitialPromptResponse(_message.Message):
    __slots__ = ("session_id", "port")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    port: int
    def __init__(self, session_id: _Optional[str] = ..., port: _Optional[int] = ...) -> None: ...

class ContinueRunRequest(_message.Message):
    __slots__ = ("issues_root", "project_root", "issue_id", "run_id", "short_id", "branch", "agent", "agent_cmd", "agent_profile", "worktree_dir", "no_pr", "prompt_template", "pr_target_branch", "multiplexer", "session_name", "repo_root")
    ISSUES_ROOT_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ROOT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    BRANCH_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    AGENT_CMD_FIELD_NUMBER: _ClassVar[int]
    AGENT_PROFILE_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_DIR_FIELD_NUMBER: _ClassVar[int]
    NO_PR_FIELD_NUMBER: _ClassVar[int]
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    PR_TARGET_BRANCH_FIELD_NUMBER: _ClassVar[int]
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    REPO_ROOT_FIELD_NUMBER: _ClassVar[int]
    issues_root: str
    project_root: str
    issue_id: str
    run_id: str
    short_id: str
    branch: str
    agent: str
    agent_cmd: str
    agent_profile: str
    worktree_dir: str
    no_pr: bool
    prompt_template: str
    pr_target_branch: str
    multiplexer: str
    session_name: str
    repo_root: str
    def __init__(self, issues_root: _Optional[str] = ..., project_root: _Optional[str] = ..., issue_id: _Optional[str] = ..., run_id: _Optional[str] = ..., short_id: _Optional[str] = ..., branch: _Optional[str] = ..., agent: _Optional[str] = ..., agent_cmd: _Optional[str] = ..., agent_profile: _Optional[str] = ..., worktree_dir: _Optional[str] = ..., no_pr: _Optional[bool] = ..., prompt_template: _Optional[str] = ..., pr_target_branch: _Optional[str] = ..., multiplexer: _Optional[str] = ..., session_name: _Optional[str] = ..., repo_root: _Optional[str] = ...) -> None: ...

class ContinueRunResponse(_message.Message):
    __slots__ = ("run_id", "branch", "worktree_path", "session_name", "status", "continued_from", "issue_id")
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    BRANCH_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_PATH_FIELD_NUMBER: _ClassVar[int]
    SESSION_NAME_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CONTINUED_FROM_FIELD_NUMBER: _ClassVar[int]
    ISSUE_ID_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    branch: str
    worktree_path: str
    session_name: str
    status: str
    continued_from: str
    issue_id: str
    def __init__(self, run_id: _Optional[str] = ..., branch: _Optional[str] = ..., worktree_path: _Optional[str] = ..., session_name: _Optional[str] = ..., status: _Optional[str] = ..., continued_from: _Optional[str] = ..., issue_id: _Optional[str] = ...) -> None: ...

class GetConfigRequest(_message.Message):
    __slots__ = ("project_root",)
    PROJECT_ROOT_FIELD_NUMBER: _ClassVar[int]
    project_root: str
    def __init__(self, project_root: _Optional[str] = ...) -> None: ...

class SlackConfigProto(_message.Message):
    __slots__ = ("enabled", "webhook_url", "bot_token", "channel", "notify_on")
    ENABLED_FIELD_NUMBER: _ClassVar[int]
    WEBHOOK_URL_FIELD_NUMBER: _ClassVar[int]
    BOT_TOKEN_FIELD_NUMBER: _ClassVar[int]
    CHANNEL_FIELD_NUMBER: _ClassVar[int]
    NOTIFY_ON_FIELD_NUMBER: _ClassVar[int]
    enabled: bool
    webhook_url: str
    bot_token: str
    channel: str
    notify_on: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, enabled: _Optional[bool] = ..., webhook_url: _Optional[str] = ..., bot_token: _Optional[str] = ..., channel: _Optional[str] = ..., notify_on: _Optional[_Iterable[str]] = ...) -> None: ...

class OpenCodeConfigProto(_message.Message):
    __slots__ = ("default_model", "default_variant", "prompt_template", "extra_args", "control_extra_args")
    DEFAULT_MODEL_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_VARIANT_FIELD_NUMBER: _ClassVar[int]
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    EXTRA_ARGS_FIELD_NUMBER: _ClassVar[int]
    CONTROL_EXTRA_ARGS_FIELD_NUMBER: _ClassVar[int]
    default_model: str
    default_variant: str
    prompt_template: str
    extra_args: _containers.RepeatedScalarFieldContainer[str]
    control_extra_args: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, default_model: _Optional[str] = ..., default_variant: _Optional[str] = ..., prompt_template: _Optional[str] = ..., extra_args: _Optional[_Iterable[str]] = ..., control_extra_args: _Optional[_Iterable[str]] = ...) -> None: ...

class ClaudeConfigProto(_message.Message):
    __slots__ = ("prompt_template", "extra_args", "control_extra_args")
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    EXTRA_ARGS_FIELD_NUMBER: _ClassVar[int]
    CONTROL_EXTRA_ARGS_FIELD_NUMBER: _ClassVar[int]
    prompt_template: str
    extra_args: _containers.RepeatedScalarFieldContainer[str]
    control_extra_args: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, prompt_template: _Optional[str] = ..., extra_args: _Optional[_Iterable[str]] = ..., control_extra_args: _Optional[_Iterable[str]] = ...) -> None: ...

class CodexConfigProto(_message.Message):
    __slots__ = ("prompt_template", "extra_args", "control_extra_args")
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    EXTRA_ARGS_FIELD_NUMBER: _ClassVar[int]
    CONTROL_EXTRA_ARGS_FIELD_NUMBER: _ClassVar[int]
    prompt_template: str
    extra_args: _containers.RepeatedScalarFieldContainer[str]
    control_extra_args: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, prompt_template: _Optional[str] = ..., extra_args: _Optional[_Iterable[str]] = ..., control_extra_args: _Optional[_Iterable[str]] = ...) -> None: ...

class GeminiConfigProto(_message.Message):
    __slots__ = ("prompt_template", "extra_args", "control_extra_args")
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    EXTRA_ARGS_FIELD_NUMBER: _ClassVar[int]
    CONTROL_EXTRA_ARGS_FIELD_NUMBER: _ClassVar[int]
    prompt_template: str
    extra_args: _containers.RepeatedScalarFieldContainer[str]
    control_extra_args: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, prompt_template: _Optional[str] = ..., extra_args: _Optional[_Iterable[str]] = ..., control_extra_args: _Optional[_Iterable[str]] = ...) -> None: ...

class PresetProto(_message.Message):
    __slots__ = ("name", "backend", "model", "variant", "profile")
    NAME_FIELD_NUMBER: _ClassVar[int]
    BACKEND_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    VARIANT_FIELD_NUMBER: _ClassVar[int]
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    name: str
    backend: str
    model: str
    variant: str
    profile: str
    def __init__(self, name: _Optional[str] = ..., backend: _Optional[str] = ..., model: _Optional[str] = ..., variant: _Optional[str] = ..., profile: _Optional[str] = ...) -> None: ...

class IssuesConfigProto(_message.Message):
    __slots__ = ("backend", "path")
    BACKEND_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    backend: str
    path: str
    def __init__(self, backend: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class GitHubConfigProto(_message.Message):
    __slots__ = ("owner", "repo", "label_filter", "poll_interval", "status_labels")
    class StatusLabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    OWNER_FIELD_NUMBER: _ClassVar[int]
    REPO_FIELD_NUMBER: _ClassVar[int]
    LABEL_FILTER_FIELD_NUMBER: _ClassVar[int]
    POLL_INTERVAL_FIELD_NUMBER: _ClassVar[int]
    STATUS_LABELS_FIELD_NUMBER: _ClassVar[int]
    owner: str
    repo: str
    label_filter: str
    poll_interval: int
    status_labels: _containers.ScalarMap[str, str]
    def __init__(self, owner: _Optional[str] = ..., repo: _Optional[str] = ..., label_filter: _Optional[str] = ..., poll_interval: _Optional[int] = ..., status_labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class MonitorConfigProto(_message.Message):
    __slots__ = ("ps_columns",)
    PS_COLUMNS_FIELD_NUMBER: _ClassVar[int]
    ps_columns: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, ps_columns: _Optional[_Iterable[str]] = ...) -> None: ...

class GetConfigResponse(_message.Message):
    __slots__ = ("agent", "model", "model_variant", "worktree_dir", "base_branch", "pr_target_branch", "log_level", "prompt_template", "multiplexer", "monitor_multiplexer", "agent_multiplexer", "no_pr", "default_preset", "control_agent", "control_model", "control_model_variant", "diff_tool", "monitor", "presets", "opencode", "claude", "codex", "gemini", "slack", "issues", "github")
    AGENT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MODEL_VARIANT_FIELD_NUMBER: _ClassVar[int]
    WORKTREE_DIR_FIELD_NUMBER: _ClassVar[int]
    BASE_BRANCH_FIELD_NUMBER: _ClassVar[int]
    PR_TARGET_BRANCH_FIELD_NUMBER: _ClassVar[int]
    LOG_LEVEL_FIELD_NUMBER: _ClassVar[int]
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    MONITOR_MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    AGENT_MULTIPLEXER_FIELD_NUMBER: _ClassVar[int]
    NO_PR_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_PRESET_FIELD_NUMBER: _ClassVar[int]
    CONTROL_AGENT_FIELD_NUMBER: _ClassVar[int]
    CONTROL_MODEL_FIELD_NUMBER: _ClassVar[int]
    CONTROL_MODEL_VARIANT_FIELD_NUMBER: _ClassVar[int]
    DIFF_TOOL_FIELD_NUMBER: _ClassVar[int]
    MONITOR_FIELD_NUMBER: _ClassVar[int]
    PRESETS_FIELD_NUMBER: _ClassVar[int]
    OPENCODE_FIELD_NUMBER: _ClassVar[int]
    CLAUDE_FIELD_NUMBER: _ClassVar[int]
    CODEX_FIELD_NUMBER: _ClassVar[int]
    GEMINI_FIELD_NUMBER: _ClassVar[int]
    SLACK_FIELD_NUMBER: _ClassVar[int]
    ISSUES_FIELD_NUMBER: _ClassVar[int]
    GITHUB_FIELD_NUMBER: _ClassVar[int]
    agent: str
    model: str
    model_variant: str
    worktree_dir: str
    base_branch: str
    pr_target_branch: str
    log_level: str
    prompt_template: str
    multiplexer: str
    monitor_multiplexer: str
    agent_multiplexer: str
    no_pr: bool
    default_preset: str
    control_agent: str
    control_model: str
    control_model_variant: str
    diff_tool: str
    monitor: MonitorConfigProto
    presets: _containers.RepeatedCompositeFieldContainer[PresetProto]
    opencode: OpenCodeConfigProto
    claude: ClaudeConfigProto
    codex: CodexConfigProto
    gemini: GeminiConfigProto
    slack: SlackConfigProto
    issues: IssuesConfigProto
    github: GitHubConfigProto
    def __init__(self, agent: _Optional[str] = ..., model: _Optional[str] = ..., model_variant: _Optional[str] = ..., worktree_dir: _Optional[str] = ..., base_branch: _Optional[str] = ..., pr_target_branch: _Optional[str] = ..., log_level: _Optional[str] = ..., prompt_template: _Optional[str] = ..., multiplexer: _Optional[str] = ..., monitor_multiplexer: _Optional[str] = ..., agent_multiplexer: _Optional[str] = ..., no_pr: _Optional[bool] = ..., default_preset: _Optional[str] = ..., control_agent: _Optional[str] = ..., control_model: _Optional[str] = ..., control_model_variant: _Optional[str] = ..., diff_tool: _Optional[str] = ..., monitor: _Optional[_Union[MonitorConfigProto, _Mapping]] = ..., presets: _Optional[_Iterable[_Union[PresetProto, _Mapping]]] = ..., opencode: _Optional[_Union[OpenCodeConfigProto, _Mapping]] = ..., claude: _Optional[_Union[ClaudeConfigProto, _Mapping]] = ..., codex: _Optional[_Union[CodexConfigProto, _Mapping]] = ..., gemini: _Optional[_Union[GeminiConfigProto, _Mapping]] = ..., slack: _Optional[_Union[SlackConfigProto, _Mapping]] = ..., issues: _Optional[_Union[IssuesConfigProto, _Mapping]] = ..., github: _Optional[_Union[GitHubConfigProto, _Mapping]] = ...) -> None: ...

class GetDaemonStatusRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetDaemonStatusResponse(_message.Message):
    __slots__ = ("running", "pid", "log_path", "version")
    RUNNING_FIELD_NUMBER: _ClassVar[int]
    PID_FIELD_NUMBER: _ClassVar[int]
    LOG_PATH_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    running: bool
    pid: int
    log_path: str
    version: str
    def __init__(self, running: _Optional[bool] = ..., pid: _Optional[int] = ..., log_path: _Optional[str] = ..., version: _Optional[str] = ...) -> None: ...

class Request(_message.Message):
    __slots__ = ("ping", "list_runs", "get_run", "start_run", "stop_run", "resolve_run", "list_issues", "get_issue", "create_issue", "close_issue", "get_control_agent_launch", "get_attach_info", "capture_session", "send_message", "get_diff_stats", "get_branch_state", "get_diff", "register_monitor", "unregister_monitor", "heartbeat", "list_monitors", "kill_monitor", "get_run_by_short_id", "resolve_issue", "append_event", "ensure_opencode_server", "register_repo", "list_repos", "delete_run", "update_issue", "validate_issue_files", "write_agent_prompt", "read_agent_prompt", "repair_state", "get_daemon_log", "read_file", "write_file", "create_run", "kill_session", "list_sessions", "resume_run", "query_opencode_server", "inject_initial_prompt", "continue_run", "get_config", "get_daemon_status")
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
    LIST_MONITORS_FIELD_NUMBER: _ClassVar[int]
    KILL_MONITOR_FIELD_NUMBER: _ClassVar[int]
    GET_RUN_BY_SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    RESOLVE_ISSUE_FIELD_NUMBER: _ClassVar[int]
    APPEND_EVENT_FIELD_NUMBER: _ClassVar[int]
    ENSURE_OPENCODE_SERVER_FIELD_NUMBER: _ClassVar[int]
    REGISTER_REPO_FIELD_NUMBER: _ClassVar[int]
    LIST_REPOS_FIELD_NUMBER: _ClassVar[int]
    DELETE_RUN_FIELD_NUMBER: _ClassVar[int]
    UPDATE_ISSUE_FIELD_NUMBER: _ClassVar[int]
    VALIDATE_ISSUE_FILES_FIELD_NUMBER: _ClassVar[int]
    WRITE_AGENT_PROMPT_FIELD_NUMBER: _ClassVar[int]
    READ_AGENT_PROMPT_FIELD_NUMBER: _ClassVar[int]
    REPAIR_STATE_FIELD_NUMBER: _ClassVar[int]
    GET_DAEMON_LOG_FIELD_NUMBER: _ClassVar[int]
    READ_FILE_FIELD_NUMBER: _ClassVar[int]
    WRITE_FILE_FIELD_NUMBER: _ClassVar[int]
    CREATE_RUN_FIELD_NUMBER: _ClassVar[int]
    KILL_SESSION_FIELD_NUMBER: _ClassVar[int]
    LIST_SESSIONS_FIELD_NUMBER: _ClassVar[int]
    RESUME_RUN_FIELD_NUMBER: _ClassVar[int]
    QUERY_OPENCODE_SERVER_FIELD_NUMBER: _ClassVar[int]
    INJECT_INITIAL_PROMPT_FIELD_NUMBER: _ClassVar[int]
    CONTINUE_RUN_FIELD_NUMBER: _ClassVar[int]
    GET_CONFIG_FIELD_NUMBER: _ClassVar[int]
    GET_DAEMON_STATUS_FIELD_NUMBER: _ClassVar[int]
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
    list_monitors: ListMonitorsRequest
    kill_monitor: KillMonitorRequest
    get_run_by_short_id: GetRunByShortIDRequest
    resolve_issue: ResolveIssueRequest
    append_event: AppendEventRequest
    ensure_opencode_server: EnsureOpenCodeServerRequest
    register_repo: RegisterRepoRequest
    list_repos: ListReposRequest
    delete_run: DeleteRunRequest
    update_issue: UpdateIssueRequest
    validate_issue_files: ValidateIssueFilesRequest
    write_agent_prompt: WriteAgentPromptRequest
    read_agent_prompt: ReadAgentPromptRequest
    repair_state: RepairStateRequest
    get_daemon_log: GetDaemonLogRequest
    read_file: ReadFileRequest
    write_file: WriteFileRequest
    create_run: CreateRunRequest
    kill_session: KillSessionRequest
    list_sessions: ListSessionsRequest
    resume_run: ResumeRunRequest
    query_opencode_server: QueryOpenCodeServerRequest
    inject_initial_prompt: InjectInitialPromptRequest
    continue_run: ContinueRunRequest
    get_config: GetConfigRequest
    get_daemon_status: GetDaemonStatusRequest
    def __init__(self, ping: _Optional[_Union[PingRequest, _Mapping]] = ..., list_runs: _Optional[_Union[ListRunsRequest, _Mapping]] = ..., get_run: _Optional[_Union[GetRunRequest, _Mapping]] = ..., start_run: _Optional[_Union[StartRunRequest, _Mapping]] = ..., stop_run: _Optional[_Union[StopRunRequest, _Mapping]] = ..., resolve_run: _Optional[_Union[ResolveRunRequest, _Mapping]] = ..., list_issues: _Optional[_Union[ListIssuesRequest, _Mapping]] = ..., get_issue: _Optional[_Union[GetIssueRequest, _Mapping]] = ..., create_issue: _Optional[_Union[CreateIssueRequest, _Mapping]] = ..., close_issue: _Optional[_Union[CloseIssueRequest, _Mapping]] = ..., get_control_agent_launch: _Optional[_Union[GetControlAgentLaunchRequest, _Mapping]] = ..., get_attach_info: _Optional[_Union[GetAttachInfoRequest, _Mapping]] = ..., capture_session: _Optional[_Union[CaptureSessionRequest, _Mapping]] = ..., send_message: _Optional[_Union[SendMessageRequest, _Mapping]] = ..., get_diff_stats: _Optional[_Union[GetDiffStatsRequest, _Mapping]] = ..., get_branch_state: _Optional[_Union[GetBranchStateRequest, _Mapping]] = ..., get_diff: _Optional[_Union[GetDiffRequest, _Mapping]] = ..., register_monitor: _Optional[_Union[RegisterMonitorRequest, _Mapping]] = ..., unregister_monitor: _Optional[_Union[UnregisterMonitorRequest, _Mapping]] = ..., heartbeat: _Optional[_Union[HeartbeatRequest, _Mapping]] = ..., list_monitors: _Optional[_Union[ListMonitorsRequest, _Mapping]] = ..., kill_monitor: _Optional[_Union[KillMonitorRequest, _Mapping]] = ..., get_run_by_short_id: _Optional[_Union[GetRunByShortIDRequest, _Mapping]] = ..., resolve_issue: _Optional[_Union[ResolveIssueRequest, _Mapping]] = ..., append_event: _Optional[_Union[AppendEventRequest, _Mapping]] = ..., ensure_opencode_server: _Optional[_Union[EnsureOpenCodeServerRequest, _Mapping]] = ..., register_repo: _Optional[_Union[RegisterRepoRequest, _Mapping]] = ..., list_repos: _Optional[_Union[ListReposRequest, _Mapping]] = ..., delete_run: _Optional[_Union[DeleteRunRequest, _Mapping]] = ..., update_issue: _Optional[_Union[UpdateIssueRequest, _Mapping]] = ..., validate_issue_files: _Optional[_Union[ValidateIssueFilesRequest, _Mapping]] = ..., write_agent_prompt: _Optional[_Union[WriteAgentPromptRequest, _Mapping]] = ..., read_agent_prompt: _Optional[_Union[ReadAgentPromptRequest, _Mapping]] = ..., repair_state: _Optional[_Union[RepairStateRequest, _Mapping]] = ..., get_daemon_log: _Optional[_Union[GetDaemonLogRequest, _Mapping]] = ..., read_file: _Optional[_Union[ReadFileRequest, _Mapping]] = ..., write_file: _Optional[_Union[WriteFileRequest, _Mapping]] = ..., create_run: _Optional[_Union[CreateRunRequest, _Mapping]] = ..., kill_session: _Optional[_Union[KillSessionRequest, _Mapping]] = ..., list_sessions: _Optional[_Union[ListSessionsRequest, _Mapping]] = ..., resume_run: _Optional[_Union[ResumeRunRequest, _Mapping]] = ..., query_opencode_server: _Optional[_Union[QueryOpenCodeServerRequest, _Mapping]] = ..., inject_initial_prompt: _Optional[_Union[InjectInitialPromptRequest, _Mapping]] = ..., continue_run: _Optional[_Union[ContinueRunRequest, _Mapping]] = ..., get_config: _Optional[_Union[GetConfigRequest, _Mapping]] = ..., get_daemon_status: _Optional[_Union[GetDaemonStatusRequest, _Mapping]] = ...) -> None: ...

class Response(_message.Message):
    __slots__ = ("ok", "error", "ping", "list_runs", "get_run", "start_run", "stop_run", "resolve_run", "list_issues", "get_issue", "create_issue", "close_issue", "get_control_agent_launch", "get_attach_info", "capture_session", "send_message", "get_diff_stats", "get_branch_state", "get_diff", "register_monitor", "unregister_monitor", "heartbeat", "list_monitors", "kill_monitor", "get_run_by_short_id", "resolve_issue", "append_event", "ensure_opencode_server", "register_repo", "list_repos", "delete_run", "update_issue", "validate_issue_files", "write_agent_prompt", "read_agent_prompt", "repair_state", "get_daemon_log", "read_file", "write_file", "create_run", "kill_session", "list_sessions", "resume_run", "query_opencode_server", "inject_initial_prompt", "continue_run", "get_config", "get_daemon_status")
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
    LIST_MONITORS_FIELD_NUMBER: _ClassVar[int]
    KILL_MONITOR_FIELD_NUMBER: _ClassVar[int]
    GET_RUN_BY_SHORT_ID_FIELD_NUMBER: _ClassVar[int]
    RESOLVE_ISSUE_FIELD_NUMBER: _ClassVar[int]
    APPEND_EVENT_FIELD_NUMBER: _ClassVar[int]
    ENSURE_OPENCODE_SERVER_FIELD_NUMBER: _ClassVar[int]
    REGISTER_REPO_FIELD_NUMBER: _ClassVar[int]
    LIST_REPOS_FIELD_NUMBER: _ClassVar[int]
    DELETE_RUN_FIELD_NUMBER: _ClassVar[int]
    UPDATE_ISSUE_FIELD_NUMBER: _ClassVar[int]
    VALIDATE_ISSUE_FILES_FIELD_NUMBER: _ClassVar[int]
    WRITE_AGENT_PROMPT_FIELD_NUMBER: _ClassVar[int]
    READ_AGENT_PROMPT_FIELD_NUMBER: _ClassVar[int]
    REPAIR_STATE_FIELD_NUMBER: _ClassVar[int]
    GET_DAEMON_LOG_FIELD_NUMBER: _ClassVar[int]
    READ_FILE_FIELD_NUMBER: _ClassVar[int]
    WRITE_FILE_FIELD_NUMBER: _ClassVar[int]
    CREATE_RUN_FIELD_NUMBER: _ClassVar[int]
    KILL_SESSION_FIELD_NUMBER: _ClassVar[int]
    LIST_SESSIONS_FIELD_NUMBER: _ClassVar[int]
    RESUME_RUN_FIELD_NUMBER: _ClassVar[int]
    QUERY_OPENCODE_SERVER_FIELD_NUMBER: _ClassVar[int]
    INJECT_INITIAL_PROMPT_FIELD_NUMBER: _ClassVar[int]
    CONTINUE_RUN_FIELD_NUMBER: _ClassVar[int]
    GET_CONFIG_FIELD_NUMBER: _ClassVar[int]
    GET_DAEMON_STATUS_FIELD_NUMBER: _ClassVar[int]
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
    list_monitors: ListMonitorsResponse
    kill_monitor: KillMonitorResponse
    get_run_by_short_id: GetRunByShortIDResponse
    resolve_issue: ResolveIssueResponse
    append_event: AppendEventResponse
    ensure_opencode_server: EnsureOpenCodeServerResponse
    register_repo: RegisterRepoResponse
    list_repos: ListReposResponse
    delete_run: DeleteRunResponse
    update_issue: UpdateIssueResponse
    validate_issue_files: ValidateIssueFilesResponse
    write_agent_prompt: WriteAgentPromptResponse
    read_agent_prompt: ReadAgentPromptResponse
    repair_state: RepairStateResponse
    get_daemon_log: GetDaemonLogResponse
    read_file: ReadFileResponse
    write_file: WriteFileResponse
    create_run: CreateRunResponse
    kill_session: KillSessionResponse
    list_sessions: ListSessionsResponse
    resume_run: ResumeRunResponse
    query_opencode_server: QueryOpenCodeServerResponse
    inject_initial_prompt: InjectInitialPromptResponse
    continue_run: ContinueRunResponse
    get_config: GetConfigResponse
    get_daemon_status: GetDaemonStatusResponse
    def __init__(self, ok: _Optional[bool] = ..., error: _Optional[str] = ..., ping: _Optional[_Union[PingResponse, _Mapping]] = ..., list_runs: _Optional[_Union[ListRunsResponse, _Mapping]] = ..., get_run: _Optional[_Union[GetRunResponse, _Mapping]] = ..., start_run: _Optional[_Union[StartRunResponse, _Mapping]] = ..., stop_run: _Optional[_Union[StopRunResponse, _Mapping]] = ..., resolve_run: _Optional[_Union[ResolveRunResponse, _Mapping]] = ..., list_issues: _Optional[_Union[ListIssuesResponse, _Mapping]] = ..., get_issue: _Optional[_Union[GetIssueResponse, _Mapping]] = ..., create_issue: _Optional[_Union[CreateIssueResponse, _Mapping]] = ..., close_issue: _Optional[_Union[CloseIssueResponse, _Mapping]] = ..., get_control_agent_launch: _Optional[_Union[GetControlAgentLaunchResponse, _Mapping]] = ..., get_attach_info: _Optional[_Union[GetAttachInfoResponse, _Mapping]] = ..., capture_session: _Optional[_Union[CaptureSessionResponse, _Mapping]] = ..., send_message: _Optional[_Union[SendMessageResponse, _Mapping]] = ..., get_diff_stats: _Optional[_Union[GetDiffStatsResponse, _Mapping]] = ..., get_branch_state: _Optional[_Union[GetBranchStateResponse, _Mapping]] = ..., get_diff: _Optional[_Union[GetDiffResponse, _Mapping]] = ..., register_monitor: _Optional[_Union[RegisterMonitorResponse, _Mapping]] = ..., unregister_monitor: _Optional[_Union[UnregisterMonitorResponse, _Mapping]] = ..., heartbeat: _Optional[_Union[HeartbeatResponse, _Mapping]] = ..., list_monitors: _Optional[_Union[ListMonitorsResponse, _Mapping]] = ..., kill_monitor: _Optional[_Union[KillMonitorResponse, _Mapping]] = ..., get_run_by_short_id: _Optional[_Union[GetRunByShortIDResponse, _Mapping]] = ..., resolve_issue: _Optional[_Union[ResolveIssueResponse, _Mapping]] = ..., append_event: _Optional[_Union[AppendEventResponse, _Mapping]] = ..., ensure_opencode_server: _Optional[_Union[EnsureOpenCodeServerResponse, _Mapping]] = ..., register_repo: _Optional[_Union[RegisterRepoResponse, _Mapping]] = ..., list_repos: _Optional[_Union[ListReposResponse, _Mapping]] = ..., delete_run: _Optional[_Union[DeleteRunResponse, _Mapping]] = ..., update_issue: _Optional[_Union[UpdateIssueResponse, _Mapping]] = ..., validate_issue_files: _Optional[_Union[ValidateIssueFilesResponse, _Mapping]] = ..., write_agent_prompt: _Optional[_Union[WriteAgentPromptResponse, _Mapping]] = ..., read_agent_prompt: _Optional[_Union[ReadAgentPromptResponse, _Mapping]] = ..., repair_state: _Optional[_Union[RepairStateResponse, _Mapping]] = ..., get_daemon_log: _Optional[_Union[GetDaemonLogResponse, _Mapping]] = ..., read_file: _Optional[_Union[ReadFileResponse, _Mapping]] = ..., write_file: _Optional[_Union[WriteFileResponse, _Mapping]] = ..., create_run: _Optional[_Union[CreateRunResponse, _Mapping]] = ..., kill_session: _Optional[_Union[KillSessionResponse, _Mapping]] = ..., list_sessions: _Optional[_Union[ListSessionsResponse, _Mapping]] = ..., resume_run: _Optional[_Union[ResumeRunResponse, _Mapping]] = ..., query_opencode_server: _Optional[_Union[QueryOpenCodeServerResponse, _Mapping]] = ..., inject_initial_prompt: _Optional[_Union[InjectInitialPromptResponse, _Mapping]] = ..., continue_run: _Optional[_Union[ContinueRunResponse, _Mapping]] = ..., get_config: _Optional[_Union[GetConfigResponse, _Mapping]] = ..., get_daemon_status: _Optional[_Union[GetDaemonStatusResponse, _Mapping]] = ...) -> None: ...
