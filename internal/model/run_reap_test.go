package model

import "testing"

// ReapedSessionGeneration folds the highest session_reaped daemon_notice
// generation from the event log (ADR-0005 R3); malformed or foreign notes
// contribute nothing.
func TestReapedSessionGeneration(t *testing.T) {
	reapNote := func(gen string) *Event {
		return NewDaemonNoticeEvent("session_reaped", map[string]string{"generation": gen})
	}

	cases := []struct {
		name   string
		events []*Event
		want   int
	}{
		{"no events", nil, 0},
		{"unrelated note", []*Event{NewDaemonNoticeEvent("gate_ack", map[string]string{"gate": "trust"})}, 0},
		{"single reap", []*Event{reapNote("1")}, 1},
		{"reap chain keeps max", []*Event{reapNote("1"), reapNote("2")}, 2},
		{"malformed generation ignored", []*Event{reapNote("banana"), reapNote("1")}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Run{}
			r.Events = append(r.Events, c.events...)
			if got := r.ReapedSessionGeneration(); got != c.want {
				t.Fatalf("ReapedSessionGeneration() = %d, want %d", got, c.want)
			}
		})
	}
}

// SessionReaped is the LS3 latch over the two folds.
func TestSessionReaped(t *testing.T) {
	r := &Run{}
	if r.SessionReaped() {
		t.Fatal("fresh run must not read as reaped")
	}
	r.Events = append(r.Events, NewDaemonNoticeEvent("session_reaped", map[string]string{"generation": "1"}))
	r.AgentSessionGeneration = 1
	if !r.SessionReaped() {
		t.Fatal("current generation reaped must latch")
	}
	r.AgentSessionGeneration = 2 // revive recorded a new generation
	if r.SessionReaped() {
		t.Fatal("revive must dissolve the latch")
	}
}
