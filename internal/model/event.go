package model

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// EventType represents the type of event
type EventType string

const (
	EventTypeStatus   EventType = "status"
	EventTypePhase    EventType = "phase"
	EventTypeArtifact EventType = "artifact"
	EventTypeTest     EventType = "test"
	EventTypeNote     EventType = "note"
)

// Status represents run operational lifecycle states
type Status string

const (
	StatusQueued      Status = "queued"
	StatusBooting     Status = "booting"
	StatusRunning     Status = "running"
	StatusWaiting     Status = "waiting"
	StatusRateLimited Status = "rate_limited"
	StatusPROpen      Status = "pr_open"
	StatusDone        Status = "done"
	StatusFailed      Status = "failed"
	StatusCanceled    Status = "canceled"
	StatusUnknown     Status = "unknown" // Agent exited unexpectedly, shell prompt showing
)

func NormalizeStatus(s string) (Status, error) {
	switch strings.TrimSpace(s) {
	case "blocked":
		return StatusWaiting, nil
	case "blocked_api":
		return StatusRateLimited, nil
	case string(StatusQueued):
		return StatusQueued, nil
	case string(StatusBooting):
		return StatusBooting, nil
	case string(StatusRunning):
		return StatusRunning, nil
	case string(StatusWaiting):
		return StatusWaiting, nil
	case string(StatusRateLimited):
		return StatusRateLimited, nil
	case string(StatusPROpen):
		return StatusPROpen, nil
	case string(StatusDone):
		return StatusDone, nil
	case string(StatusFailed):
		return StatusFailed, nil
	case string(StatusCanceled):
		return StatusCanceled, nil
	case string(StatusUnknown):
		return StatusUnknown, nil
	default:
		return "", fmt.Errorf("unknown run status: %q", s)
	}
}

func (s Status) IsTerminal() bool {
	return s == StatusDone || s == StatusCanceled || s == StatusFailed
}

func (s Status) IsActive() bool {
	switch s {
	case StatusQueued, StatusBooting, StatusRunning, StatusWaiting, StatusRateLimited, StatusPROpen:
		return true
	default:
		return false
	}
}

// EventSource distinguishes user commands from daemon observations
type EventSource string

const (
	EventSourceUser   EventSource = "user"   // CLI commands (stop, restart-from, resolve)
	EventSourceDaemon EventSource = "daemon" // Daemon inferences (PR merged, agent dead)
	EventSourceAgent  EventSource = "agent"  // Agent self-reported status
)

func CanTransitionStatus(from, to Status, source EventSource) bool {
	if from.IsTerminal() && source != EventSourceUser {
		return false
	}
	return true
}

// IssueStatus represents issue resolution states
type IssueStatus string

const (
	IssueStatusOpen     IssueStatus = "open"     // Issue is active, work in progress
	IssueStatusResolved IssueStatus = "resolved" // Issue specification has been resolved
	IssueStatusClosed   IssueStatus = "closed"   // Issue is closed/archived
)

// ParseIssueStatus converts a string to IssueStatus.
func ParseIssueStatus(s string) (IssueStatus, error) {
	switch strings.TrimSpace(s) {
	case "":
		return IssueStatusOpen, nil
	case string(IssueStatusOpen):
		return IssueStatusOpen, nil
	case "in_progress", "blocked":
		return IssueStatusOpen, nil
	case string(IssueStatusResolved):
		return IssueStatusResolved, nil
	case "completed":
		return IssueStatusResolved, nil
	case string(IssueStatusClosed):
		return IssueStatusClosed, nil
	case "canceled":
		return IssueStatusClosed, nil
	default:
		return "", fmt.Errorf("unknown issue status: %q", s)
	}
}

// IsValidIssueStatus checks if a string is a valid IssueStatus
func IsValidIssueStatus(s string) bool {
	_, err := ParseIssueStatus(s)
	return err == nil
}

// Phase values
type Phase string

const (
	PhasePlan      Phase = "plan"
	PhaseImplement Phase = "implement"
	PhaseTest      Phase = "test"
	PhasePR        Phase = "pr"
	PhaseReview    Phase = "review"
)

// Event represents a single event in a run
type Event struct {
	Timestamp time.Time
	Type      EventType
	Name      string
	Attrs     map[string]string
	Raw       string // Original line for preservation
}

// Format: - <ts> | <type> | <name> | key=value | key=value …
var eventLineRegex = regexp.MustCompile(`^-\s+(\S+)\s+\|\s+(\w+)\s+\|\s+(\S+)(.*)$`)
var attrRegex = regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([\S]+))`)

// ParseEvent parses an event line from markdown
func ParseEvent(line string) (*Event, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- ") {
		return nil, fmt.Errorf("event line must start with '- ': %s", line)
	}

	matches := eventLineRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil, fmt.Errorf("invalid event format: %s", line)
	}

	ts, err := time.Parse(time.RFC3339, matches[1])
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp %s: %w", matches[1], err)
	}

	event := &Event{
		Timestamp: ts,
		Type:      EventType(matches[2]),
		Name:      matches[3],
		Attrs:     make(map[string]string),
		Raw:       line,
	}

	// Parse attributes from the rest of the line
	if len(matches) > 4 {
		attrMatches := attrRegex.FindAllStringSubmatch(matches[4], -1)
		for _, m := range attrMatches {
			key := m[1]
			value := m[2] // quoted value
			if value == "" {
				value = m[3] // unquoted value
			}
			event.Attrs[key] = value
		}
	}

	return event, nil
}

// String formats the event as a markdown line
func (e *Event) String() string {
	var sb strings.Builder
	sb.WriteString("- ")
	sb.WriteString(e.Timestamp.Format(time.RFC3339))
	sb.WriteString(" | ")
	sb.WriteString(string(e.Type))
	sb.WriteString(" | ")
	sb.WriteString(e.Name)

	// Sort keys for consistent output
	keys := make([]string, 0, len(e.Attrs))
	for k := range e.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := e.Attrs[k]
		sb.WriteString(" | ")
		sb.WriteString(k)
		sb.WriteString("=")
		if strings.ContainsAny(v, " \t|=") {
			sb.WriteString("\"")
			sb.WriteString(v)
			sb.WriteString("\"")
		} else {
			sb.WriteString(v)
		}
	}

	return sb.String()
}

// NewEvent creates a new event with current timestamp
func NewEvent(eventType EventType, name string, attrs map[string]string) *Event {
	if attrs == nil {
		attrs = make(map[string]string)
	}
	return &Event{
		Timestamp: time.Now(),
		Type:      eventType,
		Name:      name,
		Attrs:     attrs,
	}
}

// NewStatusEvent creates a status change event
func NewStatusEvent(status Status) *Event {
	return NewEvent(EventTypeStatus, string(status), nil)
}

// AttrStatusReason is the event attribute carrying the machine-readable
// reason for a status verdict. The status vocabulary itself is a closed set;
// discrimination of *why* a verdict was reached travels as payload, the way
// Kubernetes keeps `phase` tiny and puts `reason` beside it.
const AttrStatusReason = "reason"

// Reasons attached to StatusUnknown verdicts (docs/design/run-state-machine.md §5).
const (
	// StatusReasonNeverAlive: the agent was never observed alive and the boot
	// grace expired — an infrastructure problem (binary/auth/mux env); retry
	// is futile until the host is fixed.
	StatusReasonNeverAlive = "never_alive"
	// StatusReasonSessionLost: the agent was alive but the backend lost
	// observability of its session.
	StatusReasonSessionLost = "session_lost"
	// StatusReasonAgentExited: the agent process exited without a verdict,
	// shell prompt showing — check the transcript/worktree; retry plausible.
	StatusReasonAgentExited = "agent_exited"
	// StatusReasonObserverUnverified: the dead-check threshold was reached
	// through an observation channel that never saw this run alive (L11c) —
	// check the worker/daemon mux environment; the run self-recovers on the
	// next successful capture.
	StatusReasonObserverUnverified = "observer_unverified"
	// StatusReasonPRBranchMismatch: the agent opened a PR from a branch that
	// is not the run branch, so the pr-attach law (L-PR1) refuses to track
	// it (docs/design/run-state-machine.md §11 L-PR2). The run idles waiting
	// while its work sits in an untracked PR; the daemon notice (L-N1) tells
	// the agent how to repoint it.
	StatusReasonPRBranchMismatch = "pr_branch_mismatch"
)

// DaemonNoticeEventName is the note-event name for daemon-authored messages
// delivered to a run's agent session (run-state-machine.md §11 L-N1): the
// note records that the notice was sent, making the once-per-(run,subject)
// guarantee fold-derivable. Attrs: "kind" (notice family), "url"/"head"
// (subject payload for pr_branch_mismatch).
const DaemonNoticeEventName = "daemon_notice"

// NewDaemonNoticeEvent records a delivered daemon notice (note event, closed
// vocabulary). kind names the notice family; attrs carry the subject.
func NewDaemonNoticeEvent(kind string, attrs map[string]string) *Event {
	all := map[string]string{"kind": kind}
	for k, v := range attrs {
		all[k] = v
	}
	return NewEvent(EventTypeNote, DaemonNoticeEventName, all)
}

// NewStatusEventWithReason creates a status change event carrying a
// machine-readable reason attribute. An empty reason degrades to
// NewStatusEvent.
func NewStatusEventWithReason(status Status, reason string) *Event {
	if reason == "" {
		return NewStatusEvent(status)
	}
	return NewEvent(EventTypeStatus, string(status), map[string]string{AttrStatusReason: reason})
}

// NewPhaseEvent creates a phase change event
func NewPhaseEvent(phase Phase) *Event {
	return NewEvent(EventTypePhase, string(phase), nil)
}

// NewArtifactEvent creates an artifact event
func NewArtifactEvent(name string, attrs map[string]string) *Event {
	return NewEvent(EventTypeArtifact, name, attrs)
}

// NewErrorArtifactEvent creates an error artifact event to persist error messages in run files
func NewErrorArtifactEvent(errMsg string) *Event {
	return NewEvent(EventTypeArtifact, "error", map[string]string{
		"message": errMsg,
	})
}
