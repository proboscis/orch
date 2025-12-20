package model

import (
	"testing"
	"time"
)

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*Event) bool
	}{
		{
			name:    "simple status event",
			input:   "- 2023-12-20T10:00:00+09:00 | status | running",
			wantErr: false,
			check: func(e *Event) bool {
				return e.Type == EventTypeStatus && e.Name == "running"
			},
		},
		{
			name:    "event with attributes",
			input:   `- 2023-12-20T10:00:00+09:00 | artifact | worktree | path=/tmp/test`,
			wantErr: false,
			check: func(e *Event) bool {
				return e.Type == EventTypeArtifact && e.Name == "worktree" && e.Attrs["path"] == "/tmp/test"
			},
		},
		{
			name:    "event with quoted attribute",
			input:   `- 2023-12-20T10:00:00+09:00 | question | q1 | text="What should we do?"`,
			wantErr: false,
			check: func(e *Event) bool {
				return e.Type == EventTypeQuestion && e.Name == "q1" && e.Attrs["text"] == "What should we do?"
			},
		},
		{
			name:    "event with multiple attributes",
			input:   `- 2023-12-20T10:00:00+09:00 | question | q1 | text="Choose one" | choices="A,B,C"`,
			wantErr: false,
			check: func(e *Event) bool {
				return e.Attrs["text"] == "Choose one" && e.Attrs["choices"] == "A,B,C"
			},
		},
		{
			name:    "invalid - no bullet",
			input:   "2023-12-20T10:00:00+09:00 | status | running",
			wantErr: true,
		},
		{
			name:    "invalid - bad timestamp",
			input:   "- not-a-timestamp | status | running",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := ParseEvent(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil && !tt.check(event) {
				t.Errorf("ParseEvent() check failed for event: %+v", event)
			}
		})
	}
}

func TestEventString(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339, "2023-12-20T10:00:00+09:00")

	tests := []struct {
		name  string
		event *Event
		want  string
	}{
		{
			name: "simple event",
			event: &Event{
				Timestamp: ts,
				Type:      EventTypeStatus,
				Name:      "running",
				Attrs:     map[string]string{},
			},
			want: "- 2023-12-20T10:00:00+09:00 | status | running",
		},
		{
			name: "event with attribute",
			event: &Event{
				Timestamp: ts,
				Type:      EventTypeArtifact,
				Name:      "branch",
				Attrs:     map[string]string{"name": "main"},
			},
			want: "- 2023-12-20T10:00:00+09:00 | artifact | branch | name=main",
		},
		{
			name: "event with quoted attribute",
			event: &Event{
				Timestamp: ts,
				Type:      EventTypeQuestion,
				Name:      "q1",
				Attrs:     map[string]string{"text": "What is this?"},
			},
			want: `- 2023-12-20T10:00:00+09:00 | question | q1 | text="What is this?"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.event.String()
			if got != tt.want {
				t.Errorf("Event.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewStatusEvent(t *testing.T) {
	event := NewStatusEvent(StatusRunning)
	if event.Type != EventTypeStatus {
		t.Errorf("expected status type, got %s", event.Type)
	}
	if event.Name != string(StatusRunning) {
		t.Errorf("expected running, got %s", event.Name)
	}
}

func TestNewQuestionEvent(t *testing.T) {
	event := NewQuestionEvent("q1", "What is your choice?", map[string]string{"choices": "A,B"})
	if event.Type != EventTypeQuestion {
		t.Errorf("expected question type, got %s", event.Type)
	}
	if event.Name != "q1" {
		t.Errorf("expected q1, got %s", event.Name)
	}
	if event.Attrs["text"] != "What is your choice?" {
		t.Errorf("expected question text, got %s", event.Attrs["text"])
	}
	if event.Attrs["choices"] != "A,B" {
		t.Errorf("expected choices, got %s", event.Attrs["choices"])
	}
}
