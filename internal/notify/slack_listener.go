package notify

import (
	"log"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/runevents"
)

// SlackStatusListener adapts a SlackNotifier to runevents.StatusChangeListener.
// It encapsulates the previously-inline Slack-specific logic that used to
// live in the daemon's monitor.notifyStatusChange — issue title resolution,
// blocked-vs-status-change message routing, and error logging — so the
// daemon body no longer depends on the notify package's specifics.
//
// The listener short-circuits when the underlying notifier is unconfigured
// or when the configured status filter excludes the new state.
type SlackStatusListener struct {
	Notifier *SlackNotifier
	Logger   *log.Logger
}

// NewSlackStatusListener returns a listener bound to the given notifier.
// notifier and logger may be nil; nil notifier disables the listener (useful
// for callers that always register but only sometimes have Slack configured).
func NewSlackStatusListener(notifier *SlackNotifier, logger *log.Logger) *SlackStatusListener {
	return &SlackStatusListener{Notifier: notifier, Logger: logger}
}

// OnStatusChange implements runevents.StatusChangeListener.
func (l *SlackStatusListener) OnStatusChange(ev *runevents.StatusChangeEvent) {
	if l == nil || l.Notifier == nil || ev == nil || ev.Run == nil {
		return
	}
	if !l.Notifier.config.IsConfigured() {
		return
	}
	if !l.Notifier.config.ShouldNotify(string(ev.To)) {
		return
	}

	issueTitle := ev.Run.IssueID
	if ev.Store != nil {
		if issue, err := ev.Store.ResolveIssue(ev.Run.IssueID); err == nil && issue != nil && issue.Title != "" {
			issueTitle = issue.Title
		}
	}

	var err error
	if ev.To == model.StatusWaiting || ev.To == model.StatusRateLimited {
		err = l.Notifier.NotifyBlocked(ev.Run, issueTitle, ev.LastOutput)
	} else {
		err = l.Notifier.NotifyStatusChange(ev.Run, issueTitle, ev.To)
	}

	if l.Logger == nil {
		return
	}
	if err != nil {
		l.Logger.Printf("%s#%s: failed to send slack notification: %v", ev.Run.IssueID, ev.Run.RunID, err)
	} else {
		l.Logger.Printf("%s#%s: sent slack notification for status %s", ev.Run.IssueID, ev.Run.RunID, ev.To)
	}
}
