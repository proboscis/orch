// Package runevents defines the in-process lifecycle event types and
// listener interface used by the daemon to fan out run status changes to
// extensions like the Slack notifier.
//
// This package is intentionally small (model + store imports only) so both
// the daemon and notification packages can depend on it without creating an
// import cycle.
package runevents

import (
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/store"
)

// StatusChangeEvent carries the rich, in-process context for a run status
// transition. Unlike orchpb.RunEventFrame (the wire-level public event), this
// type also exposes the captured agent output and the store reference so
// listeners (e.g. the Slack notifier) can resolve issue metadata or attach
// terminal scrollback to outgoing notifications.
//
// StatusChangeEvent is in-process only; it must not be serialized to the
// proto stream and must not leak large payloads to external observers.
type StatusChangeEvent struct {
	Run        *model.Run
	From       model.Status
	To         model.Status
	Reason     string // machine-readable verdict reason (model.AttrStatusReason); may be empty
	Source     model.EventSource
	LastOutput string
	Store      store.Store
}

// StatusChangeListener is invoked once per status transition observed by the
// daemon. Listeners run synchronously on the daemon's monitor goroutine;
// long-running work should be offloaded by the listener itself.
type StatusChangeListener interface {
	OnStatusChange(ev *StatusChangeEvent)
}

// StatusChangeListenerFunc adapts a plain function to the interface so
// callers can register inline closures.
type StatusChangeListenerFunc func(ev *StatusChangeEvent)

// OnStatusChange implements StatusChangeListener.
func (f StatusChangeListenerFunc) OnStatusChange(ev *StatusChangeEvent) { f(ev) }
