package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/orchapi"
)

func TestShowJSONIncludesEvents(t *testing.T) {
	run := &orchapi.Run{
		IssueID:      "issue-1",
		RunID:        "run-1",
		Status:       orchapi.RunStatusRunning,
		Branch:       "branch",
		WorktreePath: "/tmp/worktree",
		Target:       "mac",
		TargetHost:   "mac",
		SessionName:  "session",
		PRUrl:        "http://example.com/pr/1",
	}

	run.Events = []*orchapi.Event{
		{
			Timestamp: time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC),
			Type:      "status",
			Name:      "running",
		},
		{
			Timestamp: time.Date(2025, 1, 1, 1, 1, 0, 0, time.UTC),
			Type:      "phase",
			Name:      "implement",
		},
	}

	out := captureStdout(t, func() {
		if err := showJSON(run, &showOptions{Tail: 10}); err != nil {
			t.Fatalf("showJSON: %v", err)
		}
	})

	var got struct {
		OK         bool   `json:"ok"`
		Target     string `json:"target"`
		TargetHost string `json:"target_host"`
		Events     []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"events"`
	}

	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || len(got.Events) != 2 {
		t.Fatalf("unexpected response: %+v", got)
	}
	if got.Target != "mac" || got.TargetHost != "mac" {
		t.Fatalf("unexpected target metadata: %+v", got)
	}
}
