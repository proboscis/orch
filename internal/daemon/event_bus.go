package daemon

import (
	"sync"

	"github.com/s22625/orch/api/orchpb"
)

// runEventSubscriberBufferSize bounds each subscriber's channel.
// Publishing is non-blocking; if a subscriber falls behind, events are
// dropped for that subscriber (slow consumers do not stall the daemon).
const runEventSubscriberBufferSize = 64

// RunEventFilter narrows which RunEventFrames a subscriber receives.
// Empty fields mean "match anything".
type RunEventFilter struct {
	IssueID string
	RunID   string
}

func (f RunEventFilter) matches(ev *orchpb.RunEventFrame) bool {
	if ev == nil {
		return false
	}
	if f.IssueID != "" && f.IssueID != ev.IssueId {
		return false
	}
	if f.RunID != "" && f.RunID != ev.RunId {
		return false
	}
	return true
}

// runEventSubscription is the handle returned by RunEventBus.Subscribe.
// Consumers read frames from Events and call Close when done.
type runEventSubscription struct {
	id     uint64
	filter RunEventFilter
	events chan *orchpb.RunEventFrame
	bus    *RunEventBus
}

func (s *runEventSubscription) Events() <-chan *orchpb.RunEventFrame {
	return s.events
}

func (s *runEventSubscription) Close() {
	s.bus.unsubscribe(s.id)
}

// RunEventBus is an in-process pub/sub for run state transitions.
// It is safe for concurrent use; publishing is non-blocking.
type RunEventBus struct {
	mu     sync.RWMutex
	nextID uint64
	subs   map[uint64]*runEventSubscription
}

func NewRunEventBus() *RunEventBus {
	return &RunEventBus{subs: make(map[uint64]*runEventSubscription)}
}

func (b *RunEventBus) Subscribe(filter RunEventFilter) *runEventSubscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	sub := &runEventSubscription{
		id:     b.nextID,
		filter: filter,
		events: make(chan *orchpb.RunEventFrame, runEventSubscriberBufferSize),
		bus:    b,
	}
	b.subs[sub.id] = sub
	return sub
}

func (b *RunEventBus) unsubscribe(id uint64) {
	b.mu.Lock()
	sub, ok := b.subs[id]
	if ok {
		delete(b.subs, id)
	}
	b.mu.Unlock()
	if ok {
		close(sub.events)
	}
}

// Publish sends ev to every matching subscriber. Subscribers whose buffer is
// full silently drop this event. Publish never blocks.
func (b *RunEventBus) Publish(ev *orchpb.RunEventFrame) {
	if ev == nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subs {
		if !sub.filter.matches(ev) {
			continue
		}
		select {
		case sub.events <- ev:
		default:
		}
	}
}

// SubscriberCount returns the number of active subscribers (test helper).
func (b *RunEventBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
