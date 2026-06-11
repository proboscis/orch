;; Protobuf-based daemon client for orch daemon communication.
;; Hy version with Result types and macro-based error handling.

(require orch_monitor.macros *)

(import socket)
(import struct)
(import threading)
(import errno)
(import datetime [datetime])
(import pathlib [Path])
(import logging)
(import returns.result [Success Failure Result])

;; Logger for connection management
(setv _conn_logger (logging.getLogger "orch_monitor.proto_client.connection"))

(import orch_monitor.api.orch_pb2 :as pb)
(import orch_monitor.models [Event EventType Issue IssueStatus Phase Run Status])


;; ============================================================================
;; Exceptions - Import from Python module for consistency
;; ============================================================================

(import orch_monitor.types [ProtoDaemonError
                            ProtoDaemonConnectionRefusedError
                            ProtoDaemonPermissionError
                            ProtoDaemonSocketMissingError
                            ProtoDaemonTimeoutError])


;; ============================================================================
;; Types and Constants - Import from types module
;; ============================================================================

;; Reuse existing dataclasses from types module
(import orch_monitor.types [RunFilters IssueFilters 
                            ListRunsResponse ListIssuesResponse
                            ControlAgentLaunch ControlAgentConfig
                            MAX_PAGE_SIZE MAX_PAGES])


;; ============================================================================
;; Proto <-> Model Conversions
;; ============================================================================

(defn model-status->proto [s]
  (setv mapping {Status.QUEUED pb.RUN_STATUS_QUEUED
                 Status.BOOTING pb.RUN_STATUS_BOOTING
                 Status.RUNNING pb.RUN_STATUS_RUNNING
                 Status.WAITING pb.RUN_STATUS_WAITING
                 Status.RATE_LIMITED pb.RUN_STATUS_RATE_LIMITED
                 Status.PR_OPEN pb.RUN_STATUS_PR_OPEN
                 Status.DONE pb.RUN_STATUS_DONE
                 Status.FAILED pb.RUN_STATUS_FAILED
                 Status.CANCELED pb.RUN_STATUS_CANCELED
                 Status.UNKNOWN pb.RUN_STATUS_UNKNOWN})
  (.get mapping s pb.RUN_STATUS_UNSPECIFIED))

(defn proto-status->model [s]
  (setv mapping {pb.RUN_STATUS_QUEUED Status.QUEUED
                 pb.RUN_STATUS_BOOTING Status.BOOTING
                 pb.RUN_STATUS_RUNNING Status.RUNNING
                 pb.RUN_STATUS_WAITING Status.WAITING
                 pb.RUN_STATUS_RATE_LIMITED Status.RATE_LIMITED
                 pb.RUN_STATUS_PR_OPEN Status.PR_OPEN
                 pb.RUN_STATUS_DONE Status.DONE
                 pb.RUN_STATUS_FAILED Status.FAILED
                 pb.RUN_STATUS_CANCELED Status.CANCELED
                 pb.RUN_STATUS_UNKNOWN Status.UNKNOWN})
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
                 pb.BRANCH_STATE_CONFLICT "conflict"
                 pb.BRANCH_STATE_AHEAD "ahead"
                 pb.BRANCH_STATE_BEHIND "behind"
                 pb.BRANCH_STATE_DIVERGED "diverged"
                 pb.BRANCH_STATE_SYNCED "synced"})
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
       :session_name r.session_name
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
;; Daemon socket health helpers
;; ============================================================================

(defn _classify-socket-error [err socket-path [context "daemon communication"]]
  (setv socket-str (str socket-path))
  (setv err-no (getattr err "errno" None))
  (cond
    (isinstance err ProtoDaemonError)
      err
    (or (isinstance err FileNotFoundError)
        (= err-no errno.ENOENT)
        (= err-no (getattr errno "ENOTSOCK" None)))
      (ProtoDaemonSocketMissingError
        f"Daemon socket not found at {socket-str}")
    (or (isinstance err ConnectionRefusedError)
        (= err-no errno.ECONNREFUSED))
      (ProtoDaemonConnectionRefusedError
        f"Daemon connection refused at {socket-str}")
    (or (isinstance err socket.timeout)
        (isinstance err TimeoutError)
        (= err-no errno.ETIMEDOUT))
      (ProtoDaemonTimeoutError
        f"Daemon request timed out at {socket-str}")
    (or (isinstance err PermissionError)
        (= err-no errno.EACCES)
        (= err-no errno.EPERM))
      (ProtoDaemonPermissionError
        f"Permission denied accessing daemon socket at {socket-str}")
    True
      (ProtoDaemonError
        f"{context} failed ({(. (type err) __name__)}): {err}")))

(defn _assert-socket-file [socket-path]
  "Validate socket path exists and is a unix socket."
  (import stat)
  (try
    (when (not (.exists socket-path))
      (raise (ProtoDaemonSocketMissingError
               f"Daemon socket not found at {socket-path}")))
    (setv mode (. (.stat socket-path) st_mode))
    (when (not (stat.S_ISSOCK mode))
      (raise (ProtoDaemonSocketMissingError
               f"Daemon socket path is not a socket: {socket-path}")))
    True
    (except [e Exception]
      (when (isinstance e ProtoDaemonError)
        (raise))
      (raise (_classify-socket-error e socket-path "daemon socket check")))))

(defn _recv-exact [sock size]
  "Read exactly size bytes or fewer if peer closes."
  (setv data b"")
  (while (< (len data) size)
    (setv chunk (.recv sock (- size (len data))))
    (when (not chunk)
      (break))
    (+= data chunk))
  data)

;; ============================================================================
;; Proto Daemon Client
;; ============================================================================

(defn _parse-host-port [addr]
  "Parse a 'host:port' (optionally 'tcp://host:port') into #(host port)."
  (setv a (.strip (str addr)))
  (when (.startswith a "tcp://")
    (setv a (cut a (len "tcp://") None)))
  (setv idx (.rfind a ":"))
  (when (< idx 0)
    (raise (ValueError f"invalid remote address (expected host:port): {addr}")))
  #((cut a 0 idx) (int (cut a (+ idx 1) None))))

(defclass ProtoDaemonClient []

  (defn __init__ [self socket-path [project-root None] [remote-addr None] [timeout 30.0] [project-id None]]
    (setv self.socket-path socket-path)
    (setv self.project-root project-root)
    ;; When remote-addr is set (host:port) the client dials TCP to a remote
    ;; daemon/master instead of the local unix socket.
    (setv self.remote-addr (if remote-addr (.strip (str remote-addr)) None))
    (when (= self.remote-addr "")
      (setv self.remote-addr None))
    ;; Normalized daemon project id ("owner-repo") used to scope list_issues /
    ;; list_runs to this project via RequestContext (matches the Go CLI).
    (setv self.project-id (if project-id (.strip (str project-id)) None))
    (when (= self.project-id "")
      (setv self.project-id None))
    (setv self._timeout timeout)
    ;; Persistent connection state
    (setv self._socket None)
    (setv self._lock (threading.RLock))
    (setv self._connected False))
  
  (defn _project-root-str [self]
    (if self.project-root (str self.project-root) ""))

  (defn _ctx-project-id [self [project-root None]]
    "Resolve the daemon project_id (normalized repo id) for RequestContext scoping.
    Prefers the client's configured project-id; otherwise normalizes a project root.
    The daemon scopes start_run / control-agent RPCs by RequestContext.project_id;
    these requests have NO project_root field."
    (if self.project-id
        self.project-id
        (do
          (import orch_monitor.config [repo_id_from_project])
          (repo_id_from_project (or project-root self.project-root)))))

  (defn check-availability [self]
    "Active daemon availability check. Returns Result[bool, ProtoDaemonError]."
    (try
      (when (not self.remote-addr)
        (_assert-socket-file self.socket-path))
      ;; Keep availability checks on the persistent client channel to avoid
      ;; separate one-shot socket churn.
      (setv req (pb.Request))
      (.CopyFrom req.ping (pb.PingRequest))
      (setv resp (._send self req))
      (Success (and resp.ok resp.ping.ok))
      (except [e ProtoDaemonError]
        (Failure e))
      (except [e Exception]
        (Failure (_classify-socket-error e self.socket-path
                  "daemon availability check")))))
  
  (defn is-available [self]
    (setv result (.check-availability self))
    (isinstance result Success))
  
  ;; =========================================================================
  ;; Connection Management (persistent connection to reduce socket churn)
  ;;
  ;; ARCHITECTURE NOTE (orch-447): this client must keep a persistent Unix
  ;; socket connection. Avoid one-shot probe sockets in request paths because
  ;; socket lifecycle churn can exhaust host security service memory.
  ;; =========================================================================
  
  (defn _connect [self]
    "Establish persistent socket connection. Called with lock held."
    (when self._socket
      (try
        (.close self._socket)
        (except [e Exception] None))
      (setv self._socket None))
    
    (if self.remote-addr
        (do
          (setv #(host port) (_parse-host-port self.remote-addr))
          (setv sock (socket.create_connection #(host port) :timeout self._timeout)))
        (do
          (setv sock (socket.socket socket.AF_UNIX socket.SOCK_STREAM))
          (.settimeout sock self._timeout)
          (.connect sock (str self.socket-path))))
    (setv self._socket sock)
    (setv self._connected True)
    (.debug _conn_logger "Established persistent connection to daemon"))
  
  (defn _disconnect [self]
    "Close persistent connection. Called with lock held."
    (when self._socket
      (try
        (.close self._socket)
        (except [e Exception] None))
      (setv self._socket None)
      (setv self._connected False)
      (.debug _conn_logger "Closed persistent connection")))
  
  (defn _ensure-connected [self]
    "Ensure we have a valid connection. Called with lock held."
    (when (or (not self._connected) (is self._socket None))
      (._connect self)
      (return))
    ;; Check if socket is still alive by peeking
    (try
      ;; Set non-blocking temporarily to check socket state
      (.setblocking self._socket False)
      (try
        (setv data (.recv self._socket 1 socket.MSG_PEEK))
        ;; If recv returns empty bytes, server closed connection
        (when (= data b"")
          (.debug _conn_logger "Server closed connection, reconnecting...")
          (._connect self))
        (finally
          (.setblocking self._socket True)
          (.settimeout self._socket self._timeout)))
      (except [e BlockingIOError]
        ;; No data available = connection is fine
        (.setblocking self._socket True)
        (.settimeout self._socket self._timeout))
      (except [e #(OSError BrokenPipeError ConnectionResetError)]
        (.debug _conn_logger (+ "Connection broken (" (. (type e) __name__) "), reconnecting..."))
        (._connect self))))
  
  ;; =========================================================================
  ;; Low-level helpers
  ;; =========================================================================
  
  (defn _send [self request]
    "Send request using persistent connection. Raises typed ProtoDaemonError on failure."
    (when (not self.remote-addr)
      (_assert-socket-file self.socket-path))

    (with [_ self._lock]
      (socket-send self.socket-path
        ;; Ensure we have a valid connection
        (._ensure-connected self)
        
        (setv max-retries 2)
        (setv retry 0)
        (setv response None)
        
        (while (and (< retry max-retries) (is response None))
          (try
            (setv data (.SerializeToString request))
            (setv length (struct.pack ">I" (len data)))
            (.sendall self._socket (+ length data))
            
            ;; Read response length
            (setv len-data b"")
            (while (< (len len-data) 4)
              (setv chunk (.recv self._socket (- 4 (len len-data))))
              (when (not chunk) (break))
              (+= len-data chunk))
            
            (when (< (len len-data) 4)
              (raise (ProtoDaemonError "Incomplete response length")))
            
            ;; Read response
            (setv resp-len (get (struct.unpack ">I" len-data) 0))
            (setv resp-data b"")
            (while (< (len resp-data) resp-len)
              (setv chunk (.recv self._socket (- resp-len (len resp-data))))
              (when (not chunk) (break))
              (+= resp-data chunk))
            
            (setv response (pb.Response))
            (.ParseFromString response resp-data)
            
            (except [e #(BrokenPipeError ConnectionResetError OSError)]
              ;; Connection lost, try to reconnect
              (+= retry 1)
              (when (< retry max-retries)
                (.debug _conn_logger (+ "Connection error (" (. (type e) __name__) "), reconnecting (attempt " (str retry) ")..."))
                (._connect self))
              (when (>= retry max-retries)
                (.warning _conn_logger (+ "Failed after " (str max-retries) " retries: " (str e)))
                (raise e)))))
         
        response)))
  
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
      (set-> req.list_runs.issue_id (or filters.issue_id "")
             req.list_runs.agent (or filters.agent "")
             req.list_runs.text_search (or filters.text_search "")
             req.list_runs.time_range (or filters.time_range "")
             req.list_runs.limit MAX_PAGE_SIZE)
      (when self.project-id
        (setv req.list_runs.context.project_id self.project-id))
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
      (set-> req.list_issues.tags_mode (or filters.tags_mode "")
             req.list_issues.text_search (or filters.text_search "")
             req.list_issues.limit MAX_PAGE_SIZE)
      (when self.project-id
        (setv req.list_issues.context.project_id self.project-id))
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
      (set-> req.get_run.issue_id issue-id
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
      (set-> req.get_issue.issue_id issue-id)
      (setv resp (._send self req))
      (cond
        (and (not resp.ok) (= resp.error "not_found")) None
        (not resp.ok) (raise (ProtoDaemonError (or resp.error "Unknown error")))
        True (proto-issue->model resp.get_issue.issue))))
  
  (defn start-run [self issue-id [agent ""] [model ""]]
    "Start a run. Returns Result[dict, ProtoDaemonError]."
    (daemon-result "start_run"
      (setv req (pb.Request))
      (set-> req.start_run.issue_id issue-id
             req.start_run.agent agent
             req.start_run.model model)
      (setv pid (._ctx-project-id self))
      (when pid
        (setv req.start_run.context.project_id pid))
      (setv sr (. (._send-ok self req) start_run))
      {"run_id" sr.run_id "branch" sr.branch 
       "worktree" sr.worktree_path "session_name" sr.session_name}))
  
  (defrpc stop-run [issue-id [run-id ""]] "stop_run"
    (setv req (pb.Request))
    (set-> req.stop_run.issue_id issue-id
           req.stop_run.run_id run-id)
    (._send-ok self req)
    {"stopped" True})
  
  (defrpc send-message [issue-id run-id message] "send_message"
    (setv req (pb.Request))
    (set-> req.send_message.issue_id issue-id
           req.send_message.run_id run-id
           req.send_message.message message)
    (._send-ok self req)
    None)
  
  (defrpc ping [] "ping"
    (setv req (pb.Request))
    (.CopyFrom req.ping (pb.PingRequest))
    (setv resp (._send self req))
    (and resp.ok resp.ping.ok))
  
  (defn get-diff-stats [self issue-id run-id]
    "Get diff stats. Returns Result[tuple | None, ProtoDaemonError]."
    (daemon-result "get_diff_stats"
      (setv req (pb.Request))
      (set-> req.get_diff_stats.issue_id issue-id
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
      (set-> req.get_branch_state.issue_id issue-id
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
      (set-> req.get_diff.issue_id issue-id
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
      (set-> req.capture_session.issue_id issue-id
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
      (set-> req.create_issue.issue_id issue-id
             req.create_issue.title title
             req.create_issue.body body)
      (setv resp (._send self req))
      (if (and resp.ok (.HasField resp "create_issue"))
          resp.create_issue.path
          (raise (ProtoDaemonError (or resp.error "Failed to create issue"))))))
  
  (defrpc close-issue [issue-id] "close_issue"
    (setv req (pb.Request))
    (set-> req.close_issue.issue_id issue-id)
    (._send-ok self req "Failed to close issue")
    None)
  
  (defrpc resolve-issue [issue-id [force False]] "resolve_issue"
    (setv req (pb.Request))
    (set-> req.resolve_issue.issue_id issue-id
           req.resolve_issue.force force)
    (._send-ok self req "Failed to resolve issue")
    True)
  
  (defn get-control-agent-launch [self project-root [agent-type ""] [new-session False]]
    "Returns Result[ControlAgentLaunch | None, ProtoDaemonError]."
    (daemon-result "get_control_agent_launch"
      (setv req (pb.Request))
      (set-> req.get_control_agent_launch.agent agent-type
             req.get_control_agent_launch.new_session new-session)
      (setv pid (._ctx-project-id self project-root))
      (when pid
        (setv req.get_control_agent_launch.context.project_id pid))
      (setv resp (._send self req))
      (when (not resp.ok)
        (raise (ProtoDaemonError (or resp.error "Failed to get control agent launch"))))
      (setv r resp.get_control_agent_launch)
      (ControlAgentLaunch :command r.command
                          :prompt_file r.prompt_file
                          :port r.port
                          :session_id r.session_id
                          :agent (or agent-type "")
                          :resumed r.resumed)))

  (defn get-control-agent-config [self project-root]
    "Returns Result[ControlAgentConfig | None, ProtoDaemonError]."
    (daemon-result "get_control_agent_config"
      (setv req (pb.Request))
      (setv pid (._ctx-project-id self project-root))
      (when pid
        (setv req.get_control_agent_config.context.project_id pid))
      (setv resp (._send self req))
      (when (not resp.ok)
        (raise (ProtoDaemonError (or resp.error "Failed to get control agent config"))))
      (setv r resp.get_control_agent_config)
      (ControlAgentConfig :prompt_content r.prompt_content
                          :agent r.agent
                          :model r.model
                          :model_variant r.model_variant
                          :extra_args (list r.extra_args)
                          :codex_home r.codex_home)))
  
  (defn register-monitor [self pid monitor-type view project [session-name ""]]
    "Register monitor. Returns Result[str | None, ProtoDaemonError]."
    (daemon-result "register_monitor"
      (setv req (pb.Request))
      (set-> req.register_monitor.pid pid
             req.register_monitor.monitor_type monitor-type
             req.register_monitor.view view
             req.register_monitor.project project
             req.register_monitor.session_name session-name)
      (. (._send-ok self req "Failed to register monitor") register_monitor monitor_id)))
  
  (defrpc unregister-monitor [monitor-id] "unregister_monitor"
    (setv req (pb.Request))
    (setv req.unregister_monitor.monitor_id monitor-id)
    (._send-ok self req "Failed to unregister monitor")
    True)
  
  (defrpc monitor-heartbeat [monitor-id] "monitor_heartbeat"
    (setv req (pb.Request))
    (setv req.heartbeat.monitor_id monitor-id)
    (._send-ok self req "Heartbeat failed")
    True)
  
  (defn close [self]
    "Close the persistent connection."
    (with [_ self._lock]
      (._disconnect self)))
  
  (defn __del__ [self]
    "Cleanup on garbage collection."
    (try
      (.close self)
      (except [e Exception] None)))

  )


;; ============================================================================
;; Convenience: Create client with Result-based API
;; ============================================================================

(defn create-client [socket-path [project-root None] [remote-addr None] [timeout 30.0] [project-id None]]
  "Create a ProtoDaemonClient instance."
  (ProtoDaemonClient socket-path project-root remote-addr timeout project-id))


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
