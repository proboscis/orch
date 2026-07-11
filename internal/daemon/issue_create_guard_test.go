package daemon

import (
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/store/file"
)

// ADR-0001 creation guard must hold on the daemon create path too.
// Keep the explicit validation at the API boundary as well as the
// FileStore.CreateIssue validation used for the actual write.
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

	if _, err := st.ListIssues(); err != nil {
		t.Fatalf("initial ListIssues() error = %v", err)
	}
	if _, err := server.processCreateIssueCore(st, &CreateIssueParams{
		IssueID: "issue-0001",
		Title:   "fine",
	}); err != nil {
		t.Fatalf("processCreateIssueCore(issue-0001) error = %v, want success", err)
	}
	if _, err := st.ResolveIssue("issue-0001"); err != nil {
		t.Fatalf("ResolveIssue(issue-0001) after daemon create error = %v, want cache-coherent success", err)
	}
}
