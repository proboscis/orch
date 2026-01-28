;; Help and Onboarding screens

(require orch_monitor.macros [with-fallback-silent safe-dismiss defon])

(import textual [on])
(import textual.screen [ModalScreen])
(import textual.binding [Binding])
(import textual.containers [Vertical])
(import textual.widgets [Label Static Button])
(import textual.app [App ComposeResult])

;; ============================================================================
;; HelpScreen
;; ============================================================================

(setv HELP_CSS "
    HelpScreen {
        align: center middle;
    }

    #help-dialog {
        width: 70;
        height: auto;
        max-height: 85%;
        padding: 1 2;
        background: $surface;
        border: thick $primary;
    }

    #help-title {
        text-align: center;
        width: 100%;
        text-style: bold;
        margin-bottom: 1;
    }

    .help-section-title {
        text-style: bold;
        margin-top: 1;
        color: $primary;
    }

    #help-content {
        height: auto;
        overflow-y: auto;
    }

    .help-key {
        color: $accent;
    }

    #close-btn {
        margin-top: 1;
        width: 100%;
    }
")

(defclass HelpScreen [(get ModalScreen None)]
  
  (setv CSS HELP_CSS)
  
  (setv BINDINGS
    [(Binding "escape" "close" "Close")
     (Binding "q" "close" "Close")
     (Binding "?" "close" "Close")])
  
  (defn compose [self]
    (with [(Vertical :id "help-dialog")]
      (yield (Label "Orch Monitor Help" :id "help-title"))
      
      (with [(Vertical :id "help-content")]
        (yield (Label "Keybindings" :classes "help-section-title"))
        (yield (Label "  ?         Show this help screen"))
        (yield (Label "  q         Quit"))
        (yield (Label "  r         Refresh data"))
        (yield (Label "  Tab       Switch between Runs/Issues tabs"))
        (yield (Label "  f         Filter runs/issues"))
        (yield (Label "  Ctrl+f    Clear all filters"))
        (yield (Label "  Enter     Attach to run / Open issue"))
        (yield (Label "  a         Attach to selected run"))
        (yield (Label "  s         Stop selected run"))
        (yield (Label "  X         Kill session (force)"))
        (yield (Label "  n         New run for selected issue"))
        (yield (Label "  o         Open issue in editor"))
        (yield (Label "  x         Close issue"))
        (yield (Label "  d         View diff for selected run"))
        
        (yield (Label "Quick Workflow" :classes "help-section-title"))
        (yield (Label "  1. Select issue in Issues tab"))
        (yield (Label "  2. Press n to start a new run"))
        (yield (Label "  3. Select agent (claude/opencode/codex)"))
        (yield (Label "  4. Monitor progress in Runs tab"))
        (yield (Label "  5. Press a or Enter to attach"))
        (yield (Label "  6. Review PR when status is pr_open"))
        
        (yield (Label "Status Legend" :classes "help-section-title"))
        (yield (Label "  queued    -> Run waiting to start"))
        (yield (Label "  booting   -> Agent starting up"))
        (yield (Label "  running   -> Agent actively working"))
        (yield (Label "  blocked   -> Agent needs input (attach!)"))
        (yield (Label "  pr_open   -> PR created, review it"))
        (yield (Label "  done      -> Work completed")))
      
      (yield (Button "Close (Esc/?/q)" :id "close-btn" :variant "primary"))))
  
  (defon (Button.Pressed "#close-btn") close_help [self]
    (safe-dismiss self None))
  
  (defn action_close [self]
    (.dismiss self None)))

;; ============================================================================
;; OnboardingScreen
;; ============================================================================

(setv ONBOARDING_CSS "
    OnboardingScreen {
        align: center middle;
    }
    #onboarding-dialog {
        width: 75;
        height: auto;
        max-height: 90%;
        padding: 1 2;
        background: $surface;
        border: thick $accent;
    }
    #onboarding-title {
        text-align: center;
        width: 100%;
        text-style: bold;
        color: $warning;
        margin-bottom: 1;
    }
    .setup-section {
        margin-top: 1;
        padding: 1;
        background: $surface-darken-1;
    }
    .code-line {
        color: $success;
        margin-left: 4;
    }
    #status-line {
        margin-top: 1;
        text-align: center;
        color: $text-muted;
    }
")

(defclass OnboardingScreen [(get ModalScreen bool)]
  
  (setv CSS ONBOARDING_CSS)
  
  (setv BINDINGS
    [(Binding "r" "retry" "Retry")
     (Binding "q" "quit_app" "Quit")
     (Binding "escape" "quit_app" "Quit")])
  
  (defn __init__ [self config-state]
    (.__init__ (super))
    (setv self.config_state config-state)
    (setv self._polling True))
  
  (defn compose [self]
    (with [(Vertical :id "onboarding-dialog")]
      (yield (Label "Orch Not Configured" :id "onboarding-title"))
      (yield (Static f"Directory: {self.config_state.project_root}"))
      (yield (Static ""))
      
      (when (not self.config_state.has_orch_dir)
        (yield (Static "[yellow]Missing:[/] .orch/ directory")))
      (when (not self.config_state.has_issues_path)
        (yield (Static "[yellow]Missing:[/] Issues path not set")))
      
      (with [(Vertical :classes "setup-section")]
        (yield (Static "[bold]Quick Setup[/]"))
        (yield (Static ""))
        (yield (Static "1. Create config directory:"))
        (yield (Static "mkdir -p .orch" :classes "code-line"))
        (yield (Static ""))
        (yield (Static "2. Set issues path (one of):"))
        (yield (Static "export ORCH_ISSUES_ROOT=~/my-issues" :classes "code-line"))
        (yield (Static "# Or in .orch/config.yaml:" :classes "code-line"))
        (yield (Static "#   issues:" :classes "code-line"))
        (yield (Static "#     path: ~/my-issues" :classes "code-line"))
        (yield (Static ""))
        (yield (Static "3. For full guided setup:"))
        (yield (Static "orch tutorial" :classes "code-line")))
      
      (yield (Static ""))
      (yield (Static "[dim]Watching for setup... (auto-continues when ready)[/]"
                     :id "status-line"))
      (yield (Static ""))
      (yield (Button "[R]etry" :id "retry-btn" :variant "primary"))
      (yield (Button "[Q]uit" :id "quit-btn"))))
  
  (defn on_mount [self]
    (.set_interval self 2.0 self._check_configuration))
  
  (defn _check_configuration [self]
    (when (not self._polling)
      (return))
    (import orch_monitor.config [detect_configuration_state])
    (setv state (detect_configuration_state))
    (when (and state.has_orch_dir state.has_issues_path)
      (setv self._polling False)
      (.notify self "Configuration detected!" :severity "information")
      (safe-dismiss self True)))
  
  (defn on_button_pressed [self event]
    (cond
      (= event.button.id "retry-btn")
        (._check_configuration self)
      (= event.button.id "quit-btn")
        (.action_quit_app self)))
  
  (defn action_retry [self]
    (._check_configuration self))
  
  (defn action_quit_app [self]
    (setv self._polling False)
    (.dismiss self False)))

;; ============================================================================
;; OnboardingApp
;; ============================================================================

(setv ONBOARDING_APP_CSS "
    Screen {
        align: center middle;
        background: $surface;
    }
")

(defclass OnboardingApp [App]
  
  (setv CSS ONBOARDING_APP_CSS)
  
  (setv BINDINGS
    [(Binding "q" "quit" "Quit")])
  
  (defn __init__ [self config-state]
    (.__init__ (super))
    (setv self.config_state config-state)
    (setv self._should_launch False))
  
  (defn on_mount [self]
    (defn on-result [result]
      (if result
          (do
            (setv self._should_launch True)
            (.exit self))
          (.exit self)))
    (.push_screen self (OnboardingScreen self.config_state) on-result))
  
  (defn should_launch [self]
    self._should_launch))

(setv __all__ ["HelpScreen" "OnboardingScreen" "OnboardingApp"])
