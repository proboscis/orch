;; Example module demonstrating error handling macros
;; This shows how to use the macros defined in macros.hy

(require orch_monitor.macros *)
(import pathlib [Path])

;; ============================================================================
;; Example 1: ui-defensive - UI updates that can fail silently
;; ============================================================================

(defn update-elapsed-times [run-table runs]
  "Update elapsed times in table - cosmetic, OK to fail silently."
  (for [run runs]
    (when (.is_active run)
      (ui-defensive
        (.update_cell run-table (.ref run) "elapsed" (.elapsed_time run))))))

;; ============================================================================
;; Example 2: try-or - Simple fallbacks
;; ============================================================================

(defn get-config-value [config key default]
  "Get config value with fallback."
  (try-or default
    (get config key)))

;; ============================================================================
;; Example 3: strict-block - Compile-time enforcement
;; ============================================================================

(defn critical-operation [data]
  "This function uses strict-block to forbid silent catches.
  
  Uncommenting the silent catch below would cause a COMPILE ERROR.
  "
  (strict-block
    (setv validated (validate-data data))
    (process validated)
    validated))

(defn validate-data [data]
  (when (not data)
    (raise (ValueError "Data cannot be empty")))
  data)

(defn process [data]
  (print f"Processing: {data}"))

;; ============================================================================
;; Example 4: error-context - Metadata for debugging
;; ============================================================================

(defn fetch-with-metadata [url]
  "Fetch with error context metadata."
  (error-context "http_fetch"
    {"retry" True "max_retries" 3 "timeout" 5000 "critical" False}
    (print f"Fetching {url} with context: {__error_context__}")
    (print f"Metadata: {__error_metadata__}")
    url))

;; ============================================================================
;; Demo runner
;; ============================================================================

(defn run-demo []
  "Run all demos."
  (print "\n=== ui-defensive demo ===")
  (print "Result of division by zero (should be None):")
  (print (ui-defensive (/ 1 0)))
  
  (print "\n=== try-or demo ===")
  (print "Getting missing key with default:")
  (print (get-config-value {"a" 1} "missing" "default-value"))
  
  (print "\n=== strict-block demo ===")
  (print "Running critical operation:")
  (setv result (try-or "fallback-on-error"
                (critical-operation {"key" "value"})))
  (print f"Result: {result}")
  
  (print "\n=== error-context demo ===")
  (fetch-with-metadata "https://example.com")
  
  (print "\n=== All demos complete! ==="))

;; Run if executed directly
(when (= __name__ "__main__")
  (run-demo))
