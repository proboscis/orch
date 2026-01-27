;; Protobuf-based daemon client for orch daemon communication.
;; Hy version with Result types and macro-based error handling.

(require orch_monitor.macros *)

(import socket)
(import struct)
(import datetime [datetime])
(import pathlib [Path])
(import returns.result [Success Failure Result])

(import orch_monitor.api.orch_pb2 :as pb)
(import orch_monitor.models [Event EventType Issue IssueStatus Phase Run Status])


;; ============================================================================
;; Exceptions - Import from Python module for consistency
;; ============================================================================

(import orch_monitor.types [ProtoDaemonError ProtoDaemonNotRunningError])


;; ============================================================================
;; Types and Constants - Import from types module
;; ============================================================================

;; Reuse existing dataclasses from types module
(import orch_monitor.types [RunFilters IssueFilters 
                            ListRunsResponse ListIssuesResponse
                            ControlAgentLaunch
                            MAX_PAGE_SIZE MAX_PAGES])


;; ============================================================================
;; Proto <-> Model Conversions
;; ============================================================================

(defn model-status->proto [s]
  (setv mapping {Status.QUEUED pb.RUN_STATUS_QUEUED
                 Status.BOOTING pb.RUN_STATUS_BOOTING
                 Status.RUNNING pb.RUN_STATUS_RUNNING
                 Status.BLOCKED pb.RUN_STATUS_BLOCKED
                 Status.BLOCKED_API pb.RUN_STATUS_BLOCKED_API
                 Status.PR_OPEN pb.RUN_STATUS_PR_OPEN
                 Status.DONE pb.RUN_STATUS_DONE
                 Status.FAILED pb.RUN_STATUS_FAILED
                 Status.CANCELED pb.RUN_STATUS_CANCELED})
  (.get mapping s pb.RUN_STATUS_UNSPECIFIED))

(defn proto-status->model [s]
  (setv mapping {pb.RUN_STATUS_QUEUED Status.QUEUED
                 pb.RUN_STATUS_BOOTING Status.BOOTING
                 pb.RUN_STATUS_RUNNING Status.RUNNING
                 pb.RUN_STATUS_BLOCKED Status.BLOCKED
                 pb.RUN_STATUS_BLOCKED_API Status.BLOCKED_API
                 pb.RUN_STATUS_PR_OPEN Status.PR_OPEN
                 pb.RUN_STATUS_DONE Status.DONE
                 pb.RUN_STATUS_FAILED Status.FAILED
                 pb.RUN_STATUS_CANCELED Status.CANCELED})
  (.get mapping s Status.UNKNOWN))

(defn model-issue-status->proto [s]
  (setv mapping {IssueStatus.OPEN pb.ISSUE_STATUS_OPEN
                 IssueStatus.RESOLVED pb.ISSUE_STATUS_RESOLVED
                 IssueStatus.CLOSED pb.ISSUE_STATUS_CLOSED})
  (.get mapping s pb.ISSUE_STATUS_UNSPECIFIED))

(defn proto-issue-status->model [s]
  (setv mapping {pb.ISSUE_STATUS_OPEN IssueStatus.OPEN
                 pb.ISSUE_STATUS_RESOLVED IssueStatus.RESOLVED
                 pb.ISSUE_STATUS_CLOSED IssueStatus.CLOSED})
  (.get mapping s IssueStatus.OPEN))

(defn proto-multiplexer->str [m]
  (cond
    (= m pb.MULTIPLEXER_TMUX) "tmux"
    (= m pb.MULTIPLEXER_ZELLIJ) "zellij"
    True ""))

(defn proto-branch-state->str [s]
  (setv mapping {pb.BRANCH_STATE_CLEAN "clean"
                 pb.BRANCH_STATE_DIRTY "dirty"
                 pb.BRANCH_STATE_MERGED "merged"
                 pb.BRANCH_STATE_CONFLICT "conflict"})
  (.get mapping s ""))

;; Go's time.Time{} zero value serializes to this Unix timestamp (year 0001 AD)
(setv GO_ZERO_TIME -62135596800)

(defn _safe-timestamp [unix-ts]
  "Convert unix timestamp to datetime. Returns None for Go zero time or invalid values."
  (when (and unix-ts (!= unix-ts 0) (!= unix-ts GO_ZERO_TIME))
    (with-fallback-silent "safe_timestamp" None
      (datetime.fromtimestamp unix-ts))))

(defn proto-run->model [r]
  (setv additions 0)
  (setv deletions 0)
  (setv files-changed 0)
  (setv files [])
  (when (.HasField r "diff_stats")
    (setv additions r.diff_stats.additions)
    (setv deletions r.diff_stats.deletions)
    (setv files-changed r.diff_stats.files_changed)
    (setv files (list r.diff_stats.files)))
  
  (Run :issue_id r.issue_id
       :run_id r.run_id
       :path (Path)
       :status (proto-status->model r.status)
       :agent r.agent
       :model r.model
       :branch r.branch
       :worktree_path r.worktree_path
       :tmux_session r.tmux_session
       :multiplexer (proto-multiplexer->str r.multiplexer)
       :pr_url r.pr_url
       :server_port r.server_port
       :opencode_session_id r.opencode_session_id
       :continued_from r.continued_from
       :started_at (_safe-timestamp r.started_at_unix)
       :updated_at (_safe-timestamp r.updated_at_unix)
       :elapsed_seconds r.elapsed_seconds
       :elapsed_display r.elapsed_display
       :additions additions
       :deletions deletions
       :files_changed files-changed
       :files files
       :branch_state (proto-branch-state->str r.branch_state)))

(defn proto-issue->model [i]
  (Issue :id i.id
         :title i.title
         :summary i.summary
         :status (proto-issue-status->model i.status)
         :tags (list i.tags)
         :body i.body
         :path (if i.path (Path i.path) (Path))
         :modified_at (_safe-timestamp i.modified_at_unix)))

(defn proto-event->model [e]
  (setv event-type (try-or EventType.NOTE (EventType e.type)))
  (Event :timestamp (or (_safe-timestamp e.timestamp_unix) (datetime.now))
         :type event-type
         :name e.name
         :attrs (dict e.attrs)))


;; ============================================================================
;; Proto Daemon Client
;; ============================================================================

(defclass ProtoDaemonClient []
  
  (defn __init__ [self socket-path [issues-root None] [timeout 30.0]]
    (setv self.socket-path socket-path)
    (setv self.issues-root issues-root)
    (setv self._timeout timeout))
  
  (defn _issues-root-str [self]
    (if self.issues-root (str self.issues-root) ""))
  
  (defn is-available [self]
    (try-or False
      (import stat)
      (setv mode (. (.stat self.socket-path) st_mode))
      (stat.S_ISSOCK mode)))
  
  ;; =========================================================================
  ;; Low-level helpers
  ;; =========================================================================
  
  (defn _send [self request]
    "Send request, return response. Raises typed ProtoDaemonError on failure."
    (when (not (.is-available self))
      (raise (ProtoDaemonNotRunningError 
               f"Daemon socket not found at {self.socket-path}")))
    
    (socket-send self.socket-path
      (setv sock (socket.socket socket.AF_UNIX socket.SOCK_STREAM))
      (.settimeout sock self._timeout)
      (.connect sock (str self.socket-path))
      
      (try
        (setv data (.SerializeToString request))
        (setv length (struct.pack ">I" (len data)))
        (.sendall sock (+ length data))
        (.shutdown sock socket.SHUT_WR)
        
        ;; Read response length
        (setv len-data b"")
        (while (< (len len-data) 4)
          (setv chunk (.recv sock (- 4 (len len-data))))
          (when (not chunk) (break))
          (+= len-data chunk))
        
        (when (< (len len-data) 4)
          (raise (ProtoDaemonError "Incomplete response length")))
        
        ;; Read response
        (setv resp-len (get (struct.unpack ">I" len-data) 0))
        (setv resp-data b"")
        (while (< (len resp-data) resp-len)
          (setv chunk (.recv sock (- resp-len (len resp-data))))
          (when (not chunk) (break))
          (+= resp-data chunk))
        
        (setv response (pb.Response))
        (.ParseFromString response resp-data)
        response
        
        (finally (.close sock)))))
  
  (defn _check [self response [error-msg "Unknown error"]]
    "Check response.ok, raise ProtoDaemonError if not. Returns response."
    (when (not response.ok)
      (raise (ProtoDaemonError (or response.error error-msg))))
    response)
  
  (defn _send-ok [self request [error-msg "Unknown error"]]
    "Send request and check response.ok. Returns response."
    (._check self (._send self request) error-msg))
  
  ;; =========================================================================
  ;; Result-returning methods (high-level API)
  ;; =========================================================================
  
  (defn list-runs [self [filters None]]
    "List runs. Returns Result[ListRunsResponse, ProtoDaemonError]."
    (setv filters (or filters (RunFilters)))
    (daemon-result "list_runs"
      (setv req (pb.Request))
      (set-> req.list_runs.issues_root (._issues-root-str self)
             req.list_runs.issue_id (or filters.issue_id "")
             req.list_runs.agent (or filters.agent "")
             req.list_runs.text_search (or filters.text_search "")
             req.list_runs.time_range (or filters.time_range "")
             req.list_runs.limit MAX_PAGE_SIZE)
      (for [s filters.status]
        (.append req.list_runs.status (model-status->proto s)))
      (setv resp (._send-ok self req))
      (ListRunsResponse 
        :runs (lfor r resp.list_runs.runs (proto-run->model r))
        :next_cursor None 
        :total resp.list_runs.total)))
  
  (defn list-issues [self [filters None]]
    "List issues. Returns Result[ListIssuesResponse, ProtoDaemonError]."
    (setv filters (or filters (IssueFilters)))
    (daemon-result "list_issues"
      (setv req (pb.Request))
      (set-> req.list_issues.issues_root (._issues-root-str self)
             req.list_issues.tags_mode (or filters.tags_mode "")
             req.list_issues.text_search (or filters.text_search "")
             req.list_issues.limit MAX_PAGE_SIZE)
      (for [s filters.status]
        (.append req.list_issues.status (model-issue-status->proto s)))
      (for [tag filters.tags]
        (.append req.list_issues.tags tag))
      (setv resp (._send-ok self req))
      (ListIssuesResponse 
        :issues (lfor i resp.list_issues.issues (proto-issue->model i))
        :next_cursor None
        :total resp.list_issues.total)))
  
  (defn get-run [self issue-id run-id]
    "Get a run. Returns Result[Run | None, ProtoDaemonError]."
    (daemon-result "get_run"
      (setv req (pb.Request))
      (set-> req.get_run.issues_root (._issues-root-str self)
             req.get_run.issue_id issue-id
             req.get_run.run_id run-id)
      (setv resp (._send self req))
      (cond
        (and (not resp.ok) (= resp.error "not_found")) None
        (not resp.ok) (raise (ProtoDaemonError (or resp.error "Unknown error")))
        True (do
          (setv run (proto-run->model resp.get_run.run))
          (setv run.events (lfor e resp.get_run.events (proto-event->model e)))
          run))))
  
  (defn get-issue [self issue-id]
    "Get an issue. Returns Result[Issue | None, ProtoDaemonError]."
    (daemon-result "get_issue"
      (setv req (pb.Request))
      (set-> req.get_issue.issues_root (._issues-root-str self)
             req.get_issue.issue_id issue-id)
      (setv resp (._send self req))
      (cond
        (and (not resp.ok) (= resp.error "not_found")) None
        (not resp.ok) (raise (ProtoDaemonError (or resp.error "Unknown error")))
        True (proto-issue->model resp.get_issue.issue))))
  
  (defn start-run [self issue-id [agent ""] [model ""]]
    "Start a run. Returns Result[dict, ProtoDaemonError]."
    (daemon-result "start_run"
      (setv req (pb.Request))
      (set-> req.start_run.issues_root (._issues-root-str self)
             req.start_run.issue_id issue-id
             req.start_run.agent agent
             req.start_run.model model)
      (setv sr (. (._send-ok self req) start_run))
      {"run_id" sr.run_id "branch" sr.branch 
       "worktree" sr.worktree_path "tmux_session" sr.tmux_session}))
  
  (defn stop-run [self issue-id [run-id ""]]
    "Stop a run. Returns Result[dict, ProtoDaemonError]."
    (daemon-result "stop_run"
      (setv req (pb.Request))
      (set-> req.stop_run.issues_root (._issues-root-str self)
             req.stop_run.issue_id issue-id
             req.stop_run.run_id run-id)
      (._send-ok self req)
      {"stopped" True}))
  
  (defn send-message [self issue-id run-id message]
    "Send message to run. Returns Result[None, ProtoDaemonError]."
    (daemon-result "send_message"
      (setv req (pb.Request))
      (set-> req.send_message.issues_root (._issues-root-str self)
             req.send_message.issue_id issue-id
             req.send_message.run_id run-id
             req.send_message.message message)
      (._send-ok self req)
      None))
  
  (defn ping [self]
    "Ping daemon. Returns Result[bool, ProtoDaemonError]."
    (daemon-result "ping"
      (setv req (pb.Request))
      (.CopyFrom req.ping (pb.PingRequest))
      (setv resp (._send self req))
      (and resp.ok resp.ping.ok)))
  
  (defn get-diff-stats [self issue-id run-id]
    "Get diff stats. Returns Result[tuple | None, ProtoDaemonError]."
    (daemon-result "get_diff_stats"
      (setv req (pb.Request))
      (set-> req.get_diff_stats.issues_root (._issues-root-str self)
             req.get_diff_stats.issue_id issue-id
             req.get_diff_stats.run_id run-id)
      (setv resp (._send self req))
      (cond
        (and resp.ok (.HasField resp "get_diff_stats"))
        (do (setv ds resp.get_diff_stats.diff_stats)
            #(ds.additions ds.deletions ds.files_changed (list ds.files)))
        (not resp.ok) (raise (ProtoDaemonError (or resp.error "Failed to get diff stats")))
        True None)))
  
  (defn get-branch-state [self issue-id run-id]
    "Get branch state. Returns Result[str, ProtoDaemonError]."
    (daemon-result "get_branch_state"
      (setv req (pb.Request))
      (set-> req.get_branch_state.issues_root (._issues-root-str self)
             req.get_branch_state.issue_id issue-id
             req.get_branch_state.run_id run-id)
      (setv resp (._send self req))
      (cond
        (and resp.ok (.HasField resp "get_branch_state"))
        (proto-branch-state->str resp.get_branch_state.state)
        (not resp.ok) (raise (ProtoDaemonError (or resp.error "Failed to get branch state")))
        True "")))
  
  (defn get-diff [self issue-id run-id]
    "Get diff. Returns Result[str | None, ProtoDaemonError]."
    (daemon-result "get_diff"
      (setv req (pb.Request))
      (set-> req.get_diff.issues_root (._issues-root-str self)
             req.get_diff.issue_id issue-id
             req.get_diff.run_id run-id)
      (setv resp (._send self req))
      (cond
        (and resp.ok (.HasField resp "get_diff")) resp.get_diff.diff
        (not resp.ok) (raise (ProtoDaemonError (or resp.error "Failed to get diff")))
        True None)))
  
  (defn capture-session [self issue-id run-id]
    "Capture session. Returns Result[tuple | None, ProtoDaemonError]."
    (daemon-result "capture_session"
      (setv req (pb.Request))
      (set-> req.capture_session.issues_root (._issues-root-str self)
             req.capture_session.issue_id issue-id
             req.capture_session.run_id run-id)
      (setv resp (._send self req))
      (cond
        (and resp.ok (.HasField resp "capture_session"))
        (do (setv cs resp.capture_session) #(cs.content cs.timestamp_unix cs.source))
        (not resp.ok) (raise (ProtoDaemonError (or resp.error "Failed to capture session")))
        True None)))
  
  (defn create-issue [self issue-id title body]
    "Create issue. Returns Result[str | None, ProtoDaemonError]."
    (daemon-result "create_issue"
      (setv req (pb.Request))
      (set-> req.create_issue.issues_root (._issues-root-str self)
             req.create_issue.issue_id issue-id
             req.create_issue.title title
             req.create_issue.body body)
      (setv resp (._send self req))
      (if (and resp.ok (.HasField resp "create_issue"))
          resp.create_issue.path
          (raise (ProtoDaemonError (or resp.error "Failed to create issue"))))))
  
  (defn close-issue [self issue-id]
    "Close issue. Returns Result[None, ProtoDaemonError]."
    (daemon-result "close_issue"
      (setv req (pb.Request))
      (set-> req.close_issue.issues_root (._issues-root-str self)
             req.close_issue.issue_id issue-id)
      (._send-ok self req "Failed to close issue")
      None))
  
  (defn resolve-issue [self issue-id [force False]]
    "Resolve an issue. Returns Result[bool, ProtoDaemonError]."
    (daemon-result "resolve_issue"
      (setv req (pb.Request))
      (set-> req.resolve_issue.issues_root (._issues-root-str self)
             req.resolve_issue.issue_id issue-id
             req.resolve_issue.force force)
      (._send-ok self req "Failed to resolve issue")
      True))
  
  (defn get-control-agent-launch [self project-root [agent-type ""] [new-session False]]
    "Returns Result[ControlAgentLaunch | None, ProtoDaemonError]."
    (daemon-result "get_control_agent_launch"
      (setv req (pb.Request))
      (set-> req.get_control_agent_launch.project_root project-root
             req.get_control_agent_launch.agent agent-type
             req.get_control_agent_launch.new_session new-session)
      (setv resp (._send self req))
      (when (not resp.ok)
        (raise (ProtoDaemonError (or resp.error "Failed to get control agent launch"))))
      (setv r resp.get_control_agent_launch)
      (ControlAgentLaunch :command r.command
                          :prompt_file r.prompt_file
                          :port r.port
                          :session_id r.session_id
                          :agent (or agent-type r.agent ""))))
  
  (defn register-monitor [self pid monitor-type view project [tmux-session ""]]
    "Register monitor. Returns Result[str | None, ProtoDaemonError]."
    (daemon-result "register_monitor"
      (setv req (pb.Request))
      (set-> req.register_monitor.pid pid
             req.register_monitor.monitor_type monitor-type
             req.register_monitor.view view
             req.register_monitor.project project
             req.register_monitor.session_name tmux-session)
      (. (._send-ok self req "Failed to register monitor") register_monitor monitor_id)))
  
  (defn unregister-monitor [self monitor-id]
    "Unregister monitor. Returns Result[bool, ProtoDaemonError]."
    (daemon-result "unregister_monitor"
      (setv req (pb.Request))
      (setv req.unregister_monitor.monitor_id monitor-id)
      (._send-ok self req "Failed to unregister monitor")
      True))
  
  (defn monitor-heartbeat [self monitor-id]
    "Send heartbeat. Returns Result[bool, ProtoDaemonError]."
    (daemon-result "monitor_heartbeat"
      (setv req (pb.Request))
      (setv req.heartbeat.monitor_id monitor-id)
      (._send-ok self req "Heartbeat failed")
      True))
  
  (defn close [self]
    None))


;; ============================================================================
;; Convenience: Create client with Result-based API
;; ============================================================================

(defn create-client [socket-path [issues-root None] [timeout 30.0]]
  "Create a ProtoDaemonClient instance."
  (ProtoDaemonClient socket-path issues-root timeout))


;; ============================================================================
;; Python-friendly aliases for converter functions
;; ============================================================================

(setv proto_status_to_model proto-status->model)
(setv proto_issue_status_to_model proto-issue-status->model)
(setv proto_multiplexer_to_str proto-multiplexer->str)
(setv proto_branch_state_to_str proto-branch-state->str)
(setv proto_run_to_model proto-run->model)
(setv proto_issue_to_model proto-issue->model)
(setv proto_event_to_model proto-event->model)
(setv model_status_to_proto model-status->proto)
(setv model_issue_status_to_proto model-issue-status->proto)
(setv safe_timestamp _safe-timestamp)
