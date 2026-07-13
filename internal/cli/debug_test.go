package cli

import (
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/orchapi"
)

func TestOutputDebugRunShowsUnrevivableSessionDetail(t *testing.T) {
	run := &orchapi.Run{
		IssueID:            "issue-1",
		RunID:              "run-1",
		Status:             orchapi.RunStatusRunning,
		SessionState:       model.SessionStateReapedUnrevivable,
		SessionStateDetail: "revive precondition missing: agent_session identity is not recorded",
	}

	out := captureStdout(t, func() { outputDebugRun(run) })
	if !strings.Contains(out, "Session State: reaped(unrevivable)") {
		t.Fatalf("debug output missing session state: %q", out)
	}
	if !strings.Contains(out, "Session State Detail: revive precondition missing: agent_session identity is not recorded") {
		t.Fatalf("debug output missing unrevivable detail: %q", out)
	}
}
