package daemon

import (
	"github.com/proboscis/orch/internal/runevents"
)

// AddStatusChangeListener registers a listener that will be invoked for every
// run status transition observed by monitorRun. Listeners are invoked in
// registration order on the daemon's monitor goroutine.
func (d *Daemon) AddStatusChangeListener(l runevents.StatusChangeListener) {
	if d == nil || l == nil {
		return
	}
	d.statusListenersMu.Lock()
	defer d.statusListenersMu.Unlock()
	d.statusListeners = append(d.statusListeners, l)
}

// fireStatusChange dispatches ev to every registered listener. A panic in a
// listener is logged and re-raised so listener failures are not hidden.
func (d *Daemon) fireStatusChange(ev *runevents.StatusChangeEvent) {
	if d == nil || ev == nil {
		return
	}
	d.statusListenersMu.RLock()
	listeners := make([]runevents.StatusChangeListener, len(d.statusListeners))
	copy(listeners, d.statusListeners)
	d.statusListenersMu.RUnlock()

	for _, l := range listeners {
		d.invokeListener(l, ev)
	}
}

func (d *Daemon) invokeListener(l runevents.StatusChangeListener, ev *runevents.StatusChangeEvent) {
	defer func() {
		if r := recover(); r != nil {
			logAndRepanic(d.logger, "status change listener", r)
		}
	}()
	l.OnStatusChange(ev)
}
