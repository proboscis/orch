package cli

import (
	"strings"
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestBuildResumePrompt(t *testing.T) {
	issue := &model.Issue{ID: "orch-5", Title: "Resume"}
	run := &model.Run{IssueID: "orch-5", RunID: "run-1"}

	prompt := buildResumePrompt(issue, run)
	if !strings.Contains(prompt, "Resuming work on issue: orch-5") {
		t.Fatalf("missing issue header: %q", prompt)
	}
	if !strings.Contains(prompt, "Title: Resume") {
		t.Fatalf("missing title: %q", prompt)
	}
}
