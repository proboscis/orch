;; Error handling macros for orch-monitor-tui
;; These macros enforce consistent error handling patterns at compile time.

;; ============================================================================
;; Core Infrastructure
;; ============================================================================

(defmacro deflogger [name]
  "Define a module-level logger function that writes to the standard log file.
   Logging is critical infrastructure — OSError crashes the app (by design)."
  `(defn ~name [operation error project-root]
     (import pathlib [Path])
     (import datetime [datetime])
     (setv log-path (/ project-root ".orch" "monitor-tui.log"))
     (.mkdir (. log-path parent) :parents True :exist_ok True)
     (setv timestamp (.strftime (.now datetime) "%Y-%m-%d %H:%M:%S"))
     (with [f (open log-path "a")]
       (.write f f"{timestamp} [{operation}] {error}\n"))
     log-path))

;; ============================================================================
;; Error Context Macros
;; ============================================================================

(defmacro daemon-op [operation project-root #* body]
  "Wrap daemon operations with mandatory error logging and re-raise.
  
  Usage:
    (daemon-op \"get_diff_stats\" self.project-root
      (setv response (self._send_request req))
      response)
  
  Expands to try-except that:
  - Logs ProtoDaemonError and ProtoDaemonNotRunningError
  - Always re-raises (no silent failures)
  "
  `(do
     (import orch_monitor.types [ProtoDaemonError ProtoDaemonNotRunningError])
     (import orch_monitor.config [_log_config_error])
     (try
       ~@body
       (except [e ProtoDaemonNotRunningError]
         (_log_config_error ~operation (str e) ~project-root)
         (raise))
       (except [e ProtoDaemonError]
         (_log_config_error ~operation (str e) ~project-root)
         (raise)))))

(defmacro ui-defensive [#* body]
  "Wrap UI operations that may fail but shouldn't crash the app.
  
  Usage:
    (ui-defensive
      (.update_cell table key \"elapsed\" value))
  
  Silent failure is INTENTIONAL here - used for cosmetic updates.
  Returns None on any exception.
  "
  `(try
     ~@body
     (except [e Exception]
       None)))

(defmacro logged-catch [operation project-root exception-types #* body]
  "Catch specific exceptions with mandatory logging.
  
  Usage:
    (logged-catch \"save_config\" self.project-root [OSError]
      (with [f (open path \"w\")]
        (.write f data)))
  
  Unlike daemon-op, this catches and returns None (doesn't re-raise).
  Use when you want to handle the error gracefully but still log it.
  "
  `(do
     (import orch_monitor.config [_log_config_error])
     (try
       ~@body
       (except [e ~exception-types]
         (_log_config_error ~operation (str e) ~project-root)
         None))))

;; ============================================================================
;; Compile-Time Enforcement Macros
;; ============================================================================

(defn _is-collection? [form]
  "Check if form is a collection (list, tuple, or Hy expression)."
  (import hy.models)
  (or (isinstance form list)
      (isinstance form tuple)
      (isinstance form hy.models.Expression)
      (isinstance form hy.models.List)))

(defn _contains-bare-except? [form]
  "Check if a form contains a bare 'except: pass' pattern."
  (cond
    (not (_is-collection? form)) False
    (and (>= (len form) 3)
         (= (get form 0) 'except)
         (= (get form -1) 'None)) True
    True (any (map _contains-bare-except? form))))

(defmacro strict-block [#* body]
  "Compile-time check: forbids any silent catches in the block.
  
  Usage:
    (strict-block
      (setv result (some-operation))
      (process result))
  
  Will raise SyntaxError at macro-expansion time if body contains
  any bare 'except: pass' or 'except [e Type]: None' patterns.
  "
  (for [form body]
    (when (_contains-bare-except? form)
      (raise (SyntaxError f"Silent catch forbidden in strict-block: {form}"))))
  `(do ~@body))

;; ============================================================================
;; Result-Type Enforcement  
;; ============================================================================

;; The `returns` library provides:
;;   Success[T] - wraps successful value
;;   Failure[E] - wraps error value
;;   Result[T, E] = Success[T] | Failure[E]

(defmacro with-result [result-binding api-call on-failure #* on-success]
  "Pattern for handling Result[T, Error] types consistently.
  
  Usage:
    (with-result result (self.api.list_runs filters)
      (do  ; on-failure
        (setv error (str (.failure result)))
        (self.notify f\"Failed: {error}\" :severity \"error\"))
      ; on-success body
      (setv runs (.unwrap result))
      (process runs))
  "
  `(do
     (import returns.result [Failure])
     (setv ~result-binding ~api-call)
     (if (isinstance ~result-binding Failure)
         ~on-failure
         (do ~@on-success))))

(defmacro result-of [error-type #* body]
  "Wrap body to return Result[T, error-type]. Catches exceptions and wraps in Failure.
  
  Usage:
    (result-of DaemonError
      (setv response (send-request req))
      (parse-response response))
    
  Returns Success(last-expr) or Failure(error-type(str(e)))
  "
  `(do
     (import returns.result [Success Failure])
     (try
       (Success (do ~@body))
       (except [e Exception]
         (Failure (~error-type (str e)))))))

(defmacro result-ok [value]
  "Wrap value in Success. Shorthand for (Success value)."
  `(do
     (import returns.result [Success])
     (Success ~value)))

(defmacro result-err [error]
  "Wrap error in Failure. Shorthand for (Failure error)."
  `(do
     (import returns.result [Failure])
     (Failure ~error)))

(defmacro result-let [bindings #* body]
  "Monadic do-notation for Result (vector style). Short-circuits on first Failure.
  
  Usage:
    (result-let [user (get-user id)
                 posts (get-posts user.id)]
      (process user posts))
  "
  (if (= (len bindings) 0)
      `(do
         (import returns.result [Success])
         (Success (do ~@body)))
      (do
        (setv var (get bindings 0))
        (setv expr (get bindings 1))
        (setv rest (cut bindings 2 None))
        `(do
           (import returns.result [Failure])
           (setv __result__ ~expr)
           (if (isinstance __result__ Failure)
               __result__
               (do
                 (setv ~var (.unwrap __result__))
                 (result-let ~rest ~@body)))))))

(defn _is-bind-form? [form]
  "Check if form is (var <- expr)."
  (import hy.models)
  (and (isinstance form hy.models.Expression)
       (>= (len form) 3)
       (= (str (get form 1)) "<-")))

(defn _is-pure-form? [form]
  "Check if form is (pure expr)."
  (import hy.models)
  (and (isinstance form hy.models.Expression)
       (>= (len form) 2)
       (= (str (get form 0)) "pure")))

(defn _is-let-form? [form]
  "Check if form is (let var expr)."
  (import hy.models)
  (and (isinstance form hy.models.Expression)
       (= (len form) 3)
       (= (str (get form 0)) "let")))

(defmacro result-do [#* forms]
  "Haskell-style do-notation for Result. Short-circuits on first Failure.
  
  Usage:
    (result-do
      (user <- (get-user id))
      (let name user.name)
      (posts <- (get-posts user.id))
      (pure {:user user :name name :posts posts}))
  
  Forms:
    (var <- expr)  - Kleisli bind: unwrap Result, short-circuit on Failure
    (let var expr) - Pure binding: bind without unwrap
    (pure expr)    - Wrap in Success and return (usually last)
    expr           - Side effect: execute and continue
  "
  (if (= (len forms) 0)
      `(do
         (import returns.result [Success])
         (Success None))
      (if (= (len forms) 1)
          ;; Last form
          (do
            (setv form (get forms 0))
            (cond
              ;; (var <- expr) - bind and return
              (_is-bind-form? form)
              `(do
                 (import returns.result [Failure])
                 (setv __result__ ~(get form 2))
                 (if (isinstance __result__ Failure)
                     __result__
                     (do
                       (setv ~(get form 0) (.unwrap __result__))
                       __result__)))
              ;; (let var expr) - pure binding, return Success(None)
              (_is-let-form? form)
              `(do
                 (import returns.result [Success])
                 (setv ~(get form 1) ~(get form 2))
                 (Success None))
              ;; (pure expr) - wrap in Success
              (_is-pure-form? form)
              `(do
                 (import returns.result [Success])
                 (Success ~(get form 1)))
              ;; Other - execute and return Success(None)
              True
              `(do
                 (import returns.result [Success])
                 ~form
                 (Success None))))
          ;; Multiple forms - process first and recurse
          (do
            (setv form (get forms 0))
            (setv rest (cut forms 1 None))
            (cond
              ;; (var <- expr) - Kleisli bind and continue
              (_is-bind-form? form)
              `(do
                 (import returns.result [Failure])
                 (setv __result__ ~(get form 2))
                 (if (isinstance __result__ Failure)
                     __result__
                     (do
                       (setv ~(get form 0) (.unwrap __result__))
                       (result-do ~@rest))))
              ;; (let var expr) - pure binding and continue
              (_is-let-form? form)
              `(do
                 (setv ~(get form 1) ~(get form 2))
                 (result-do ~@rest))
              ;; (pure expr) - should only be last, but handle it
              (_is-pure-form? form)
              `(do
                 (import returns.result [Success])
                 (Success ~(get form 1)))
              ;; Other - execute side effect and continue
              True
              `(do
                 ~form
                 (result-do ~@rest)))))))

(defmacro pure [value]
  "Wrap value in Success. Use as last form in result-do."
  `(do
     (import returns.result [Success])
     (Success ~value)))

(defmacro defn-result [name params error-type #* body]
  "Define a function that returns Result[T, error-type].
  
  Usage:
    (defn-result get-user [id] UserError
      (setv user (db.find id))
      (when (none? user)
        (raise (ValueError \"User not found\")))
      user)
  
  The function automatically catches exceptions and wraps them in Failure.
  "
  `(defn ~name ~params
     (result-of ~error-type ~@body)))

(defmacro ?-> [result #* transforms]
  "Railway-oriented pipeline for Result. Applies transforms only if Success.
  
  Usage:
    (?-> (get-user id)
         (fn [u] (get-posts u.id))
         (fn [posts] (filter active? posts)))
  
  Each transform receives the unwrapped value and should return a Result.
  "
  (if (= (len transforms) 0)
      result
      `(do
         (import returns.result [Failure])
         (setv __r__ ~result)
         (if (isinstance __r__ Failure)
             __r__
             (?-> ((get ~transforms 0) (.unwrap __r__))
                  ~@(cut transforms 1 None))))))

(defmacro result-map [f result]
  "Apply function to Success value, leave Failure unchanged.
  
  Usage:
    (result-map len (get-users))  ; Success([1,2,3]) -> Success(3)
  "
  `(do
     (import returns.result [Success Failure])
     (setv __r__ ~result)
     (if (isinstance __r__ Failure)
         __r__
         (Success (~f (.unwrap __r__))))))

(defmacro result-unwrap-or [result default]
  "Unwrap Result, returning default on Failure.
  
  Usage:
    (result-unwrap-or (get-config \"key\") \"default-value\")
  "
  `(do
     (import returns.result [Failure])
     (setv __r__ ~result)
     (if (isinstance __r__ Failure)
         ~default
         (.unwrap __r__))))

;; ============================================================================
;; Result Control Flow — Idiomatic branching on Result[T, E]
;; ============================================================================

(defmacro when-ok [binding #* body]
  "Execute body only on Success, binding unwrapped value.
   
   Usage:
     (when-ok [monitor-id (register-monitor api)]
       (setv self._monitor_id monitor-id))
   
   Expands to: check result, unwrap into var, run body. Skip on Failure.
  "
  (setv var (get binding 0))
  (setv result (get binding 1))
  `(do
     (import returns.result [Failure])
     (setv __r__ ~result)
     (when (not (isinstance __r__ Failure))
       (setv ~var (.unwrap __r__))
       ~@body)))

(defmacro when-err [binding #* body]
  "Execute body only on Failure, binding the error.
   
   Usage:
     (when-err [err (.stop_run api issue-id run-id)]
       (notify self f\"Failed: {err}\" :severity \"error\"))
   
   Expands to: check result, bind failure value, run body. Skip on Success.
  "
  (setv var (get binding 0))
  (setv result (get binding 1))
  `(do
     (import returns.result [Failure])
     (setv __r__ ~result)
     (when (isinstance __r__ Failure)
       (setv ~var (.failure __r__))
       ~@body)))

(defmacro if-ok [binding ok-body err-body]
  "Branch on Result: bind unwrapped value on Success, bind error on Failure.
   
   Usage:
     (if-ok [response (.list_runs api filters)]
       ;; Success: response is the unwrapped value
       (process response)
       ;; Failure: response is the error
       (notify-error response))
   
   In ok-body, var is the unwrapped success value.
   In err-body, var is the failure error value.
  "
  (setv var (get binding 0))
  (setv result (get binding 1))
  `(do
     (import returns.result [Failure])
     (setv __r__ ~result)
     (if (isinstance __r__ Failure)
         (do
           (setv ~var (.failure __r__))
           ~err-body)
         (do
           (setv ~var (.unwrap __r__))
           ~ok-body))))

(defmacro ok-or [result default]
  "Unwrap Result on Success, return default on Failure. Alias for result-unwrap-or.
   
   Usage:
     (setv run (ok-or (.get_run api id) None))
  "
  `(result-unwrap-or ~result ~default))

;; ============================================================================
;; Optional Control Flow — Idiomatic branching on None
;; ============================================================================

(defmacro when-some [binding #* body]
  "Execute body only if expr is not None, binding value to var.
   
   Usage:
     (when-some [session (get-session run)]
       (attach session))
   
   Skip body entirely if expr is None.
  "
  (setv var (get binding 0))
  (setv expr (get binding 1))
  `(do
     (setv __v__ ~expr)
     (when (is-not __v__ None)
       (setv ~var __v__)
       ~@body)))

(defmacro when-none [expr #* body]
  "Execute body only if expr is None.
   
   Usage:
     (when-none (get-session run)
       (notify \"No session available\"))
  "
  `(when (is ~expr None)
     ~@body))

(defmacro some-or [expr default]
  "Return expr if not None, otherwise default.
   
   Usage:
     (setv name (some-or user.name \"anonymous\"))
  "
  `(do
     (setv __v__ ~expr)
     (if (is __v__ None) ~default __v__)))

;; ============================================================================
;; Proto Client Specific Macros
;; ============================================================================

(defmacro socket-send [socket-path #* body]
  "Execute socket operations, normalizing errors to typed ProtoDaemonError.
   
   Usage:
     (socket-send self.socket-path
       (setv sock (socket.socket ...))
       (.connect sock path)
       (send-and-receive sock))
   
   Catches:
     socket.timeout       -> ProtoDaemonError
     ConnectionRefusedError -> ProtoDaemonNotRunningError
     FileNotFoundError    -> ProtoDaemonNotRunningError
     Exception            -> ProtoDaemonError
   
   All errors are ALWAYS re-raised as typed exceptions (never swallowed).
  "
  `(do
     (import socket)
     (import orch_monitor.types [ProtoDaemonError ProtoDaemonNotRunningError])
     (import logging)
     (try
       ~@body
       (except [e socket.timeout]
         (raise (ProtoDaemonError "Timeout communicating with daemon")))
       (except [e ConnectionRefusedError]
         (raise (ProtoDaemonNotRunningError "Daemon is not running")))
       (except [e FileNotFoundError]
         (raise (ProtoDaemonNotRunningError
                  f"Daemon socket not found at {~socket-path}")))
       (except [e Exception]
         (setv logger (logging.getLogger "orch_monitor.proto_client"))
         (setv __err_type__ (. (type e) __name__))
         (.error logger f"[socket_send] Unexpected: {__err_type__}: {e}")
         (raise (ProtoDaemonError f"Socket error: {e}"))))))

(defn _contains-return? [form]
  "Check if form contains a (return ...) statement."
  (cond
    (not (_is-collection? form)) False
    (and (>= (len form) 1) (= (str (get form 0)) "return")) True
    True (any (map _contains-return? form))))

(defmacro daemon-result [operation #* body]
  "Execute daemon operation, return Result[T, DaemonError].
   
   Usage:
     (daemon-result \"list_runs\"
       (setv response (self._send_request req))
       (when (not response.ok)
         (raise (ProtoDaemonError response.error)))
       (parse-runs response))
   
   Catches ALL exceptions -> Failure (preserves exception type in message)
   Success path -> Success(last-expr)
   
   WARNING: Do NOT use (return ...) inside body - it bypasses Success wrapper.
   Use cond/if to select return value, or raise exceptions for errors.
   "
  (for [form body]
    (when (_contains-return? form)
      (raise (SyntaxError f"daemon-result: (return) forbidden - bypasses Success wrapper. Use cond/raise instead. Found in: {form}"))))
  `(do
     (import returns.result [Success Failure])
     (import orch_monitor.types [ProtoDaemonError ProtoDaemonNotRunningError])
     (import traceback)
     (import logging)
     (try
       (Success (do ~@body))
       (except [e ProtoDaemonNotRunningError]
         (Failure e))
       (except [e ProtoDaemonError]
         (Failure e))
       (except [e Exception]
         (setv err-type (. (type e) __name__))
         (setv tb (traceback.format_exc))
         (setv logger (logging.getLogger "orch_monitor.proto_client"))
         (.error logger (+ "Unexpected error in " ~operation ": " err-type ": " (str e) "\n" tb))
         (Failure (ProtoDaemonError (+ err-type ": " (str e))))))))

(defmacro send-daemon-request [client req operation parse-fn]
  "Standard pattern for sending daemon request and parsing response.
  
  Usage:
    (send-daemon-request self req \"list_runs\" 
      (fn [resp] (lfor r resp.list_runs.runs (proto-run->model r))))
  
  Returns Result[T, ProtoDaemonError]
  "
  `(daemon-result ~operation
     (setv response (._send_request ~client ~req))
     (when (not response.ok)
       (import orch_monitor.types [ProtoDaemonError])
       (raise (ProtoDaemonError (or response.error "Unknown error"))))
     (~parse-fn response)))

(defmacro defmethod-result [name self-param params operation #* body]
  "Define a method that returns Result[T, ProtoDaemonError].
  
  Usage:
    (defmethod-result list-runs self [filters] \"list_runs\"
      (setv req (pb.Request))
      ... build request ...
      (setv response (self._send_request req))
      (parse-response response))
  "
  `(defn ~name [~self-param ~@params]
     (daemon-result ~operation ~@body)))

;; ============================================================================
;; Metadata / Labeling
;; ============================================================================

(defmacro error-context [context-name metadata #* body]
  "Wrap code with error context metadata for debugging.
  
  Usage:
    (error-context \"fetch_runs\"
      {:retry True :timeout 5000 :critical False}
      (fetch-data))
  
  The metadata is available for introspection and can be used
  by error handlers to make decisions (retry, alert, ignore, etc.)
  "
  `(do
     (setv __error_context__ ~context-name)
     (setv __error_metadata__ ~metadata)
     ~@body))

;; ============================================================================
;; Convenience Aliases
;; ============================================================================

(defmacro try-or [default #* body]
  "Try body, return default on any exception. Simple fallback pattern."
  `(try
     ~@body
     (except [e Exception]
       ~default)))

(defmacro try-log-or [operation project-root default #* body]
  "Try body, log and return default on any exception."
  `(do
     (import orch_monitor.config [_log_config_error])
     (try
       ~@body
       (except [e Exception]
         (_log_config_error ~operation (str e) ~project-root)
         ~default))))

;; ============================================================================
;; Fallback with Notification Macro
;; ============================================================================

(defmacro with-fallback [context default app #* body]
  "Execute body with fallback value. Logs error and shows notification on failure.
   
   Usage:
     (with-fallback \"fetch_runs\" [] self
       (expensive-api-call))
   
   On exception:
     1. Logs to monitor-tui.log with traceback
     2. Shows notification via app.notify()
     3. Returns default value
   
   The 'app' parameter must be a Textual App instance with notify() method.
   "
  `(do
     (import logging)
     (import traceback)
     (try
       ~@body
       (except [e Exception]
         (setv logger (logging.getLogger "orch_monitor"))
         (setv err-type (. (type e) __name__))
         (setv err-msg f"[{~context}] {err-type}: {e}")
         (.warning logger err-msg)
         (.debug logger (traceback.format_exc))
         (when (hasattr ~app "notify")
           (.notify ~app err-msg :severity "error" :timeout 5))
         ~default))))

(defmacro with-fallback-silent [context default #* body]
  "Execute body with fallback value. Logs error but NO notification.
   
   Usage:
     (with-fallback-silent \"update_cell\" None
       (table.update_cell key col val))
   
   Use for non-critical operations where notification would be noisy.
   "
  `(do
     (import logging)
     (try
       ~@body
       (except [e Exception]
         (setv logger (logging.getLogger "orch_monitor"))
         (setv err-type (. (type e) __name__))
         (.debug logger f"[{~context}] {err-type}: {e}")
         ~default))))

(defmacro must-succeed [context #* body]
  "Execute body. On error, log with full traceback and re-raise.
   
   Usage:
     (must-succeed \"critical_init\"
       (initialize-components))
   
   Use for operations that MUST NOT silently fail.
   "
  `(do
     (import logging)
     (import traceback)
     (try
       ~@body
       (except [e Exception]
         (setv logger (logging.getLogger "orch_monitor"))
         (setv err-type (. (type e) __name__))
         (.error logger f"[{~context}] CRITICAL: {err-type}: {e}")
         (.error logger (traceback.format_exc))
         (raise)))))

;; ============================================================================
;; UI Component Macros
;; ============================================================================



;; ============================================================================
;; Assignment Macros
;; ============================================================================

(setv _augment-ops #{"+=" "-=" "*=" "/=" "//=" "%=" "**=" "&=" "|=" "^=" "<<=" ">>="})
(setv _augment-op-map {"+=" '+ "-=" '- "*=" '* "/=" '/ "//=" '// "%=" '%
                       "**=" '** "&=" '& "|=" '| "^=" '^ "<<=" '<< ">>=" '>>})

(defn _is-augment-op? [sym]
  "Check if symbol is an augmented assignment operator."
  (in (str sym) _augment-ops))

(defn _get-augment-fn [sym]
  "Get the function symbol for an augmented operator."
  (.get _augment-op-map (str sym)))

(defn _normalize-target [target]
  "Normalize target: [a b] -> (get a b), [a b c d] -> (get (get (get a b) c) d).
   Supports nested access: [d \"a\" \"b\" \"c\"] for deep dictionary paths."
  (import hy.models [List])
  (if (isinstance target List)
      (if (>= (len target) 2)
          ;; Build nested gets: [d "a" "b"] -> (get (get d "a") "b")
          (do
            (setv base (get target 0))
            (setv keys (cut target 1 None))
            (for [key keys]
              (setv base `(get ~base ~key)))
            base)
          (raise (SyntaxError f"set->: bracket target needs at least 2 elements: {target}")))
      target))

(defmacro -> [head #* forms]
  "Thread-first macro. Inserts result as first arg of each form.
   
   (-> x (f a) (g b))  =>  (g (f x a) b)
   
   Handles special cases:
     symbol     -> attribute access: (-> x attr) => x.attr
     (f args)   -> insert as first: (-> x (f a)) => (f x a)
     [k1 k2]    -> nested get: (-> x [\"a\" \"b\"]) => (get (get x \"a\") \"b\")
   
   Examples:
     (-> d (get \"a\") (get \"b\"))           ; d[\"a\"][\"b\"]
     (-> obj attr items (get 0) name)       ; obj.attr.items[0].name
     (-> s (upper) (strip))                 ; s.upper().strip()
     (-> x [\"a\" \"b\" \"c\"])                ; x[\"a\"][\"b\"][\"c\"]
  "
  (import hy.models [Symbol List Expression])
  (setv result head)
  (for [form forms]
    (setv result
      (cond
        ;; Symbol -> attribute access
        (isinstance form Symbol)
        `(. ~result ~form)
        
        ;; List [k1 k2 ...] -> nested get
        (isinstance form List)
        (do
          (setv r result)
          (for [k form]
            (setv r `(get ~r ~k)))
          r)
        
        ;; Expression (f args) -> check for method (.method) vs function
        (isinstance form Expression)
        (do
          (setv fn-name (get form 0))
          (setv fn-args (list (cut form 1 None)))
          (setv fn-str (str fn-name))
          ;; If starts with ".", it's a method call: (-> x (.upper)) => (x.upper)
          (if (and (isinstance fn-name Symbol) (.startswith fn-str "."))
              (do
                (setv method-name (Symbol (cut fn-str 1 None)))
                `((. ~result ~method-name) ~@fn-args))
              ;; Otherwise normal function: (-> x (f a)) => (f x a)
              `(~fn-name ~result ~@fn-args)))
        
        True
        (raise (SyntaxError (.format "->: unknown form type: {}" form))))))
  result)

(defmacro ->> [head #* forms]
  "Thread-last macro. Inserts result as last arg of each form.
   
   (->> x (f a) (g b))  =>  (g b (f a x))
   
   Useful for sequence operations where data comes last.
   
   Examples:
     (->> items (map str) (filter bool) list)
     (->> (range 10) (map inc) (filter even?) list)
  "
  (import hy.models [Symbol List Expression])
  (setv result head)
  (for [form forms]
    (setv result
      (cond
        ;; Symbol -> just call it
        (isinstance form Symbol)
        `(~form ~result)
        
        ;; List [k1 k2 ...] -> nested get (same as ->)
        (isinstance form List)
        (do
          (setv r result)
          (for [k form]
            (setv r `(get ~r ~k)))
          r)
        
        ;; Expression (f args) -> check for method (.method) vs function
        (isinstance form Expression)
        (do
          (setv fn-name (get form 0))
          (setv fn-args (list (cut form 1 None)))
          (setv fn-str (str fn-name))
          ;; If starts with ".", it's a method call (same as ->)
          (if (and (isinstance fn-name Symbol) (.startswith fn-str "."))
              (do
                (setv method-name (Symbol (cut fn-str 1 None)))
                `((. ~result ~method-name) ~@fn-args))
              ;; Otherwise thread as LAST arg: (f a b x)
              `(~fn-name ~@fn-args ~result)))
        
        True
        (raise (SyntaxError (.format "->>: unknown form type: {}" form))))))
  result)

(defmacro get-in [target #* keys]
  "Get nested value from dicts/lists. Like Clojure's get-in.
   
   (get-in d \"a\" \"b\" \"c\")  ; same as d[\"a\"][\"b\"][\"c\"]
   (get-in obj.data \"x\" 0 \"y\")  ; mixed dict/list access
  "
  (setv result target)
  (for [key keys]
    (setv result `(get ~result ~key)))
  result)

(defn path-get [data path]
  "Runtime version of get-in. Path is a list/tuple of keys.
   
   (path-get data [\"users\" 0 \"name\"])
   
   Use when path is computed at runtime.
  "
  (setv result data)
  (for [key path]
    (setv result (get result key)))
  result)

(defn path-set [data path value]
  "Set nested value. Returns NEW dict/list (doesn't mutate original).
   
   (path-set data [\"users\" 0 \"name\"] \"Bob\")
   
   Like Clojure's assoc-in - functional update.
  "
  (if (= (len path) 0)
      value
      (do
        (setv key (get path 0))
        (setv rest-path (cut path 1 None))
        (setv current (get data key))
        (setv new-val (path-set current rest-path value))
        ;; Return new dict/list with updated value
        (if (isinstance data dict)
            (| (dict data) {key new-val})
            (do
              (setv new-list (list data))
              (setv (get new-list key) new-val)
              new-list)))))

(defn path-update [data path f]
  "Update nested value by applying f. Returns NEW structure.
   
   (path-update data [\"count\"] inc)
   (path-update data [\"users\" 0 \"age\"] (fn [x] (+ x 1)))
  "
  (setv current (path-get data path))
  (path-set data path (f current)))

;; ============================================================================
;; Path as First-Class Value
;; ============================================================================
;; 
;; A Path is a list of steps. Each step is a tuple:
;;   (:attr "name")           - attribute access
;;   (:item key)              - single item access  
;;   (:items [k1 k2 ...])     - nested item access
;;   (:call "method" args...) - method call
;;
;; Paths can be created, stored, composed, and applied to any data.

(defn path-apply [data path-steps]
  "Apply a path (list of steps) to data. Returns the traversed value.
   
   (path-apply obj [(, :attr \"data\") (, :item \"key\")])
  "
  (setv result data)
  (for [step path-steps]
    (setv op (get step 0))
    (setv result
      (cond
        (= op :attr)
        (getattr result (get step 1))
        
        (= op :item)
        (get result (get step 1))
        
        (= op :items)
        (do
          (setv r result)
          (for [k (get step 1)]
            (setv r (get r k)))
          r)
        
        (= op :call)
        (do
          (setv method-name (get step 1))
          (setv args (if (> (len step) 2) (list (cut step 2 None)) []))
          ((getattr result method-name) #* args))
        
        True
        (raise (ValueError (.format "Unknown path op: {}" op))))))
  result)

(defn path-concat [#* paths]
  "Concatenate multiple paths into one."
  (setv result [])
  (for [p paths]
    (.extend result p))
  result)

(defmacro path [#* steps]
  "Create a reusable path from traversal steps.
   
   (setv p (path .data [\"users\" 0] .name (.upper)))
   (path-apply response p)
   
   Steps:
     .attr        -> attribute access
     [k1 k2 ...]  -> item access (can be nested)
     (.method a)  -> method call
   
   Returns a list of path steps that can be stored, passed, composed.
  "
  (import hy.models [Symbol List Expression Keyword])
  (setv result [])
  (for [step steps]
    (cond
      ;; (.method args) -> [:call \"method\" args...]
      (_is-method-call? step)
      (do
        (setv method-name (str (_get-dot-attr (get step 0))))
        (setv args (list (cut step 1 None)))
        (.append result `[:call ~method-name ~@args]))
      
      ;; (. None attr) -> [:attr \"name\"]
      (_is-dot-accessor? step)
      (.append result `[:attr ~(str (_get-dot-attr step))])
      
      ;; Symbol starting with . -> [:attr \"name\"]
      (and (isinstance step Symbol) (.startswith (str step) "."))
      (.append result `[:attr ~(cut (str step) 1 None)])
      
      ;; [k1 k2 ...] -> [:items [k1 k2 ...]]
      (isinstance step List)
      (if (= (len step) 1)
          (.append result `[:item ~(get step 0)])
          (.append result `[:items ~(list step)]))
      
      True
      (raise (SyntaxError (.format "path: invalid step {}" step)))))
  `~result)

(defn _is-dot-accessor? [form]
  "Check if form is a (. None attr) expression (how Hy parses .attr)."
  (import hy.models [Expression Symbol])
  (and (isinstance form Expression)
       (= (len form) 3)
       (= (str (get form 0)) ".")
       (= (str (get form 1)) "None")))

(defn _get-dot-attr [form]
  "Extract attr name from (. None attr) expression."
  (get form 2))

(defn _is-method-call? [form]
  "Check if form is (.method args) - a method call expression."
  (import hy.models [Expression])
  (and (isinstance form Expression)
       (>= (len form) 1)
       (_is-dot-accessor? (get form 0))))

(defmacro nav [target #* steps]
  "Structure traversal - data flows FROM source. Attrs, items, and methods allowed.
   
   Python: x.attr['hello'].method()['key']
   Hy:     (nav x .attr [\"hello\"] (.method) [\"key\"])
   
   Semantics: Everything in the chain originates from the source.
   
   Allowed:
   - .name       -> attribute access
   - [keys]      -> item access (supports nested: [\"a\" \"b\"])
   - (.method a) -> method call (bound to object in chain)
   
   NOT allowed:
   - (external-fn args) -> breaks the 'from source' guarantee
   
   Examples:
     (nav obj .data)                     ; obj.data
     (nav obj .data [\"key\"])            ; obj.data[\"key\"]
     (nav d [\"a\" \"b\" \"c\"])             ; d[\"a\"][\"b\"][\"c\"]
     (nav obj .items [0] .name)          ; obj.items[0].name
     (nav response (.json) [\"data\" 0])  ; response.json()[\"data\"][0]
     (nav s (.strip) (.upper))           ; s.strip().upper()
  "
  (import hy.models [Symbol List Expression])
  (setv result target)
  (for [step steps]
    (setv result
      (cond
        ;; (.method args) -> method call on current result
        (_is-method-call? step)
        (do
          (setv method-accessor (get step 0))  ; (. None method)
          (setv method-name (_get-dot-attr method-accessor))
          (setv method-args (list (cut step 1 None)))
          `((. ~result ~method-name) ~@method-args))
        
        ;; (. None attr) -> attribute access (how Hy parses .attr)
        (_is-dot-accessor? step)
        `(. ~result ~(_get-dot-attr step))
        
        ;; Symbol starting with . -> attribute access (fallback)
        (and (isinstance step Symbol) (.startswith (str step) "."))
        (do
          (setv attr-name (Symbol (cut (str step) 1 None)))
          `(. ~result ~attr-name))
        
        ;; List [k1 k2 ...] -> nested item access
        (isinstance step List)
        (do
          (setv r result)
          (for [k step]
            (setv r `(get ~r ~k)))
          r)
        
        ;; Anything else is forbidden - breaks "from source" guarantee
        True
        (raise (SyntaxError (.format "nav: only .attr, [key], (.method) allowed. External fn breaks source guarantee. Got: {}" step))))))
  result)

(defn _chain-step [result step]
  "Process a single step in a chain. Returns new expression.
   
   Step types:
     symbol       -> attribute access: (. result symbol)
     [k1 k2 ...]  -> nested get: (get (get result k1) k2) ...
     (fn args)    -> method call: (.fn result args)
  "
  (import hy.models [Symbol List Expression])
  (cond
    ;; Symbol -> attribute access
    (isinstance step Symbol)
    `(. ~result ~step)
    
    ;; List [k1 k2 ...] -> nested get  
    (isinstance step List)
    (do
      (setv r result)
      (for [k step]
        (setv r `(get ~r ~k)))
      r)
    
    ;; Expression (fn ...) -> method call: ((. result fn) args)
    (isinstance step Expression)
    (do
      (setv fn-name (get step 0))
      (setv fn-args (cut step 1 None))
      ;; Build ((. result method) args...)
      `((. ~result ~fn-name) ~@fn-args))
    
    ;; Fallback
    True
    (raise (SyntaxError (.format "chain: unknown step type {}: {}" (type step) step)))))

(defmacro chain [start #* steps]
  "Chain attribute access, item access, and method calls. Python-style chaining.
   
   Python: x.attr['hello'].something.do()['key-1']
   Hy:     (chain x attr [\"hello\"] something (do) [\"key-1\"])
   
   Step types:
     attr         -> .attr (attribute access)
     [\"key\"]      -> [\"key\"] (item access)  
     [\"a\" \"b\"]   -> [\"a\"][\"b\"] (nested item access)
     (method arg) -> .method(arg) (method call)
   
   Examples:
     (chain obj data)                    ; obj.data
     (chain obj data [\"key\"])           ; obj.data[\"key\"]
     (chain obj items [0] name)          ; obj.items[0].name
     (chain obj (get-data) [\"result\"])  ; obj.get_data()[\"result\"]
     (chain d [\"a\" \"b\" \"c\"])           ; d[\"a\"][\"b\"][\"c\"]
  "
  (setv result start)
  (for [step steps]
    (setv result (_chain-step result step)))
  result)

(defmacro set-> [#* args]
  "Multi-assignment macro with support for nested attrs, computed keys, and augmented ops.
   
   Simple assignment (pairs):
     (set-> obj.foo.bar 1
            [d \"key\"] \"value\"
            simple-var 42)
   
   Augmented assignment (triplets with operator):
     (set-> player.health -= 10
            player.score += 100
            [obj.items \"count\"] *= 2)
   
   Mixed:
     (set-> player.health -= 10
            player.name \"Alice\"
            [game.stats \"plays\"] += 1)
   
   Supported operators: += -= *= /= //= %= **= &= |= ^= <<= >>=
   
   Targets can be:
     - Simple variables: x
     - Dotted attributes: obj.foo.bar  
     - Bracket syntax: [d \"key\"] -> (get d \"key\")
     - Nested bracket: [obj.items \"key\"] -> (get obj.items \"key\")
     - Explicit get: (get d \"key\") (still supported)
  "
  (setv assignments [])
  (setv i 0)
  (setv args-list (list args))
  
  (while (< i (len args-list))
    (setv raw-target (get args-list i))
    (setv target (_normalize-target raw-target))
    
    ;; Look ahead to determine if this is pairs or triplets
    (when (>= (+ i 1) (len args-list))
      (raise (SyntaxError f"set->: missing value for target")))
    
    (setv next-val (get args-list (+ i 1)))
    
    ;; Check if next is an augmented operator
    (if (_is-augment-op? next-val)
        ;; Triplet: target op value
        (do
          (when (>= (+ i 2) (len args-list))
            (raise (SyntaxError f"set->: missing value after operator '{next-val}'")))
          (setv op-fn (_get-augment-fn next-val))
          (setv value (get args-list (+ i 2)))
          ;; Build: (setv target (op target value))
          (.append assignments `(setv ~target (~op-fn ~target ~value)))
          (+= i 3))
        ;; Pair: target value  
        (do
          (.append assignments `(setv ~target ~next-val))
          (+= i 2))))
  
  `(do ~@assignments))

;; ============================================================================
;; Screen Dismissal
;; ============================================================================

(defmacro safe-dismiss [self result]
  "Dismiss a ModalScreen safely from message handlers.
   
   dismiss() returns AwaitComplete. If a message handler returns an awaitable,
   Textual automatically awaits it - which triggers ScreenError. We must
   discard the return value so the handler returns None instead."
  `(do
     (.dismiss ~self ~result)
     None))

(defmacro defon [event-spec name params #* body]
  "Define a Textual @on message handler with compile-time safety.
   Rejects raw .dismiss calls — forces safe-dismiss to prevent
   the push_screen(callback) + dismiss-from-handler deadlock.

   Usage:
     (defon (Button.Pressed \"#apply-btn\") apply_filter [self]
       (safe-dismiss self result))

   Equivalent to:
     (defn [(on Button.Pressed \"#apply-btn\")] apply_filter [self]
       (safe-dismiss self result))

   But compile-fails if body contains (.dismiss ...)."
  (import hy.models [Expression Symbol])
  (defn _is-dot-dismiss [form]
    "Check if form is (.dismiss ...) — Hy represents this as
     (Expression[(. None dismiss)] self args...)."
    (and (isinstance form Expression)
         (>= (len form) 1)
         (do (setv head (get form 0))
             (and (isinstance head Expression)
                  (= (len head) 3)
                  (isinstance (get head 0) Symbol)
                  (= (str (get head 0)) ".")
                  (isinstance (get head 2) Symbol)
                  (= (str (get head 2)) "dismiss")))))
  (setv stack (list body))
  (while stack
    (setv form (.pop stack))
    (when (isinstance form Expression)
      (when (_is-dot-dismiss form)
        (raise (SyntaxError
          f"defon {name}: raw .dismiss inside message handler — use (safe-dismiss self ...) instead")))
      (for [child form]
        (.append stack child))))
  `(defn [(on ~@event-spec)] ~name ~params
     ~@body))

;; ============================================================================
;; Dashboard Action Macros — Compile-time behavior injection
;; ============================================================================
;;
;; These macros eliminate copy-paste across dashboard classes by generating
;; methods at compile time. Unlike Python mixins (runtime MRO), these expand
;; to flat method definitions — what you see is what you get.

(defmacro defaction [name params guards #* body]
  "Define an action method with declarative guards.
   
   guards: list of guard keywords
     :guard-input     - skip if Input widget has focus
     :require-run     - require self.selected_run
     :require-issue   - require self.selected_issue
     :require-run-ref - require self._highlighted_run_ref (binds to run-ref)
   
   Usage:
     (defaction action_stop [self] [:guard-input :require-run-ref]
       (._do_stop self run-ref)
       (.notify self f\"Stopping {run-ref}\"))
     
     (defaction action_diff [self] [:guard-input :require-run]
       ;; can add extra guards after the standard ones
       (when (not self.selected_run.worktree_path)
         (.notify self \"Run has no worktree\" :severity \"warning\")
         (return))
       (._do_diff self self.selected_run))
  "
  (import hy.models [Keyword Symbol])
  (setv guard-forms [])
  (for [g guards]
    (setv gs (cond
               (isinstance g Keyword) (. g name)
               (isinstance g Symbol) (str g)
               True (str g)))
    (cond
      (= gs "guard-input")
      (.append guard-forms `(when (_input-has-focus self) (return)))
      
      (= gs "require-run")
      (.append guard-forms `(when (not self.selected_run)
                               (.notify self "No run selected" :severity "warning")
                               (return)))
      
      (= gs "require-issue")
      (.append guard-forms `(when (not self.selected_issue)
                               (.notify self "No issue selected" :severity "warning")
                               (return)))
      
      (= gs "require-run-ref")
      (.extend guard-forms [`(setv run-ref (getattr self "_highlighted_run_ref" None))
                             `(when (not run-ref)
                                (.notify self "No run selected" :severity "warning")
                                (return))])
      
      True
      (raise (SyntaxError f"defaction: unknown guard: {gs}"))))
  `(defn ~name ~params
     ~@guard-forms
     ~@body))

(defmacro with-run-actions []
  "Inject run action methods into a dashboard class.
   
   Generates: action_attach, _do_attach, _exit_and_attach,
              action_stop, _do_stop,
              action_diff, _do_diff, _exit_and_diff,
              action_kill_session, _do_kill_session
   
   Requires in file:
     - Imports: subprocess, detect_current_multiplexer, get_multiplexer,
       MultiplexerType, get_multiplexer_type_from_run, get_session_name,
       get_multiplexer_for_run, KillConfirmScreen, _build-orch-cmd, _input-has-focus
     - Macros: with-fallback, when-err
     - Instance attrs: self.selected_run, self._highlighted_run_ref,
       self.config, self.api
   
   Usage:
     (defclass RunsDashboard [App]
       (with-run-actions)   ;; injects ~10 methods
       ;; ... your unique methods ...)
  "
  `(do
     ;; ===================== ATTACH =====================
     (defn action_attach [self]
       (when (_input-has-focus self) (return))
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
     
     ;; ===================== STOP =====================
     (defn action_stop [self]
       (when (_input-has-focus self) (return))
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
     
     ;; ===================== DIFF =====================
     (defn action_diff [self]
       (when (_input-has-focus self) (return))
       (when (not self.selected_run)
         (.notify self "No run selected" :severity "warning")
         (return))
       (when (not self.selected_run.worktree_path)
         (.notify self "Run has no worktree" :severity "warning")
         (return))
       (._do_diff self self.selected_run))
     
     (defn [(work :thread True)] _do_diff [self run]
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
       (.call_from_thread self self._exit_and_diff diff-cmd))
     
     (defn _exit_and_diff [self diff-cmd]
       "Exit TUI and run diff command."
       (.exit self)
       (subprocess.run diff-cmd))
     
     ;; ===================== KILL SESSION =====================
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
         (.call_from_thread self self.refresh_data)))))

(defmacro with-issue-actions []
  "Inject issue action methods into a dashboard class.
   
   Generates: action_new_run, _on_agent_selected, _do_new_run,
              action_open_issue,
              action_close_issue, _do_close_issue
   
   Requires in file:
     - Imports: subprocess, _input-has-focus, _get-editor-command,
       _get-issue-file-path, _get-available-agents, detect_current_multiplexer,
       get_multiplexer, AgentSelectScreen, CloseIssueConfirmScreen, get-logger
     - Macros: if-ok
     - Instance attrs: self.selected_issue, self.config, self.api
   
   Usage:
     (defclass IssuesDashboard [App]
       (with-issue-actions)
       ;; ... your unique methods ...)
  "
  `(do
     ;; ===================== NEW RUN =====================
     (defn action_new_run [self]
       (when (_input-has-focus self) (return))
       (when (not self.selected_issue)
         (.notify self "No issue selected" :severity "warning")
         (return))
       (setv agents (_get-available-agents self.config))
       (.push_screen self
         (AgentSelectScreen self.selected_issue.id agents)
         self._on_agent_selected))
     
     (defn _on_agent_selected [self agent]
       (when (and agent self.selected_issue)
         (setv issue-id self.selected_issue.id)
         (.notify self f"Starting run for {issue-id} with {agent}...")
         (._do_new_run self issue-id agent)))
     
     (defn [(work :thread True :exclusive True)] _do_new_run [self issue-id agent]
       "Start a new run for an issue. Logs errors properly."
       (setv log (get-logger))
       (if-ok [_response (.start_run self.api issue-id agent)]
         (.call_from_thread self self.notify
           f"Run started for {issue-id}"
           :severity "information")
         (do
           (setv error-msg (str _response))
           (when (> (len error-msg) 200)
             (setv error-msg (+ (cut error-msg 0 200) "...")))
           (.error log f"Failed to start run for {issue-id}: {error-msg}")
           (.call_from_thread self self.notify
             f"Failed to start run: {error-msg}"
             :severity "error")))
       (.call_from_thread self self.refresh_data))
     
     ;; ===================== OPEN ISSUE =====================
     (defn action_open_issue [self]
       (when (_input-has-focus self) (return))
       (when (not self.selected_issue)
         (.notify self "No issue selected" :severity "warning")
         (return))
       (setv #(file-path error) (_get-issue-file-path self.selected_issue))
       (when (or error (is file-path None))
         (.notify self (or error "Unknown error") :severity "error")
         (return))
       (setv #(cmd error) (_get-editor-command file-path))
       (when (or error (is cmd None))
         (.notify self (or error "Unknown error") :severity "error")
         (return))
       (setv current-mux-type (detect_current_multiplexer))
       (when current-mux-type
         (setv current-mux (get_multiplexer current-mux-type))
         (setv tab-name f"edit-{self.selected_issue.id}")
         (when (.new_tab_with_command current-mux tab-name cmd)
           (.notify self f"Opened tab: {tab-name}")
           (return))
         (.notify self "Failed to create tab, falling back to suspend" :severity "warning"))
       (with [(.suspend self)]
         (subprocess.run cmd))
       (.refresh_data self))
     
     ;; ===================== CLOSE ISSUE =====================
     (defn action_close_issue [self]
       (when (_input-has-focus self) (return))
       (when (not self.selected_issue)
         (.notify self "No issue selected" :severity "warning")
         (return))
       (setv issue-id self.selected_issue.id)
       (setv issue-title self.selected_issue.title)
       (defn on-confirm [confirmed]
         (when confirmed
           (._do_close_issue self issue-id)))
       (.push_screen self
         (CloseIssueConfirmScreen issue-id issue-title)
         on-confirm))
     
     (defn [(work :thread True :exclusive True)] _do_close_issue [self issue-id]
       "Close an issue. Logs errors properly."
       (setv log (get-logger))
       (if-ok [_response (.close_issue self.api issue-id)]
         (.call_from_thread self self.notify
           f"Closed issue {issue-id}"
           :severity "information")
         (do
           (setv error-msg (str _response))
           (when (> (len error-msg) 200)
             (setv error-msg (+ (cut error-msg 0 200) "...")))
           (.error log f"Failed to close issue {issue-id}: {error-msg}")
           (.call_from_thread self self.notify
             f"Failed to close issue: {error-msg}"
             :severity "error")))
       (.call_from_thread self self.refresh_data))))

(defmacro defrpc [name params operation #* body]
  "Define a daemon RPC method returning Result[T, ProtoDaemonError].
   Automatically adds self as first parameter and wraps body in daemon-result.
   
   Usage:
     (defrpc stop-run [issue-id [run-id \"\"]] \"stop_run\"
       (setv req (pb.Request))
       (set-> req.stop_run.issues_root (._issues-root-str self)
              req.stop_run.issue_id issue-id
              req.stop_run.run_id run-id)
       (._send-ok self req)
       {\"stopped\" True})
  "
  `(defn ~name [self ~@params]
     (daemon-result ~operation ~@body)))
