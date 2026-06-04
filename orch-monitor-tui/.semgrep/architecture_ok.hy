(defn thin-view [bootstrap req]
  (setv session-name bootstrap.monitor_session_name)
  (.append req.list_runs.status_text "running")
  (.append req.list_runs.agents "codex")
  session-name)
