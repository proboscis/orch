package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestShowJSONIncludesQuestions(t *testing.T) {
	run := &model.Run{
		IssueID:      "issue-1",
		RunID:        "run-1",
		Status:       model.StatusBlocked,
		Branch:       "branch",
		WorktreePath: "/tmp/worktree",
		TmuxSession:  "session",
		PRUrl:        "http://example.com/pr/1",
	}

	run.Events = []*model.Event{
		{
			Timestamp: time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC),
			Type:      model.EventTypeStatus,
			Name:      string(model.StatusBlocked),
		},
		{
			Timestamp: time.Date(2025, 1, 1, 1, 1, 0, 0, time.UTC),
			Type:      model.EventTypeQuestion,
			Name:      "q1",
			Attrs: map[string]string{
				"text":     "Need input",
				"choices":  "A/B",
				"severity": "high",
			},
		},
	}

	out := captureStdout(t, func() {
		if err := showJSON(run, &showOptions{Tail: 10}); err != nil {
			t.Fatalf("showJSON: %v", err)
		}
	})

	var got struct {
		OK        bool `json:"ok"`
		Questions []struct {
			ID       string `json:"id"`
			Text     string `json:"text"`
			Choices  string `json:"choices"`
			Severity string `json:"severity"`
		} `json:"unanswered_questions"`
		Events []struct {
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
	if len(got.Questions) != 1 || got.Questions[0].ID != "q1" {
		t.Fatalf("unexpected questions: %+v", got.Questions)
	}
}
