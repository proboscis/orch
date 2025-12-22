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
			input:   `- 2023-12-20T10:00:00+09:00 | note | n1 | text="What should we do?"`,
			wantErr: false,
			check: func(e *Event) bool {
				return e.Type == EventTypeNote && e.Name == "n1" && e.Attrs["text"] == "What should we do?"
			},
		},
		{
			name:    "event with multiple attributes",
			input:   `- 2023-12-20T10:00:00+09:00 | note | n1 | text="Choose one" | choices="A,B,C"`,
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
				Type:      EventTypeNote,
				Name:      "n1",
				Attrs:     map[string]string{"text": "What is this?"},
			},
			want: `- 2023-12-20T10:00:00+09:00 | note | n1 | text="What is this?"`,
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

func TestNewArtifactEvent(t *testing.T) {
	event := NewArtifactEvent("worktree", map[string]string{"path": "/tmp/test"})
	if event.Type != EventTypeArtifact {
		t.Errorf("expected artifact type, got %s", event.Type)
	}
	if event.Name != "worktree" {
		t.Errorf("expected worktree, got %s", event.Name)
	}
	if event.Attrs["path"] != "/tmp/test" {
		t.Errorf("expected path attr, got %s", event.Attrs["path"])
	}
}
