package daemon

import (
	"reflect"
	"testing"
)

// Golden field allowlist for the WL#2 store-of-record vs derived-state core
// (docs/design/coupling-core-roadmap.md watchlist row 2, run-state-machine.md
// §7 D-C1). Every monitor-state field must belong to one of the legal
// classes; a persisted aggregate or snapshot mirror is not a class:
//
//   - "fold-derived": re-derived from the event log by initialRunCore at
//     monitor registration. Adding a field here requires extending
//     initialRunCore in the same change.
//   - "ephemeral": deliberately reset on daemon restart, re-converges within
//     bounded ticks (L7); reset direction must be delay-only/softening (L7').
//   - "scheduling": shell-owned "when to observe" state on RunState itself —
//     mechanism, never read by the transition policy.
//
// If this test fails because you added a field, classify it above, add it to
// the map, and make the classification true (initialRunCore for fold-derived,
// documented reset direction for ephemeral). If the field's value must
// survive restarts and cannot be derived from existing events, that is a
// change to the store of record: route to frontier + human review and revise
// run-state-machine.md in the same change set (AGENTS.md routing rule). The
// syntactic complement of this test is .semgrep/derived-state-guard/.

var runCoreFieldClasses = map[string]string{
	"LastOutput":        "ephemeral",
	"LastOutputAt":      "ephemeral",
	"OutputHash":        "ephemeral",
	"ReadingKind":       "ephemeral",
	"ReadingStreak":     "ephemeral",
	"WasAlive":          "fold-derived",
	"PRRecorded":        "fold-derived",
	"DeadCheckCount":    "ephemeral",
	"AliveObserver":     "ephemeral",
	"DeadCheckObserver": "ephemeral",
	"PRMismatchURL":     "ephemeral",
	"PRMismatchHead":    "ephemeral",
}

var runStateSchedulingFields = map[string]bool{
	"runCore":               true, // the embedded semantic core, classified above
	"LastCheckAt":           true,
	"CaptureEndpoint":       true,
	"CaptureFailureCount":   true,
	"CaptureRetryAt":        true,
	"CaptureErrorKey":       true,
	"CaptureErrorLogAt":     true,
	"SuppressedCaptureLogs": true,
	"RemoteCaptureAt":       true,
}

func TestRunCoreFieldsAreClassified(t *testing.T) {
	tp := reflect.TypeOf(runCore{})
	seen := make(map[string]bool, tp.NumField())
	for i := 0; i < tp.NumField(); i++ {
		f := tp.Field(i)
		seen[f.Name] = true
		class, ok := runCoreFieldClasses[f.Name]
		if !ok {
			t.Errorf("runCore.%s is not classified: every runCore field must be fold-derived or ephemeral (run-state-machine.md §7 D-C1); add it to runCoreFieldClasses and make the classification true", f.Name)
			continue
		}
		if class != "fold-derived" && class != "ephemeral" {
			t.Errorf("runCore.%s has illegal class %q: the only legal classes are fold-derived and ephemeral — a persisted mirror is a second store of record", f.Name, class)
		}
		if f.Tag != "" {
			t.Errorf("runCore.%s carries struct tag %q: monitor state must never be serialization-ready — the event log is the only persistent representation (D-C1 rejected snapshots)", f.Name, f.Tag)
		}
	}
	for name := range runCoreFieldClasses {
		if !seen[name] {
			t.Errorf("runCoreFieldClasses lists %q but runCore has no such field: remove the stale entry", name)
		}
	}
}

func TestRunStateFieldsAreSchedulingOnly(t *testing.T) {
	tp := reflect.TypeOf(RunState{})
	seen := make(map[string]bool, tp.NumField())
	for i := 0; i < tp.NumField(); i++ {
		f := tp.Field(i)
		seen[f.Name] = true
		if !runStateSchedulingFields[f.Name] {
			t.Errorf("RunState.%s is not in the scheduling allowlist: RunState carries only shell-owned scheduling state (when to observe); semantic state belongs in runCore behind stepRun, and nothing here may mirror or persist live state", f.Name)
		}
		if f.Tag != "" {
			t.Errorf("RunState.%s carries struct tag %q: monitor state must never be serialization-ready — the event log is the only persistent representation (D-C1 rejected snapshots)", f.Name, f.Tag)
		}
	}
	for name := range runStateSchedulingFields {
		if !seen[name] {
			t.Errorf("runStateSchedulingFields lists %q but RunState has no such field: remove the stale entry", name)
		}
	}
}
