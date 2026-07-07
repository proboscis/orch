package daemon

import (
	"sync"
	"testing"
	"time"

	"github.com/proboscis/orch/api/orchpb"
)

func TestRunEventBus_DeliversToMatchingSubscribers(t *testing.T) {
	bus := NewRunEventBus()
	subA := bus.Subscribe(RunEventFilter{})
	defer subA.Close()
	subB := bus.Subscribe(RunEventFilter{IssueID: "issue-x"})
	defer subB.Close()
	subC := bus.Subscribe(RunEventFilter{RunID: "run-y"})
	defer subC.Close()

	bus.Publish(&orchpb.RunEventFrame{IssueId: "issue-x", RunId: "run-1"})
	bus.Publish(&orchpb.RunEventFrame{IssueId: "issue-z", RunId: "run-y"})
	bus.Publish(&orchpb.RunEventFrame{IssueId: "issue-z", RunId: "run-2"})

	gotA := drain(subA.Events(), 3, 100*time.Millisecond)
	if len(gotA) != 3 {
		t.Fatalf("subA expected 3 events, got %d", len(gotA))
	}

	gotB := drain(subB.Events(), 1, 100*time.Millisecond)
	if len(gotB) != 1 || gotB[0].IssueId != "issue-x" {
		t.Fatalf("subB expected 1 event for issue-x, got %v", gotB)
	}

	gotC := drain(subC.Events(), 1, 100*time.Millisecond)
	if len(gotC) != 1 || gotC[0].RunId != "run-y" {
		t.Fatalf("subC expected 1 event for run-y, got %v", gotC)
	}
}

func TestRunEventBus_DropsWhenSubscriberFull(t *testing.T) {
	bus := NewRunEventBus()
	sub := bus.Subscribe(RunEventFilter{})
	defer sub.Close()

	// Publish more events than buffer capacity; Publish must never block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < runEventSubscriberBufferSize*4; i++ {
			bus.Publish(&orchpb.RunEventFrame{RunId: "spam"})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked when subscriber buffer was full")
	}

	// Drain whatever fit; expect at most buffer capacity, but at least 1.
	got := drain(sub.Events(), runEventSubscriberBufferSize, 100*time.Millisecond)
	if len(got) == 0 {
		t.Fatal("expected at least one event delivered")
	}
	if len(got) > runEventSubscriberBufferSize {
		t.Fatalf("delivered %d events, exceeds buffer cap %d", len(got), runEventSubscriberBufferSize)
	}
}

func TestRunEventBus_CloseUnsubscribes(t *testing.T) {
	bus := NewRunEventBus()
	sub := bus.Subscribe(RunEventFilter{})
	if got := bus.SubscriberCount(); got != 1 {
		t.Fatalf("expected 1 subscriber after Subscribe, got %d", got)
	}
	sub.Close()
	if got := bus.SubscriberCount(); got != 0 {
		t.Fatalf("expected 0 subscribers after Close, got %d", got)
	}
}

func TestRunEventBus_ConcurrentPublishAndSubscribe(t *testing.T) {
	bus := NewRunEventBus()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub := bus.Subscribe(RunEventFilter{})
			drain(sub.Events(), 4, 50*time.Millisecond)
			sub.Close()
		}()
	}
	for i := 0; i < 256; i++ {
		bus.Publish(&orchpb.RunEventFrame{RunId: "x"})
	}
	wg.Wait()

	if got := bus.SubscriberCount(); got != 0 {
		t.Fatalf("expected all subscribers cleaned up, got %d", got)
	}
}

func drain(ch <-chan *orchpb.RunEventFrame, max int, deadline time.Duration) []*orchpb.RunEventFrame {
	out := make([]*orchpb.RunEventFrame, 0, max)
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for len(out) < max {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-timer.C:
			return out
		}
	}
	return out
}
