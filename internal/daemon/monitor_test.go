package daemon

import (
	"io"
	"log"
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestDetectStatusAPILimited(t *testing.T) {
	d := &Daemon{
		logger: log.New(io.Discard, "", 0),
	}
	run := &model.Run{IssueID: "orch-016", RunID: "20251221-144231"}
	state := &RunState{LastOutputAt: time.Now()}

	tests := []string{
		"Cost limit reached",
		"Rate limit exceeded",
		"Rate limit reached",
		"Quota exceeded",
		"Resource exhausted",
		"Insufficient quota",
	}

	for _, msg := range tests {
		output := "Error: " + msg
		status := d.detectStatus(run, output, state, false, true)
		if status != model.StatusBlockedAPI {
			t.Fatalf("expected blocked_api for %q, got %q", msg, status)
		}
	}
}
