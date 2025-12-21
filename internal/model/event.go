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
	EventTypeQuestion EventType = "question"
	EventTypeAnswer   EventType = "answer"
	EventTypeNote     EventType = "note"
)

// Status represents run operational lifecycle states
type Status string

const (
	StatusQueued     Status = "queued"
	StatusBooting    Status = "booting"
	StatusRunning    Status = "running"
	StatusBlocked    Status = "blocked"
	StatusBlockedAPI Status = "blocked_api"
	StatusPROpen     Status = "pr_open"
	StatusDone       Status = "done"
	StatusFailed     Status = "failed"
	StatusCanceled   Status = "canceled"
	StatusUnknown    Status = "unknown" // Agent exited unexpectedly, shell prompt showing
)

// IssueStatus represents issue resolution states
type IssueStatus string

const (
	IssueStatusOpen     IssueStatus = "open"     // Issue is active, work in progress
	IssueStatusResolved IssueStatus = "resolved" // Issue specification has been resolved
	IssueStatusClosed   IssueStatus = "closed"   // Issue is closed/archived
)

// ParseIssueStatus converts a string to IssueStatus, returning IssueStatusOpen for unknown values
func ParseIssueStatus(s string) IssueStatus {
	switch s {
	case string(IssueStatusOpen):
		return IssueStatusOpen
	case string(IssueStatusResolved):
		return IssueStatusResolved
	case string(IssueStatusClosed):
		return IssueStatusClosed
	default:
		return IssueStatusOpen // Default to open for backwards compatibility
	}
}

// IsValidIssueStatus checks if a string is a valid IssueStatus
func IsValidIssueStatus(s string) bool {
	switch s {
	case string(IssueStatusOpen), string(IssueStatusResolved), string(IssueStatusClosed):
		return true
	default:
		return false
	}
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

// NewPhaseEvent creates a phase change event
func NewPhaseEvent(phase Phase) *Event {
	return NewEvent(EventTypePhase, string(phase), nil)
}

// NewArtifactEvent creates an artifact event
func NewArtifactEvent(name string, attrs map[string]string) *Event {
	return NewEvent(EventTypeArtifact, name, attrs)
}

// NewQuestionEvent creates a question event
func NewQuestionEvent(id, text string, attrs map[string]string) *Event {
	if attrs == nil {
		attrs = make(map[string]string)
	}
	attrs["text"] = text
	return NewEvent(EventTypeQuestion, id, attrs)
}

// NewAnswerEvent creates an answer event
func NewAnswerEvent(questionID, text, by string) *Event {
	return NewEvent(EventTypeAnswer, questionID, map[string]string{
		"text": text,
		"by":   by,
	})
}
