//go:build semgrepfixture

// Semgrep test fixture for run-status-write-surface rules.
// Parsed by `semgrep test`, never compiled.
package fixture

func badConstructStatusEvent() {
	// ruleid: run-status-write-surface
	_ = model.NewStatusEvent(model.StatusRunning)
}

func badConstructViaNewEvent() {
	// ruleid: run-status-write-surface
	_ = model.NewEvent(model.EventTypeStatus, "done", nil)
}

func badConstructViaNewEventStringLiteral() {
	// ruleid: run-status-write-surface
	_ = model.NewEvent("status", "done", nil)
}

func badClientStatusLiteral() {
	ev := &orchapi.Event{
		// ruleid: run-status-write-surface
		Type: "status",
		Name: "done",
	}
	_ = ev
}

func badIgnoredAppendResult(api API, ctx Ctx, ref Ref, ev *Event) {
	// ruleid: no-ignored-status-append
	_, _ = api.AppendEvent(ctx, ref, ev)
}

func okReadStatusEventType(e Event) bool {
	// ok: run-status-write-surface
	return e.Type == "status"
}

func okArtifactAppend(st Store, ref Ref) error {
	// ok: run-status-write-surface
	return st.AppendEvent(ref, model.NewArtifactEvent("pr", nil))
}

func okGuardedAppendErrorHandled(api API, ctx Ctx, ref Ref, ev *Event) error {
	// ok: no-ignored-status-append
	_, err := api.AppendEvent(ctx, ref, ev)
	return err
}
