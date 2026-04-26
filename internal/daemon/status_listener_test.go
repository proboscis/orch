package daemon

import (
	"io"
	"log"
	"sync/atomic"
	"testing"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/runevents"
)

func TestAddStatusChangeListener_FanoutPreservesOrder(t *testing.T) {
	d := newTestDaemon()

	var order []string
	d.AddStatusChangeListener(runevents.StatusChangeListenerFunc(func(_ *runevents.StatusChangeEvent) {
		order = append(order, "first")
	}))
	d.AddStatusChangeListener(runevents.StatusChangeListenerFunc(func(_ *runevents.StatusChangeEvent) {
		order = append(order, "second")
	}))

	d.fireStatusChange(&runevents.StatusChangeEvent{
		Run: &model.Run{IssueID: "i", RunID: "r"},
		To:  model.StatusDone,
	})

	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("listeners fired in wrong order or count: %v", order)
	}
}

func TestFireStatusChange_PanicInListenerDoesNotStopOthers(t *testing.T) {
	d := newTestDaemon()
	d.logger = log.New(io.Discard, "", 0)

	var ran int32
	d.AddStatusChangeListener(runevents.StatusChangeListenerFunc(func(_ *runevents.StatusChangeEvent) {
		panic("boom")
	}))
	d.AddStatusChangeListener(runevents.StatusChangeListenerFunc(func(_ *runevents.StatusChangeEvent) {
		atomic.StoreInt32(&ran, 1)
	}))

	d.fireStatusChange(&runevents.StatusChangeEvent{
		Run: &model.Run{IssueID: "i", RunID: "r"},
		To:  model.StatusDone,
	})

	if atomic.LoadInt32(&ran) != 1 {
		t.Fatal("second listener was not invoked after first panicked")
	}
}

func TestFireStatusChange_NilEventIsNoop(t *testing.T) {
	d := newTestDaemon()

	called := false
	d.AddStatusChangeListener(runevents.StatusChangeListenerFunc(func(_ *runevents.StatusChangeEvent) {
		called = true
	}))

	d.fireStatusChange(nil)

	if called {
		t.Fatal("listener was invoked for nil event")
	}
}
