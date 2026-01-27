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

(import orch_monitor.proto_client [ProtoDaemonError ProtoDaemonNotRunningError])


;; ============================================================================
;; Constants
;; ============================================================================

(setv MAX_PAGE_SIZE 200)
(setv MAX_PAGES 100)


;; ============================================================================
;; Data Classes - Import from Python module to avoid Hy annotation issues
;; ============================================================================

;; Reuse existing dataclasses from Python proto_client
(import orch_monitor.proto_client [RunFilters IssueFilters 
                                   ListRunsResponse ListIssuesResponse])


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
       :started_at (when r.started_at_unix 
                     (datetime.fromtimestamp r.started_at_unix))
       :updated_at (when r.updated_at_unix
                     (datetime.fromtimestamp r.updated_at_unix))
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
         :modified_at (when i.modified_at_unix
                        (datetime.fromtimestamp i.modified_at_unix))))

(defn proto-event->model [e]
  (setv event-type (try-or EventType.NOTE (EventType e.type)))
  (Event :timestamp (if e.timestamp_unix
                        (datetime.fromtimestamp e.timestamp_unix)
                        (datetime.now))
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
  
  ;; Low-level send - raises exceptions (not Result)
  (defn _send_request [self request]
    (when (not (.is-available self))
      (raise (ProtoDaemonNotRunningError 
               f"Daemon socket not found at {self.socket-path}")))
    
    (try
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
        
        (finally (.close sock)))
      
      (except [e socket.timeout]
        (raise (ProtoDaemonError "Timeout communicating with daemon")))
      (except [e ConnectionRefusedError]
        (raise (ProtoDaemonNotRunningError "Daemon is not running")))
      (except [e FileNotFoundError]
        (raise (ProtoDaemonNotRunningError 
                 f"Daemon socket not found at {self.socket-path}")))
      (except [e Exception]
        (raise (ProtoDaemonError f"Socket error: {e}")))))
  
  ;; =========================================================================
  ;; Result-returning methods (high-level API)
  ;; =========================================================================
  
  (defn list-runs [self [filters None]]
    "List runs. Returns Result[ListRunsResponse, ProtoDaemonError]."
    (setv filters (or filters (RunFilters)))
    (daemon-result "list_runs"
      (setv req (pb.Request))
      (setv req.list_runs.issues_root (._issues-root-str self))
      (setv req.list_runs.issue_id (or filters.issue_id ""))
      (for [s filters.status]
        (.append req.list_runs.status (model-status->proto s)))
      (setv req.list_runs.agent (or filters.agent ""))
      (setv req.list_runs.text_search (or filters.text_search ""))
      (setv req.list_runs.time_range (or filters.time_range ""))
      (setv req.list_runs.limit MAX_PAGE_SIZE)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Unknown error"))))
      
      (setv runs (lfor r response.list_runs.runs (proto-run->model r)))
      (ListRunsResponse :runs runs 
                        :next_cursor None 
                        :total response.list_runs.total)))
  
  (defn list-issues [self [filters None]]
    "List issues. Returns Result[ListIssuesResponse, ProtoDaemonError]."
    (setv filters (or filters (IssueFilters)))
    (daemon-result "list_issues"
      (setv req (pb.Request))
      (setv req.list_issues.issues_root (._issues-root-str self))
      (for [s filters.status]
        (.append req.list_issues.status (model-issue-status->proto s)))
      (for [tag filters.tags]
        (.append req.list_issues.tags tag))
      (setv req.list_issues.tags_mode (or filters.tags_mode ""))
      (setv req.list_issues.text_search (or filters.text_search ""))
      (setv req.list_issues.limit MAX_PAGE_SIZE)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Unknown error"))))
      
      (setv issues (lfor i response.list_issues.issues (proto-issue->model i)))
      (ListIssuesResponse :issues issues
                          :next_cursor None
                          :total response.list_issues.total)))
  
  (defn get-run [self issue-id run-id]
    "Get a run. Returns Result[Run | None, ProtoDaemonError]."
    (daemon-result "get_run"
      (setv req (pb.Request))
      (setv req.get_run.issues_root (._issues-root-str self))
      (setv req.get_run.issue_id issue-id)
      (setv req.get_run.run_id run-id)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (if (= response.error "not_found")
            (return None)
            (raise (ProtoDaemonError (or response.error "Unknown error")))))
      
      (setv run (proto-run->model response.get_run.run))
      (setv run.events (lfor e response.get_run.events (proto-event->model e)))
      run))
  
  (defn get-issue [self issue-id]
    "Get an issue. Returns Result[Issue | None, ProtoDaemonError]."
    (daemon-result "get_issue"
      (setv req (pb.Request))
      (setv req.get_issue.issues_root (._issues-root-str self))
      (setv req.get_issue.issue_id issue-id)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (if (= response.error "not_found")
            (return None)
            (raise (ProtoDaemonError (or response.error "Unknown error")))))
      
      (proto-issue->model response.get_issue.issue)))
  
  (defn start-run [self issue-id [agent ""] [model ""]]
    "Start a run. Returns Result[dict, ProtoDaemonError]."
    (daemon-result "start_run"
      (setv req (pb.Request))
      (setv req.start_run.issues_root (._issues-root-str self))
      (setv req.start_run.issue_id issue-id)
      (setv req.start_run.agent agent)
      (setv req.start_run.model model)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Unknown error"))))
      
      (setv sr response.start_run)
      {"run_id" sr.run_id
       "branch" sr.branch
       "worktree" sr.worktree_path
       "tmux_session" sr.tmux_session}))
  
  (defn stop-run [self issue-id [run-id ""]]
    "Stop a run. Returns Result[dict, ProtoDaemonError]."
    (daemon-result "stop_run"
      (setv req (pb.Request))
      (setv req.stop_run.issues_root (._issues-root-str self))
      (setv req.stop_run.issue_id issue-id)
      (setv req.stop_run.run_id run-id)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Unknown error"))))
      
      {"stopped" True}))
  
  (defn send-message [self issue-id run-id message]
    "Send message to run. Returns Result[None, ProtoDaemonError]."
    (daemon-result "send_message"
      (setv req (pb.Request))
      (setv req.send_message.issues_root (._issues-root-str self))
      (setv req.send_message.issue_id issue-id)
      (setv req.send_message.run_id run-id)
      (setv req.send_message.message message)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Unknown error"))))
      None))
  
  (defn ping [self]
    "Ping daemon. Returns Result[bool, ProtoDaemonError]."
    (daemon-result "ping"
      (setv req (pb.Request))
      (.CopyFrom req.ping (pb.PingRequest))
      (setv response (._send_request self req))
      (and response.ok response.ping.ok)))
  
  (defn get-diff-stats [self issue-id run-id]
    "Get diff stats. Returns Result[tuple | None, ProtoDaemonError]."
    (daemon-result "get_diff_stats"
      (setv req (pb.Request))
      (setv req.get_diff_stats.issues_root (._issues-root-str self))
      (setv req.get_diff_stats.issue_id issue-id)
      (setv req.get_diff_stats.run_id run-id)
      
      (setv response (._send_request self req))
      (when (and response.ok (.HasField response "get_diff_stats"))
        (setv ds response.get_diff_stats.diff_stats)
        (return #(ds.additions ds.deletions ds.files_changed (list ds.files))))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Failed to get diff stats"))))
      None))
  
  (defn get-branch-state [self issue-id run-id]
    "Get branch state. Returns Result[str, ProtoDaemonError]."
    (daemon-result "get_branch_state"
      (setv req (pb.Request))
      (setv req.get_branch_state.issues_root (._issues-root-str self))
      (setv req.get_branch_state.issue_id issue-id)
      (setv req.get_branch_state.run_id run-id)
      
      (setv response (._send_request self req))
      (when (and response.ok (.HasField response "get_branch_state"))
        (return (proto-branch-state->str response.get_branch_state.state)))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Failed to get branch state"))))
      ""))
  
  (defn get-diff [self issue-id run-id]
    "Get diff. Returns Result[str | None, ProtoDaemonError]."
    (daemon-result "get_diff"
      (setv req (pb.Request))
      (setv req.get_diff.issues_root (._issues-root-str self))
      (setv req.get_diff.issue_id issue-id)
      (setv req.get_diff.run_id run-id)
      
      (setv response (._send_request self req))
      (when (and response.ok (.HasField response "get_diff"))
        (return response.get_diff.diff))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Failed to get diff"))))
      None))
  
  (defn capture-session [self issue-id run-id]
    "Capture session. Returns Result[tuple | None, ProtoDaemonError]."
    (daemon-result "capture_session"
      (setv req (pb.Request))
      (setv req.capture_session.issues_root (._issues-root-str self))
      (setv req.capture_session.issue_id issue-id)
      (setv req.capture_session.run_id run-id)
      
      (setv response (._send_request self req))
      (when (and response.ok (.HasField response "capture_session"))
        (setv cs response.capture_session)
        (return #(cs.content cs.timestamp_unix cs.source)))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Failed to capture session"))))
      None))
  
  (defn create-issue [self issue-id title body]
    "Create issue. Returns Result[str | None, ProtoDaemonError]."
    (daemon-result "create_issue"
      (setv req (pb.Request))
      (setv req.create_issue.issues_root (._issues-root-str self))
      (setv req.create_issue.issue_id issue-id)
      (setv req.create_issue.title title)
      (setv req.create_issue.body body)
      
      (setv response (._send_request self req))
      (when (and response.ok (.HasField response "create_issue"))
        (return response.create_issue.path))
      (raise (ProtoDaemonError (or response.error "Failed to create issue")))))
  
  (defn close-issue [self issue-id]
    "Close issue. Returns Result[None, ProtoDaemonError]."
    (daemon-result "close_issue"
      (setv req (pb.Request))
      (setv req.close_issue.issues_root (._issues-root-str self))
      (setv req.close_issue.issue_id issue-id)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Failed to close issue"))))
      None))
  
  (defn resolve-issue [self issue-id [force False]]
    "Resolve an issue. Returns Result[bool, ProtoDaemonError]."
    (daemon-result "resolve_issue"
      (setv req (pb.Request))
      (setv req.resolve_issue.issues_root (._issues-root-str self))
      (setv req.resolve_issue.issue_id issue-id)
      (setv req.resolve_issue.force force)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Failed to resolve issue"))))
      True))
  
  (defn get-control-agent-launch [self project-root [agent-type ""] [new-session False]]
    "Get control agent launch info. Returns Result[tuple, ProtoDaemonError]."
    (daemon-result "get_control_agent_launch"
      (setv req (pb.Request))
      (setv req.get_control_agent_launch.project_root project-root)
      (setv req.get_control_agent_launch.agent agent-type)
      (setv req.get_control_agent_launch.new_session new-session)
      
      (setv response (._send_request self req))
      (if response.ok
          (do
            (setv r response.get_control_agent_launch)
            #(True r.command r.prompt_file r.port r.session_id agent-type None))
          #(False None None 0 None None response.error))))
  
  (defn register-monitor [self pid monitor-type view project [tmux-session ""]]
    "Register monitor. Returns Result[str | None, ProtoDaemonError]."
    (daemon-result "register_monitor"
      (setv req (pb.Request))
      (setv req.register_monitor.pid pid)
      (setv req.register_monitor.monitor_type monitor-type)
      (setv req.register_monitor.view view)
      (setv req.register_monitor.project project)
      (setv req.register_monitor.session_name tmux-session)
      
      (setv response (._send_request self req))
      (when response.ok
        (return response.register_monitor.monitor_id))
      (raise (ProtoDaemonError (or response.error "Failed to register monitor")))))
  
  (defn unregister-monitor [self monitor-id]
    "Unregister monitor. Returns Result[bool, ProtoDaemonError]."
    (daemon-result "unregister_monitor"
      (setv req (pb.Request))
      (setv req.unregister_monitor.monitor_id monitor-id)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Failed to unregister monitor"))))
      True))
  
  (defn monitor-heartbeat [self monitor-id]
    "Send heartbeat. Returns Result[bool, ProtoDaemonError]."
    (daemon-result "monitor_heartbeat"
      (setv req (pb.Request))
      (setv req.heartbeat.monitor_id monitor-id)
      
      (setv response (._send_request self req))
      (when (not response.ok)
        (raise (ProtoDaemonError (or response.error "Heartbeat failed"))))
      True))
  
  (defn close [self]
    None))


;; ============================================================================
;; Convenience: Create client with Result-based API
;; ============================================================================

(defn create-client [socket-path [issues-root None] [timeout 30.0]]
  "Create a ProtoDaemonClient instance."
  (ProtoDaemonClient socket-path issues-root timeout))
