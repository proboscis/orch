;; Main Textual app for orch monitor
;; Rewritten in Hy for consistent error handling via macros

(require orch_monitor.macros [with-fallback with-fallback-silent must-succeed])

;; ============================================================================
;; Standard library imports
;; ============================================================================

(import logging)
(import os)
(import shlex)
(import shutil)
(import subprocess)
(import tempfile)
(import traceback)
(import stat)
(import json)
(import urllib.request)


(import datetime [datetime timedelta])
(import pathlib [Path])
(import typing [Optional Tuple])

(import yaml)

;; ============================================================================
;; Textual imports
;; ============================================================================

(import textual [on work])
(import textual.app [App ComposeResult])
(import textual.binding [Binding])
(import textual.containers [Container Vertical Horizontal Grid])
(import textual.screen [ModalScreen])
(import textual.widgets [Footer Header TabbedContent TabPane Button Checkbox
                         Label Static Input RadioButton RadioSet SelectionList])

;; ============================================================================
;; Local imports
;; ============================================================================

(import returns.result [Failure])

(import orch_monitor.config [Config ConfigurationState FilterState
                             IssueFilterState RunFilterState])
(import orch_monitor.models [DiffStats FileChange Issue IssueStatus Run Status])
(import orch_monitor.types [RunFilterResult IssueFilterResult])
(import orch_monitor.orch_api [OrchAPI OrchError create_orch_api
                               DaemonNotRunningError :as ApiDaemonNotRunningError])
(import orch_monitor.orch_api [Issue :as ApiIssue
                               IssueFilters :as ApiIssueFilters
                               IssueStatus :as ApiIssueStatus
                               Run :as ApiRun
                               RunFilters :as ApiRunFilters
                               RunStatus :as ApiRunStatus])
(import orch_monitor.converters [api_run_to_model :as _api_run_to_model
                                 api_runs_to_model :as _api_runs_to_model_runs
                                 api_issue_to_model :as _api_issue_to_model
                                 api_issues_to_model :as _api_issues_to_model_issues])
(import orch_monitor.confirm_screens [KillConfirmScreen CloseIssueConfirmScreen])
(import orch_monitor.multiplexer [Multiplexer MultiplexerType
                                  detect_current_multiplexer get_multiplexer
                                  get_multiplexer_for_run get_multiplexer_type_from_run
                                  get_session_name])
(import orch_monitor.widgets [DetailPanel IssueTable RunTable TabbedStatsPanel])

;; ============================================================================
;; Constants
;; ============================================================================

(setv AUTO_REFRESH_INTERVAL 5.0)
(setv ELAPSED_UPDATE_INTERVAL 2.0)
(setv MESSAGE_REFRESH_INTERVAL 2.5)

(setv AGENTS ["claude" "codex" "opencode" "gemini"])
(setv TIME_RANGES [#("hour" "Last hour")
                   #("today" "Today")
                   #("week" "This week")
                   #("all" "All time")])

;; ============================================================================
;; Logging
;; ============================================================================

(setv _logger None)

(defn get-logger []
  "Get or create the module logger."
  (global _logger)
  (when (is _logger None)
    (setv _logger (logging.getLogger "orch_monitor")))
  _logger)

(defn setup-logging [log-path]
  "Setup logging to file."
  (.mkdir log-path.parent :parents True :exist_ok True)
  (setv handler (logging.FileHandler log-path :encoding "utf-8"))
  (.setFormatter handler
    (logging.Formatter "%(asctime)s %(levelname)s %(message)s"
                       :datefmt "%Y-%m-%d %H:%M:%S"))
  (setv logger (get-logger))
  (.addHandler logger handler)
  (.setLevel logger logging.DEBUG))

(defn _log-fallback [context error]
  "Log a fallback error at warning level."
  (setv err-type (. (type error) __name__))
  (.warning (get-logger) f"[{context}] {err-type}: {error}"))

;; ============================================================================
;; Helper Functions
;; ============================================================================

(defn _log-error [operation error project-root * [socket-path None] [exc-info False]]
  "Log error to file with full context and return the log file path.
   Crashes on failure - logging is critical infrastructure."
  (import orch_monitor.xdg :as xdg)
  (setv log-path (/ project-root ".orch" "monitor-tui.log"))
  (must-succeed "log_error"
    (.mkdir log-path.parent :parents True :exist_ok True)
    (setv timestamp (cut (.strftime (.now datetime) "%Y-%m-%d %H:%M:%S.%f") 0 -3))
    (setv context-lines [])
    (setv actual-socket (or socket-path (xdg.socket_path)))
    (.append context-lines f"  socket_path: {actual-socket}")
    (.append context-lines f"  socket_exists: {(.exists actual-socket)}")
    (when (.exists actual-socket)
      (must-succeed "log_error_stat_socket"
        (setv mode (. (.stat actual-socket) st_mode))
        (.append context-lines f"  is_socket: {(stat.S_ISSOCK mode)}")))
    (.append context-lines f"  project_root: {project-root}")
    (with [f (open log-path "a")]
      (.write f f"{timestamp} [{operation}] {error}\n")
      (for [line context-lines]
        (.write f f"{line}\n"))
      (when exc-info
        (.write f "  traceback:\n")
        (for [tb-line (.splitlines (traceback.format_exc))]
          (.write f f"    {tb-line}\n")))
      (.write f "\n")))
  log-path)

(defn _get-log-path [project-root]
  "Get the log file path."
  (/ project-root ".orch" "monitor-tui.log"))

(defn _format-changed-files-lines [diff-stats [max-files 10] [path-width 35]]
  "Format changed files for display in detail panel."
  (if (or (not diff-stats) (not diff-stats.files))
      []
      (do
        (setv lines [])
        (.append lines "")
        (.append lines f"[bold]Changed Files ({diff-stats.file_count}):[/bold]")
        (for [fc (cut diff-stats.files 0 max-files)]
          (setv path fc.path)
          (when (> (len path) path-width)
            (setv path (+ "..." (cut path (- (- path-width 3)) None))))
          (setv add-plain (if fc.additions f"+{fc.additions}" ""))
          (setv del-plain (if fc.deletions f"-{fc.deletions}" ""))
          ;; Use .format for complex formatting that Hy f-strings don't support
          (setv add-str (if add-plain (.format "[green]{:>6}[/green]" add-plain) (* " " 6)))
          (setv del-str (if del-plain (.format "[red]{:>6}[/red]" del-plain) (* " " 6)))
          (setv path-padded (.ljust path path-width))
          (.append lines f"  {path-padded}  {add-str}  {del-str}"))
        (when (> (len diff-stats.files) max-files)
          (setv remaining (- (len diff-stats.files) max-files))
          (.append lines f"  [dim]... and {remaining} more file(s)[/dim]"))
        (setv separator (* "─" path-width))
        (.append lines f"  [bold]{separator}[/bold]")
        (setv total-adds diff-stats.total_additions)
        (setv total-dels diff-stats.total_deletions)
        (.append lines
          f"  [bold]Total: [green]+{total-adds}[/green] [red]-{total-dels}[/red][/bold]")
        lines)))

(defn _build-orch-cmd [config]
  "Build orch command with project root and issues root."
  (setv cmd ["orch"])
  (when config.project_root
    (.extend cmd ["--project-root" (str config.project_root)]))
  (when config.issues_root
    (.extend cmd ["--issues-root" (str config.issues_root)]))
  cmd)

(defn _get-editor-command [file-path]
  "Get editor command for opening a file.
   Returns tuple of (command_list, error_message)."
  (when (or (not file-path) (= (str file-path) ".") (not (.exists file-path)))
    (return #(None "Issue file path not found")))
  (when (.is_dir file-path)
    (return #(None "Issue path is a directory, not a file")))
  (setv editor-env (or (.get os.environ "VISUAL")
                       (.get os.environ "EDITOR")
                       "vim"))
  (setv editor-parts (with-fallback-silent "shlex_split_editor" None
                       (shlex.split editor-env)))
  (when (is editor-parts None)
    (return #(None f"Invalid editor command: {editor-env}")))
  (when (not editor-parts)
    (return #(None "Empty editor command")))
  (setv editor-executable (get editor-parts 0))
  (when (not (shutil.which editor-executable))
    (return #(None f"Editor not found: {editor-executable}")))
  #((+ editor-parts [(str file-path)]) None))

(defn _is-url-path [s]
  "Check if string is a URL path."
  (or (.startswith s "http://")
      (.startswith s "https://")
      (.startswith s "http:/")
      (.startswith s "https:/")))

(defn _get-issue-file-path [issue]
  "Get the file path for an issue, creating a temp file for GitHub issues.
   Returns tuple of (file_path, error_message)."
  (setv path-str (if issue.path (str issue.path) ""))
  (setv is-github-issue (or (.startswith issue.id "gh-")
                            (.startswith issue.id "gh#")
                            (_is-url-path path-str)))
  (setv log (get-logger))
  
  (if is-github-issue
      ;; GitHub issue - create temp file
      (do
        (when (not issue.body)
          (setv err f"GitHub issue {issue.id} has no body content")
          (.error log err)
          (return #(None err)))
        (setv github-url "")
        (cond
          (get issue.frontmatter "url" None)
            (setv github-url (get issue.frontmatter "url"))
          (_is-url-path path-str)
            (setv github-url (.replace (.replace path-str "https:/" "https://") "http:/" "http://"))
          True
            (do
              (setv err f"GitHub issue {issue.id} has no URL (path={path-str!r}, frontmatter.url missing)")
              (.error log err)
              (return #(None err))))
        (setv temp-result
          (with-fallback-silent "create_github_issue_tempfile" None
            (setv safe-id (.join "" (lfor c issue.id (if (or (.isalnum c) (in c "-_")) c "_"))))
            (setv #(fd temp-path) (tempfile.mkstemp :suffix ".md" :prefix f"orch-issue-{safe-id}-"))
            (with [f (os.fdopen fd "w")]
              (.write f f"# {(or issue.title issue.id)}\n\n")
              (.write f f"<!-- GitHub Issue: {github-url} -->\n")
              (.write f "<!-- Note: Changes here are NOT synced to GitHub -->\n\n")
              (.write f issue.body))
            (.debug log f"Created temp file for {issue.id}: {temp-path}")
            (Path temp-path)))
        (if (is temp-result None)
            (do
              (setv err f"Failed to create temp file for {issue.id}")
              (.error log err)
              #(None err))
            #(temp-result None)))
      ;; Local issue
      (do
        (when (or (not path-str) (= path-str "."))
          (setv err f"Local issue {issue.id} has no file path")
          (.error log err)
          (return #(None err)))
        (setv file-path issue.path)
        (when (not (.exists file-path))
          (setv err f"Local issue {issue.id} file not found: {file-path}")
          (.error log err)
          (return #(None err)))
        (when (.is_dir file-path)
          (setv err f"Local issue {issue.id} path is a directory: {file-path}")
          (.error log err)
          (return #(None err)))
        #(file-path None))))

(defn _input-has-focus [app]
  "Check if any Input widget has focus."
  (with-fallback-silent "input_has_focus" False
    (setv focused app.focused)
    (isinstance focused Input)))

(defn _get-available-agents [config]
  "Get list of available agents and presets from config."
  (setv agents (list AGENTS))
  (setv seen (set agents))
  (setv config-path (/ config.project_root ".orch" "config.yaml"))
  (when (.exists config-path)
    (with-fallback-silent "load_agent_presets" None
      (with [f (open config-path)]
        (setv data (yaml.safe_load f)))
      (when (isinstance data dict)
        (setv presets (.get data "presets" []))
        (for [preset presets]
          (when (isinstance preset dict)
            (setv name (.get preset "name" ""))
            (when name
              (setv backend (or (.get preset "backend") "opencode"))
              (setv preset-str f"{backend}:{name}")
              (when (not (in preset-str seen))
                (.append agents preset-str)
                (.add seen preset-str))))))))
  agents)

;; ============================================================================
;; CSS Constants
;; ============================================================================

(setv FILTER_SCREEN_CSS "
    #filter-dialog {
        width: 50;
        height: auto;
        max-height: 80%;
        padding: 1 2;
        background: $surface;
        border: thick $primary;
    }
    
    #filter-title {
        text-align: center;
        width: 100%;
        text-style: bold;
    }
    
    #filter-buttons {
        height: 3;
        align: center middle;
    }
    
    #filter-buttons Button {
        margin: 0 1;
    }
    
    .filter-section-title {
        text-style: bold;
        margin-top: 1;
    }
    
    SelectionList {
        height: 6;
        margin: 0 1;
    }
    
    #status-list {
        height: 5;
    }
    
    #agent-list {
        height: 4;
    }
    
    #time-range {
        height: auto;
        padding: 0 1;
    }
    
    #text-search-input {
        width: 100%;
        margin: 0 1;
    }
")

;; Export for Python imports  
(setv __all__ ["get_logger" "setup_logging" "_log_fallback" "_log_error"
               "_get_log_path" "_format_changed_files_lines" "_build_orch_cmd"
               "_get_editor_command" "_get_issue_file_path" "_input_has_focus"
               "_get_available_agents" "_is_url_path"
               "RunFilterResult" "IssueFilterResult" "FILTER_SCREEN_CSS"
               "AUTO_REFRESH_INTERVAL" "ELAPSED_UPDATE_INTERVAL" "MESSAGE_REFRESH_INTERVAL"
               "AGENTS" "TIME_RANGES"])
