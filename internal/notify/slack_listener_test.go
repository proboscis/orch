package notify

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/config"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/runevents"
)

// TestSlackStatusListener_RoutesBlockedAndStatusChange asserts that the
// listener calls NotifyBlocked for waiting/rate_limited and NotifyStatusChange
// otherwise — the same routing the daemon used to do inline.
func TestSlackStatusListener_RoutesBlockedAndStatusChange(t *testing.T) {
	var bodies []SlackMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg SlackMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, msg)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		Enabled:    true,
		WebhookURL: server.URL,
		NotifyOn:   []string{"waiting", "rate_limited", "done"},
	}
	listener := NewSlackStatusListener(NewSlackNotifier(cfg), log.New(io.Discard, "", 0))

	run := &model.Run{IssueID: "i", RunID: "r"}

	listener.OnStatusChange(&runevents.StatusChangeEvent{
		Run: run, From: model.StatusRunning, To: model.StatusWaiting, LastOutput: "scrollback",
	})
	listener.OnStatusChange(&runevents.StatusChangeEvent{
		Run: run, From: model.StatusRunning, To: model.StatusRateLimited,
	})
	listener.OnStatusChange(&runevents.StatusChangeEvent{
		Run: run, From: model.StatusRunning, To: model.StatusDone,
	})

	if len(bodies) != 3 {
		t.Fatalf("expected 3 webhook calls, got %d", len(bodies))
	}

	// First two are NotifyBlocked (carries last_output / "blocked" framing in the text).
	for i := 0; i < 2; i++ {
		if !strings.Contains(strings.ToLower(bodies[i].Text), "blocked") &&
			!strings.Contains(strings.ToLower(bodies[i].Text), "waiting") &&
			!strings.Contains(strings.ToLower(bodies[i].Text), "rate") {
			t.Errorf("event %d: expected blocked/waiting/rate framing, got text=%q", i, bodies[i].Text)
		}
	}
	// Third is the generic status-change message.
	if !strings.Contains(strings.ToLower(bodies[2].Text), "done") {
		t.Errorf("event 2: expected status-change framing for done, got text=%q", bodies[2].Text)
	}
}

func TestSlackStatusListener_NoOpsWhenUnconfigured(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{Enabled: false} // no webhook, not configured
	listener := NewSlackStatusListener(NewSlackNotifier(cfg), nil)

	listener.OnStatusChange(&runevents.StatusChangeEvent{
		Run: &model.Run{IssueID: "i", RunID: "r"}, To: model.StatusWaiting,
	})

	if calls != 0 {
		t.Fatalf("expected zero webhook calls when unconfigured, got %d", calls)
	}
}

func TestSlackStatusListener_RespectsShouldNotifyFilter(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.SlackConfig{
		Enabled:    true,
		WebhookURL: server.URL,
		// NotifyOn explicitly excludes "running" so the listener should skip it.
		NotifyOn: []string{"waiting", "done"},
	}
	listener := NewSlackStatusListener(NewSlackNotifier(cfg), nil)

	listener.OnStatusChange(&runevents.StatusChangeEvent{
		Run: &model.Run{IssueID: "i", RunID: "r"}, To: model.StatusRunning,
	})

	if calls != 0 {
		t.Fatalf("expected listener to skip filtered status, got %d webhook calls", calls)
	}
}

func TestSlackStatusListener_NilNotifierIsNoop(t *testing.T) {
	listener := NewSlackStatusListener(nil, nil)
	// Should not panic.
	listener.OnStatusChange(&runevents.StatusChangeEvent{
		Run: &model.Run{IssueID: "i", RunID: "r"}, To: model.StatusWaiting,
	})
}
