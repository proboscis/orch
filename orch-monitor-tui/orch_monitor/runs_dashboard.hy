;; RunsDashboard - Hy implementation with enforced error handling
;; All error handling uses macros to ensure no silent failures

(require orch_monitor.macros [with-fallback with-fallback-silent must-succeed
                              daemon-result result-unwrap-or set-> nav
                              when-ok when-err if-ok ok-or when-some when-none])

;; ============================================================================
;; Standard library imports
;; ============================================================================

(import logging)
(import os)
(import subprocess)
(import json)
(import urllib.request)

(import datetime [datetime])
(import pathlib [Path])
(import typing [Optional])

;; ============================================================================
;; Textual imports
;; ============================================================================

(import textual [on work])
(import textual.app [App ComposeResult])
(import textual.binding [Binding])
(import textual.containers [Container Vertical])
(import textual.widgets [Footer Header Input])

;; ============================================================================
;; Local imports
;; ============================================================================

(import orch_monitor.config [Config RunFilterState])
(import orch_monitor.models [DiffStats Run Status Issue])
(import orch_monitor.types [RunFilterResult])
(import orch_monitor.orch_api [OrchAPI create_orch_api])
(import orch_monitor.orch_api [RunFilters :as ApiRunFilters
                               RunStatus :as ApiRunStatus])
(import orch_monitor.converters [api_run_to_model :as _api_run_to_model
                                 api_runs_to_model :as _api_runs_to_model_runs
                                 api_issue_to_model :as _api_issue_to_model])
(import orch_monitor.confirm_screens [KillConfirmScreen])
(import orch_monitor.filter_screens [RunFilterScreen])
(import orch_monitor.help_screen [HelpScreen])
(import orch_monitor.multiplexer [Multiplexer MultiplexerType
                                  detect_current_multiplexer get_multiplexer
                                  get_multiplexer_for_run get_multiplexer_type_from_run
                                  get_session_name])
(import orch_monitor.widgets [RunTable TabbedStatsPanel model_display_name])
(import orch_monitor.app_base [get-logger _log-error _build-orch-cmd _input-has-focus
                               AUTO_REFRESH_INTERVAL ELAPSED_UPDATE_INTERVAL AGENTS])

;; ============================================================================
;; CSS
;; ============================================================================

(setv COMMON_CSS "
Screen {
    layout: vertical;
}

#main-container {
    height: 1fr;
}

DataTable {
    height: 1fr;
}

#detail-container {
    height: 40%;
    border-top: solid $accent;
}

#detail-content {
    padding: 1;
    height: 1fr;
    overflow-y: auto;
}
")

(setv RUNS_DASHBOARD_CSS (+ COMMON_CSS "
#main-container {
    layout: horizontal;
}

#runs-table {
    width: 55%;
}

#run-detail-container {
    width: 45%;
    border-left: solid $accent;
}

#run-tabs {
    height: 1fr;
}

#run-tabs > ContentSwitcher {
    height: 1fr;
}

#stats-scroll, #issue-scroll, #changes-scroll {
    height: 1fr;
    padding: 1;
}

#stats-content, #issue-content, #changes-content {
    width: 100%;
}
"))

;; ============================================================================
;; RunsDashboard Class
;; ============================================================================

(defclass RunsDashboard [App]
  "Dashboard for monitoring and managing runs."
  
  (setv CSS RUNS_DASHBOARD_CSS)
  
  (setv BINDINGS [(Binding "question_mark" "help" "Help" :key_display "?")
                  (Binding "q" "quit" "Quit")
                  (Binding "r" "refresh" "Refresh")
                  (Binding "enter" "attach" "Attach" :priority True)
                  (Binding "s" "stop" "Stop")
                  (Binding "X" "kill_session" "Kill")
                  (Binding "f" "filter" "Filter")
                  (Binding "ctrl+f" "clear_filters" "Clear Filters")
                  (Binding "d" "diff" "Diff")])
  
  ;; =========================================================================
  ;; Initialization
  ;; =========================================================================
  
  (defn __init__ [self [issues-root None] [auto-refresh True] [api None]]
    (.__init__ (super))
    (setv self.config (if issues-root
                          (Config.from_issues_root issues-root)
                          (Config.load)))
    (setv self.api (or api (create_orch_api self.config.socket_path
                                            self.config.issues_root)))
    (setv self.runs [])
    (setv self.selected_run None)
    (setv self.filter_state (.load_filters self.config))
    (setv self._auto_refresh_enabled auto-refresh)
    (setv self._base_title f"Runs [{self.config.project_root.name}]")
    (setv self.title self._base_title)
    (setv self._daemon_error None)
    (setv self._last_update None)
    (setv self._highlighted_run_ref None)
    (setv self._monitor_id None)
    (setv self._is_loading False))
  
  ;; =========================================================================
  ;; Compose UI
  ;; =========================================================================
  
  (defn compose [self]
    (yield (Header :show_clock False))
    (with [(Container :id "main-container")]
      (yield (RunTable :id "runs-table"))
      (with [(Vertical :id "run-detail-container")]
        (yield (TabbedStatsPanel :id "run-detail-tabs"))))
    (yield (Footer)))
  
  ;; =========================================================================
  ;; Lifecycle
  ;; =========================================================================
  
  (defn on_mount [self]
    (when-ok [monitor-id (.register_monitor self.api
                          :pid (os.getpid)
                          :monitor_type "python"
                          :view "runs"
                          :project (str self.config.project_root))]
      (setv self._monitor_id monitor-id))
    (._update_title self)
    (.refresh_data self)
    (when self._auto_refresh_enabled
      (.set_interval self AUTO_REFRESH_INTERVAL self._do_auto_refresh))
    (.set_interval self ELAPSED_UPDATE_INTERVAL self._update_elapsed_times))
  
  (defn on_unmount [self]
    (when self._monitor_id
      (.unregister_monitor self.api self._monitor_id)))
  
  ;; =========================================================================
  ;; Timer callbacks - Use with-fallback-silent for cosmetic updates
  ;; =========================================================================
  
  (defn _update_elapsed_times [self]
    "Update elapsed time display for active runs. Silent failure OK."
    (when (not self.runs)
      (return))
    ;; Query table with silent fallback - cosmetic update
    (setv run-table (with-fallback-silent "query_runs_table" None
                      (.query_one self "#runs-table" RunTable)))
    (when (is run-table None)
      (return))
    ;; Update each active run's elapsed time - silent fallback for each
    (for [run self.runs]
      (when (.is_active run)
        (with-fallback-silent "update_elapsed_cell" None
          (.update_cell run-table (.ref run) "elapsed" (.elapsed_time run))))))
  
  (defn _update_title [self]
    (setv count (.run_filter_count self.filter_state))
    (setv self.title
      (if (> count 0)
          f"Runs [{self.config.project_root.name}] ({count} filters)"
          f"Runs [{self.config.project_root.name}]")))
  
  (defn _do_auto_refresh [self]
    (.refresh_data self))
  
  ;; =========================================================================
  ;; Actions
  ;; =========================================================================
  
  (defn action_refresh [self]
    (when (_input-has-focus self)
      (return))
    (.refresh_data self))
  
  (defn action_help [self]
    (.push_screen self (HelpScreen)))
  
  (defn action_filter [self]
    (when (_input-has-focus self)
      (return))
    (.push_screen self
      (RunFilterScreen self.filter_state.run_filters)
      self.on_filter_result))
  
  (defn action_clear_filters [self]
    (when (_input-has-focus self)
      (return))
    (.clear_run_filters self.filter_state)
    (.save_filters self.config self.filter_state)
    (._update_title self)
    (.refresh_data self)
    (.notify self "Filters cleared"))
  
  (defn on_filter_result [self result]
    (when (is-not result None)
      (setv all-statuses (sfor s Status :if (!= s Status.UNKNOWN) s))
      (setv selected-statuses
        (if (= result.statuses all-statuses)
            []
            (lfor s result.statuses s.value)))
      (setv all-agents (set AGENTS))
      (setv selected-agents
        (if (= result.agents all-agents)
            []
            (list result.agents)))
      (setv self.filter_state.run_filters
        (RunFilterState :statuses selected-statuses
                        :agents selected-agents
                        :text_search result.text_search
                        :time_range result.time_range))
      (.save_filters self.config self.filter_state)
      (._update_title self)
      (.refresh_data self)))
  
  ;; =========================================================================
  ;; Data fetching
  ;; =========================================================================
  
  (defn refresh_data [self]
    (setv self._is_loading True)
    (cond
      self._daemon_error
        (setv self.title f"{self._base_title} | Reconnecting...")
      (not self.runs)
        (setv self.title f"{self._base_title} | Loading..."))
    (._fetch_runs self))
  
  (defn [(work :thread True :exclusive True)] _fetch_runs [self]
    (setv run-filters self.filter_state.run_filters)
    (setv status-filter [])
    (for [s run-filters.statuses]
      (with-fallback-silent "parse_status" None
        (.append status-filter (ApiRunStatus s))))
    (setv agent-filter (if (= (len run-filters.agents) 1)
                           (get run-filters.agents 0)
                           None))
    (setv filters (ApiRunFilters
                    :status status-filter
                    :agent agent-filter
                    :text_search (or run-filters.text_search None)
                    :time_range (if (!= run-filters.time_range "all")
                                    run-filters.time_range
                                    None)))
    (if-ok [response (.list_runs self.api filters)]
      (do
        (setv runs (_api_runs_to_model_runs response.runs))
        (when (> (len run-filters.agents) 1)
          (setv runs (lfor r runs :if (in r.agent run-filters.agents) r)))
        (.sort runs :key (fn [r] (or r.updated_at r.started_at datetime.min)) :reverse True)
        (.call_from_thread self self._update_runs_table runs None))
      (.call_from_thread self self._update_runs_table None (str response))))
  
  (defn _update_runs_table [self runs error]
    (setv self._is_loading False)
    (setv self._last_update (.now datetime))
    (when error
      (setv self._daemon_error error)
      (setv self.title f"{self._base_title} | Disconnected")
      (setv log-path (_log-error "list_runs" error self.config.project_root
                       :socket-path self.config.socket_path))
      (.notify self f"Daemon unavailable: {error}\nSee {log-path}"
               :severity "warning" :timeout 5)
      (return))
    (setv self._daemon_error None)
    (when (is-not runs None)
      (setv self.runs runs))
    (setv time-str (.strftime self._last_update "%H:%M:%S"))
    (setv self.title f"{self._base_title} | {time-str}")
    ;; Build diff stats and branch states
    (setv diff-stats {})
    (setv branch-states {})
    (for [run self.runs]
      (when (or (> run.additions 0) (> run.deletions 0))
        (setv (get diff-stats (.ref run))
          (DiffStats :files []
                     :total_additions run.additions
                     :total_deletions run.deletions)))
      (when run.branch_state
        (setv (get branch-states (.ref run)) run.branch_state)))
    ;; Update table
    (setv run-table (.query_one self "#runs-table" RunTable))
    (.populate run-table self.runs :diff_stats diff-stats :branch_states branch-states))
  
  ;; =========================================================================
  ;; Row selection events
  ;; =========================================================================
  
  (defn [(on RunTable.RowSelected)] on_run_selected [self event]
    (.action_attach self))
  
  (defn [(on RunTable.RowHighlighted)] on_run_highlighted [self event]
    "Track highlighted run for Enter key attach functionality."
    (setv run-ref (if event.row_key event.row_key.value None))
    (when (or (not run-ref) (not (in "#" run-ref)))
      (setv self._highlighted_run_ref None)
      (return))
    ;; Skip if already highlighted
    (when (= (getattr self "_highlighted_run_ref" None) run-ref)
      (return))
    (setv self._highlighted_run_ref run-ref)
    (setv #(issue-id run-id) (.rsplit run-ref "#" 1))
    (._fetch_run_detail self issue-id run-id run-ref))
  
  (defn [(work :thread True :exclusive True)] _fetch_run_detail [self issue-id run-id run-ref]
    (setv run (ok-or (.get_run self.api issue-id run-id) None))
    (when-some [r run]
      (setv run (_api_run_to_model r)))
    (.call_from_thread self self._set_selected_run run run-ref))
  
  (defn _set_selected_run [self run run-ref]
    (when (= (getattr self "_highlighted_run_ref" None) run-ref)
      (setv self.selected_run run)
      (._update_run_detail_panel self run)))
  
  ;; =========================================================================
  ;; Detail panel - Uses with-fallback-silent for UI queries
  ;; =========================================================================
  
  (defn _update_run_detail_panel [self run]
    "Update the detail panel with run info. Silent fallback for UI queries."
    (setv tabs-panel (with-fallback-silent "query_run_detail_tabs" None
                       (.query_one self "#run-detail-tabs" TabbedStatsPanel)))
    (when (is tabs-panel None)
      (return))
    (when (not run)
      (.clear_all tabs-panel)
      (return))
    ;; === Stats Tab ===
    (setv agent-str (or run.agent "-"))
    (setv stats-lines [f"[bold]{(.ref run)}[/bold]"
                       ""
                       f"[bold]Status:[/bold] {run.status.value}"
                       f"[bold]Elapsed:[/bold] {(.elapsed_time run)}"
                       f"[bold]Agent:[/bold] {agent-str}"])
    (when run.model
      (setv model-str (model_display_name run.model run.model_variant))
      (.append stats-lines f"[bold]Model:[/bold] {model-str}"))
    (when run.branch
      (.append stats-lines f"[bold]Branch:[/bold] {run.branch}"))
    (when run.pr_url
      (.append stats-lines f"[bold]PR:[/bold] {run.pr_url}"))
    ;; Add chat messages or session output
    (cond
      (and (= run.agent "opencode") run.server_port run.opencode_session_id)
      (do
        (.append stats-lines "")
        (.append stats-lines "[bold]Chat Messages:[/bold]")
        (setv messages (._fetch_opencode_messages self run))
        (if messages
            (for [msg (cut messages -8 None)]
              (setv role (.get msg "role" "?"))
              (setv text (cut (.get msg "text" "") 0 150))
              (when text
                (setv color (if (= role "assistant") "cyan" "green"))
                (.append stats-lines f"[{color}]{role}:[/{color}] {text}")))
            (.append stats-lines "[dim]No messages yet[/dim]")))
      run.tmux_session
      (do
        (.append stats-lines "")
        (.append stats-lines "[bold]Session Output:[/bold]")
        (setv output (._capture_session_output self run))
        (if output
            (for [line (cut output -12 None)]
              (.append stats-lines f"[dim]{line}[/dim]"))
            (.append stats-lines "[dim]No output captured[/dim]"))))
    (.update_stats tabs-panel (.join "\n" stats-lines))
    ;; === Issue Tab ===
    (setv issue (._get_issue_for_run self run))
    (if issue
        (do
          (setv issue-title (or issue.title issue.id))
          (setv issue-lines [f"[bold]{issue-title}[/bold]"])
          (when issue.tags
            (setv tags-str (.join ", " issue.tags))
            (.append issue-lines f"[dim]Tags: {tags-str}[/dim]"))
          (when issue.status
            (.append issue-lines f"[dim]Status: {issue.status.value}[/dim]"))
          (.append issue-lines "")
          (cond
            issue.body (.append issue-lines issue.body)
            issue.summary (.append issue-lines issue.summary))
          (.update_issue tabs-panel (.join "\n" issue-lines)))
        (.update_issue tabs-panel f"[dim]Issue not found: {run.issue_id}[/dim]"))
    ;; === Changes Tab ===
    (if (or (> run.additions 0) (> run.deletions 0))
        (.update_changes tabs-panel
          f"[bold]Total: [green]+{run.additions}[/green] [red]-{run.deletions}[/red][/bold]")
        (.update_changes tabs-panel "[dim]No changes detected[/dim]")))
  
  ;; =========================================================================
  ;; Helper methods - All use with-fallback-silent for non-critical ops
  ;; =========================================================================
  
  (defn _fetch_opencode_messages [self run]
    "Fetch chat messages from opencode server. Returns [] on any error."
    (when (or (not run.server_port) (not run.opencode_session_id))
      (return []))
    ;; Use with-fallback-silent - this is cosmetic, shouldn't interrupt TUI
    (with-fallback-silent "fetch_opencode_messages" []
      (setv url f"http://127.0.0.1:{run.server_port}/session/{run.opencode_session_id}/message")
      (setv req (urllib.request.Request url
                  :headers {"X-OpenCode-Directory" (or run.worktree_path "")}))
      (with [resp (urllib.request.urlopen req :timeout 2)]
        (setv data (json.loads (.decode (.read resp))))
        (setv result [])
        (for [msg data]
          (setv role (nav msg ["info"] ["role"]))
          (setv parts (.get msg "parts" []))
          (setv text (.join " " (lfor p parts :if (= (.get p "type") "text") (.get p "text" ""))))
          (when text
            (.append result {"role" (or role "") "text" text})))
        result)))
  
  (defn _get_issue_for_run [self run]
    "Get issue for a run. Returns None on error."
    (when-some [issue (ok-or (.get_issue self.api run.issue_id) None)]
      (_api_issue_to_model issue)))
  
  (defn _capture_session_output [self run]
    "Capture terminal session output. Returns [] on any error."
    (when (not run.tmux_session)
      (return []))
    ;; Use with-fallback-silent - cosmetic operation
    (with-fallback-silent "capture_session_output" []
      (setv mux-type (get_multiplexer_type_from_run run))
      (setv result
        (if (= mux-type MultiplexerType.ZELLIJ)
            (subprocess.run ["zellij" "action" "dump-screen" "/dev/stdout"]
                            :capture_output True :text True :timeout 2
                            :env (| (dict os.environ) {"ZELLIJ_SESSION_NAME" run.tmux_session}))
            (subprocess.run ["tmux" "capture-pane" "-t" run.tmux_session "-p" "-S" "-30"]
                            :capture_output True :text True :timeout 2)))
      (when (!= result.returncode 0)
        (return []))
      (setv lines (lfor line (.split result.stdout "\n") (.rstrip line)))
      (lfor line lines :if (.strip line) line)))
  
  ;; =========================================================================
  ;; Attach action
  ;; =========================================================================
  
  (defn action_attach [self]
    (when (not self.selected_run)
      (.notify self "No run selected" :severity "warning")
      (return))
    (._do_attach self self.selected_run))
  
  (defn [(work :thread True)] _do_attach [self run]
    "Attach to run in background thread to avoid blocking TUI."
    (setv current-mux-type (detect_current_multiplexer))
    (setv attach-cmd (+ (_build-orch-cmd self.config) ["attach" (.ref run)]))
    (when current-mux-type
      (setv current-mux (get_multiplexer current-mux-type))
      ;; Check for cross-session Zellij attach
      (when (= current-mux-type MultiplexerType.ZELLIJ)
        (setv run-mux-type (get_multiplexer_type_from_run run))
        (when (= run-mux-type MultiplexerType.ZELLIJ)
          (setv current-session (.get_current_session current-mux))
          (setv run-session (get_session_name run))
          (when (and current-session run-session (!= current-session run-session))
            (setv cmd-str (.join " " attach-cmd))
            (.call_from_thread self self.notify
              (+ "Cannot attach to different Zellij session from inside Zellij.\n"
                 f"Run in a separate terminal: {cmd-str}")
              :severity "warning" :timeout 15)
            (return))))
      (setv tab-name f"{run.issue_id}[{(.short_id run)}]")
      (when (.new_tab_with_command current-mux tab-name attach-cmd)
        (.call_from_thread self self.notify f"Opened tab: {tab-name}")
        (return))
      (.call_from_thread self self.notify
        "Failed to create tab, falling back to exit"
        :severity "warning"))
    (.call_from_thread self self._exit_and_attach attach-cmd))
  
  (defn _exit_and_attach [self attach-cmd]
    "Exit TUI and run attach command (must be called from main thread)."
    (.exit self)
    (subprocess.run attach-cmd))
  
  ;; =========================================================================
  ;; Stop action
  ;; =========================================================================
  
  (defn action_stop [self]
    (when (_input-has-focus self)
      (return))
    (setv run-ref (getattr self "_highlighted_run_ref" None))
    (when (not run-ref)
      (.notify self "No run selected" :severity "warning")
      (return))
    (._do_stop self run-ref)
    (.notify self f"Stopping {run-ref}"))
  
  (defn [(work :thread True)] _do_stop [self run-ref]
    (setv parts (.split run-ref "#" 1))
    (setv issue-id (get parts 0))
    (setv run-id (if (> (len parts) 1) (get parts 1) ""))
    (when-err [err (.stop_run self.api issue-id run-id)]
      (.call_from_thread self self.notify
        f"Failed to stop run: {err}"
        :severity "error"))
    (.call_from_thread self self.refresh_data))
  
  ;; =========================================================================
  ;; Diff action
  ;; =========================================================================
  
  (defn action_diff [self]
    (when (_input-has-focus self)
      (return))
    (when (not self.selected_run)
      (.notify self "No run selected" :severity "warning")
      (return))
    (when (not self.selected_run.worktree_path)
      (.notify self "Run has no worktree" :severity "warning")
      (return))
    (._do_diff_runs self self.selected_run))
  
  (defn [(work :thread True)] _do_diff_runs [self run]
    "Open diff in a new terminal tab."
    (setv current-mux-type (detect_current_multiplexer))
    (setv diff-cmd (+ (_build-orch-cmd self.config) ["diff" (.ref run)]))
    (when current-mux-type
      (setv current-mux (get_multiplexer current-mux-type))
      (setv tab-name f"diff:{(.short_id run)}")
      (when (.new_tab_with_command current-mux tab-name diff-cmd)
        (.call_from_thread self self.notify f"Opened diff: {tab-name}")
        (return))
      (.call_from_thread self self.notify
        "Failed to create tab, falling back to exit"
        :severity "warning"))
    (.call_from_thread self self._exit_and_diff_runs diff-cmd))
  
  (defn _exit_and_diff_runs [self diff-cmd]
    "Exit TUI and run diff command."
    (.exit self)
    (subprocess.run diff-cmd))
  
  ;; =========================================================================
  ;; Kill session action - Uses with-fallback for error notification
  ;; =========================================================================
  
  (defn action_kill_session [self]
    "Show kill confirmation dialog for selected run."
    (when (not self.selected_run)
      (.notify self "No run selected" :severity "warning")
      (return))
    (setv session-name (get_session_name self.selected_run))
    (when (not session-name)
      (.notify self "Run has no session" :severity "warning")
      (return))
    (setv run self.selected_run)
    (setv multiplexer (get_multiplexer_for_run run))
    (setv run-ref (.ref run))
    (defn on-confirm [confirmed]
      (when confirmed
        (._do_kill_session self session-name multiplexer run-ref)))
    (.push_screen self (KillConfirmScreen run) on-confirm))
  
  (defn [(work :thread True)] _do_kill_session [self session-name multiplexer run-ref]
    "Kill terminal session and mark run as canceled."
    ;; Use with-fallback for critical operation - needs user notification on error
    (with-fallback "kill_session" None self
      (setv session-existed (.kill_session multiplexer session-name))
      (setv stop-cmd (+ (_build-orch-cmd self.config) ["stop" run-ref]))
      (setv stop-result (subprocess.run stop-cmd :capture_output True))
      (when (!= stop-result.returncode 0)
        (setv stderr (.strip (.decode stop-result.stderr)))
        (.call_from_thread self self.notify
          (do (setv err-msg (or stderr "unknown error")) f"Failed to stop run: {err-msg}")
          :severity "error")
        (return))
      (setv msg (if session-existed
                    f"Killed session for {run-ref}"
                    f"Session already dead; run {run-ref} marked canceled"))
      (.call_from_thread self self.notify msg :severity "information")
      (.call_from_thread self self.refresh_data))))


;; ============================================================================
;; Exports
;; ============================================================================

(setv __all__ ["RunsDashboard" "RUNS_DASHBOARD_CSS" "COMMON_CSS"])
