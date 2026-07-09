package daemon

import (
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/store/file"
)

// ADR-0001 creation guard must hold on the daemon create path too —
// processCreateIssueCore writes issue files without going through
// FileStore.CreateIssue, so it needs its own call to the shared validator.
// (Found live: `orch issue create beef` succeeded through the daemon while
// the store-level guard test was green.)
func TestProcessCreateIssueCoreRejectsHexLikeIDs(t *testing.T) {
	st, err := file.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewSocketServer(nil, &timingTestLogger{})

	_, err = server.processCreateIssueCore(st, &CreateIssueParams{
		IssueID: "beef",
		Title:   "should fail",
	})
	if err == nil {
		t.Fatal("processCreateIssueCore(beef) = nil error, want hex-lookalike rejection")
	}
	if !strings.Contains(err.Error(), "hex") {
		t.Fatalf("error = %v, want hex-grammar rejection message", err)
	}

	if _, err := server.processCreateIssueCore(st, &CreateIssueParams{
		IssueID: "issue-0001",
		Title:   "fine",
	}); err != nil {
		t.Fatalf("processCreateIssueCore(issue-0001) error = %v, want success", err)
	}
}
